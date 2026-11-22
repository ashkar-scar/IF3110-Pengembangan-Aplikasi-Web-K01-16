package queue

import (
	"context"
	"errors"
	"strconv"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Service struct{ *Repository }

func NewService(repository *Repository) *Service { return &Service{repository} }

var _ JobEnqueuer = (*Service)(nil)

func (s *Service) Enqueue(ctx context.Context, req EnqueueRequest) (jobID string, err error) {
	ctx, span := observability.Start(ctx, "queue.enqueue")
	defer func() { observability.End(span, err) }()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var targetID int64
	var action ActionType
	var mode, status string
	err = tx.QueryRow(ctx, `SELECT target_id,action_type,execution_mode,status FROM migration_requests WHERE id=$1 FOR UPDATE`, req.RequestID).Scan(&targetID, &action, &mode, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if targetID != req.TargetID || action != req.Action {
		return "", ErrConflict
	}
	if err = Authorize(ctx, tx, req.ActorID, targetID); err != nil {
		return "", err
	}
	if err = ValidateApproval(ctx, tx, req.RequestID); err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM migration_jobs WHERE request_id=$1 AND status IN ('PENDING','RUNNING')`, req.RequestID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if status != "APPROVED" {
		return "", ErrConflict
	}
	id := "job_" + uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO migration_jobs(id,request_id,target_id,action_type,execution_mode,status,created_by,trace_context)
 VALUES($1,$2,$3,$4,$5,'PENDING',$6,$7)`, id, req.RequestID, targetID, action, mode, req.ActorID, observability.TraceParent(ctx))
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE migration_requests SET status='ENQUEUED',updated_at=now() WHERE id=$1`, req.RequestID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	observability.Event(ctx, "queue_enqueue", "PENDING", "job_id", id, "target_id", targetID, "actor_id", req.ActorID)
	return id, nil
}

func (s *Service) Retry(ctx context.Context, id string, actorID int64, actorName string) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM migration_jobs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return "", err
	}
	if err = Authorize(ctx, tx, actorID, j.TargetID); err != nil {
		return "", err
	}
	if err = ValidateApproval(ctx, tx, j.RequestID); err != nil {
		return "", err
	}
	if !j.CanRetry {
		return "", ErrConflict
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM target_locks WHERE target_id=$1 AND job_id<>$2 AND blocked_reason<>'')`, j.TargetID, id).Scan(&blocked); err != nil {
		return "", err
	}
	if blocked {
		return "", ErrConflict
	}
	var other bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM migration_jobs WHERE target_id=$1 AND id<>$2 AND status IN ('PENDING','RUNNING','PARTIAL','PARTIALLY_REVERTED','UNKNOWN') AND action_type='CANCEL_BATCH')`, j.TargetID, id).Scan(&other); err != nil {
		return "", err
	}
	if other {
		return "", ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE migration_jobs SET status='PENDING',retry_count=retry_count+1,next_retry_at=now(),
 finished_at=NULL,safe_error='',created_by=$2,trace_context=$3 WHERE id=$1`, id, actorID, observability.TraceParent(ctx))
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE migration_requests SET status='ENQUEUED',updated_at=now() WHERE id=$1`, j.RequestID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	observability.Event(ctx, "queue_retry", "PENDING", "job_id", id, "actor_id", actorID)
	return id, nil
}

func (s *Service) CancelBatch(ctx context.Context, id string, actorID int64, actorName string) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM migration_jobs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return "", err
	}
	if err = Authorize(ctx, tx, actorID, j.TargetID); err != nil {
		return "", err
	}
	if !j.CanCancelBatch {
		return "", ErrConflict
	}
	var executionID string
	err = tx.QueryRow(ctx, `SELECT id FROM migration_attempts WHERE job_id=$1 AND batch_no IS NULL
 ORDER BY (status='SUCCESS') DESC,started_at DESC,id DESC LIMIT 1`, id).Scan(&executionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	var requestID int64
	err = tx.QueryRow(ctx, `SELECT id FROM migration_requests WHERE action_type='CANCEL_BATCH' AND target_execution_id=$1
 AND status IN ('PENDING_REVIEW','APPROVED','ENQUEUED') ORDER BY id DESC LIMIT 1`, executionID).Scan(&requestID)
	if err == nil {
		return strconv.FormatInt(requestID, 10), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	err = tx.QueryRow(ctx, `INSERT INTO migration_requests(target_id,draft_id,draft_revision,action_type,target_execution_id,
 sql_snapshot_up,sql_snapshot_down,is_irreversible,irreversible_reason,execution_mode,batch_column,batch_size,
 base_version,fingerprint,requested_by,status)
 SELECT target_id,draft_id,draft_revision,'CANCEL_BATCH',$2,sql_snapshot_up,sql_snapshot_down,is_irreversible,
 irreversible_reason,execution_mode,batch_column,batch_size,base_version,fingerprint,$3,'PENDING_REVIEW'
 FROM migration_requests WHERE id=$1 AND NOT is_irreversible AND sql_snapshot_down<>'' RETURNING id`, j.RequestID, executionID, actorID).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	var missing bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM database_grants WHERE database_id=$1) OR EXISTS(
 SELECT 1 FROM database_grants g JOIN groups gr ON gr.id=g.group_id LEFT JOIN users u ON u.id=gr.leader_id
 WHERE g.database_id=$1 AND NOT COALESCE(u.is_active,false))`, j.TargetID).Scan(&missing)
	if err != nil {
		return "", err
	}
	if missing {
		return "", ErrNoGroupLeader
	}
	_, err = tx.Exec(ctx, `INSERT INTO migration_approvals(request_id,group_id,leader_id)
 SELECT $1,g.group_id,gr.leader_id FROM database_grants g JOIN groups gr ON gr.id=g.group_id WHERE g.database_id=$2`, requestID, j.TargetID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	observability.Event(ctx, "batch_cancel_requested", "PENDING_REVIEW", "job_id", id, "request_id", requestID, "actor_id", actorID)
	return strconv.FormatInt(requestID, 10), nil
}
