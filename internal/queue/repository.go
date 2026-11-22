package queue

import (
	"context"
	"errors"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{Pool: pool} }

const jobColumns = `id,request_id,target_id,action_type,execution_mode,status,total_batches,completed_batches,
 rows_processed,last_cursor,retry_count,max_retries,next_retry_at,locked_by,lock_expires_at,trace_context,
 safe_error,created_by,created_at,started_at,finished_at,
 (EXISTS(SELECT 1 FROM migration_requests r WHERE r.id=migration_jobs.request_id AND NOT r.is_irreversible AND r.sql_snapshot_down<>'')
 AND NOT EXISTS(SELECT 1 FROM target_locks l WHERE l.target_id=migration_jobs.target_id AND l.job_id<>migration_jobs.id AND l.blocked_reason<>'')),
 (NOT EXISTS(SELECT 1 FROM target_locks l WHERE l.target_id=migration_jobs.target_id AND l.job_id<>migration_jobs.id AND l.blocked_reason<>'')
 AND NOT EXISTS(SELECT 1 FROM migration_requests cancel_request JOIN migration_attempts source ON source.id=cancel_request.target_execution_id
 WHERE source.job_id=migration_jobs.id AND cancel_request.action_type='CANCEL_BATCH'
 AND cancel_request.status IN ('PENDING_REVIEW','APPROVED','ENQUEUED')))`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var reversible bool
	var retryAllowed bool
	err := row.Scan(&j.ID, &j.RequestID, &j.TargetID, &j.Action, &j.Mode, &j.Status, &j.TotalBatches, &j.CompletedBatches,
		&j.RowsProcessed, &j.LastCursor, &j.RetryCount, &j.MaxRetries, &j.NextRetryAt, &j.LockedBy, &j.LockExpiresAt,
		&j.TraceContext, &j.SafeError, &j.CreatedBy, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &reversible, &retryAllowed)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	j.CanRetry = retryAllowed && (j.Status == "FAILED" || j.Status == "PARTIAL" || j.Status == "PARTIALLY_REVERTED" || j.Status == "UNKNOWN")
	j.CanCancelBatch = reversible && j.Mode == core.ExecutionBatch && (j.Status == "PARTIAL" || j.Status == "SUCCESS") && (j.Action == ActionUp || j.Action == ActionForward)
	j.Checkpoints = []Checkpoint{}
	return j, err
}

func (r *Repository) Get(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(r.Pool.QueryRow(ctx, "SELECT "+jobColumns+" FROM migration_jobs WHERE id=$1", id))
	if err != nil {
		return j, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT batch_no,cursor_start,cursor_end,rows_affected,status,committed_at,reverted_at
 FROM batch_checkpoints WHERE job_id=$1 ORDER BY batch_no`, id)
	if err != nil {
		return j, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Checkpoint
		if err = rows.Scan(&c.BatchNo, &c.CursorStart, &c.CursorEnd, &c.RowsAffected, &c.Status, &c.CommittedAt, &c.RevertedAt); err != nil {
			return j, err
		}
		j.Checkpoints = append(j.Checkpoints, c)
	}
	return j, rows.Err()
}

func (r *Repository) List(ctx context.Context, targetID int64, offset int) ([]Job, error) {
	rows, err := r.Pool.Query(ctx, "SELECT "+jobColumns+" FROM migration_jobs WHERE target_id=$1 ORDER BY created_at DESC,id DESC LIMIT 201 OFFSET $2", targetID, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *Repository) Request(ctx context.Context, id int64) (core.MigrationRequest, error) {
	var q core.MigrationRequest
	err := r.Pool.QueryRow(ctx, `SELECT r.id,r.target_id,r.draft_id,r.draft_revision,r.action_type,r.target_execution_id,
 r.sql_snapshot_up,r.sql_snapshot_down,r.is_irreversible,r.irreversible_reason,r.execution_mode,r.batch_column,
 r.batch_size,r.base_version,r.fingerprint,r.requested_by,r.status,d.description
 FROM migration_requests r JOIN migration_drafts d ON d.id=r.draft_id WHERE r.id=$1`, id).Scan(
		&q.ID, &q.TargetID, &q.DraftID, &q.DraftRevision, &q.Action, &q.TargetExecutionID, &q.SQLSnapshotUp, &q.SQLSnapshotDown,
		&q.IsIrreversible, &q.IrreversibleReason, &q.ExecutionMode, &q.BatchColumn, &q.BatchSize, &q.BaseVersion, &q.Fingerprint,
		&q.RequestedBy, &q.Status, &q.Description)
	return q, err
}

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Authorize(ctx context.Context, db queryRow, actorID, targetID int64) error {
	var allowed bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN group_members m ON m.user_id=u.id
 JOIN database_grants g ON g.group_id=m.group_id JOIN database_targets t ON t.id=g.database_id
 WHERE u.id=$1 AND u.is_active AND t.id=$2 AND t.is_active AND t.driver='postgres' AND g.permission='EDIT')`, actorID, targetID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func ValidateApproval(ctx context.Context, db queryRow, requestID int64) error {
	var valid bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM database_grants WHERE database_id=r.target_id)
 AND NOT EXISTS(SELECT 1 FROM database_grants g JOIN groups gr ON gr.id=g.group_id
 LEFT JOIN users u ON u.id=gr.leader_id
 LEFT JOIN migration_approvals a ON a.request_id=r.id AND a.group_id=g.group_id
 WHERE g.database_id=r.target_id AND (gr.leader_id IS NULL OR NOT COALESCE(u.is_active,false)
 OR a.id IS NULL OR a.leader_id<>gr.leader_id OR a.decision<>'APPROVED'))
 FROM migration_requests r WHERE r.id=$1`, requestID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrApproval
	}
	return nil
}
