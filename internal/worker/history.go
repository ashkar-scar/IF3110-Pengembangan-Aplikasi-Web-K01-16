package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/jackc/pgx/v5"
)

func executionSQL(e targetdb.WorkerExecution) string {
	if e.Request.Action == core.ActionRollback || e.Request.Action == core.ActionCancelBatch {
		return e.Request.SQLSnapshotDown
	}
	return e.Request.SQLSnapshotUp
}

func batchSQL(e targetdb.WorkerExecution) string {
	query := strings.TrimSuffix(strings.TrimSpace(executionSQL(e)), ";")
	if e.Request.Action == core.ActionUp || e.Request.Action == core.ActionForward {
		query += " RETURNING " + targetdb.Quote(e.Request.BatchColumn)
	}
	return query
}

func (w *Worker) startAttempt(ctx context.Context, j queue.Job, e targetdb.WorkerExecution) error {
	tx, err := w.Queue.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = w.lockOwnedJob(ctx, tx, j.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO migration_attempts(id,target_id,draft_id,draft_revision,request_id,job_id,
 action_type,reverts_execution_id,attempt_no,description,sql_text,actor_id,actor_name,base_version,status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'RUNNING') ON CONFLICT(id) DO NOTHING`,
		e.ExecutionID, j.TargetID, e.Request.DraftID, e.Request.DraftRevision, j.RequestID, j.ID, j.Action, e.Request.TargetExecutionID,
		j.RetryCount+1, e.Request.Description, executionSQL(e), e.ActorID, e.ActorName, e.Request.BaseVersion)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	observability.Event(ctx, "migration_attempt", "RUNNING", "query", observability.MaskSQL(executionSQL(e)))
	return err
}

func (w *Worker) syncProgress(ctx context.Context, j queue.Job, e targetdb.WorkerExecution, session *targetdb.WorkerSession, state targetdb.WorkerResult) error {
	checkpoints, err := session.Checkpoints(ctx, j.ID)
	if err != nil {
		return err
	}
	tx, err := w.Queue.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = w.lockOwnedJob(ctx, tx, j.ID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT batch_no,status FROM batch_checkpoints WHERE job_id=$1`, j.ID)
	if err != nil {
		return err
	}
	existing := map[int]string{}
	for rows.Next() {
		var n int
		var status string
		if err = rows.Scan(&n, &status); err != nil {
			rows.Close()
			return err
		}
		existing[n] = status
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var newBatches, newRows int
	for _, c := range checkpoints {
		if existing[c.BatchNo] == c.Status {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO batch_checkpoints(job_id,batch_no,cursor_start,cursor_end,rows_affected,status,committed_at,reverted_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(job_id,batch_no) DO UPDATE SET status=excluded.status,reverted_at=excluded.reverted_at`,
			j.ID, c.BatchNo, c.CursorStart(), c.CursorEnd(), c.Rows, c.Status, c.CommittedAt, c.RevertedAt)
		if err != nil {
			return err
		}
		if c.Status == "REVERTED" && e.SourceJobID != "" {
			if _, err = tx.Exec(ctx, `UPDATE batch_checkpoints SET status='REVERTED',reverted_at=$3 WHERE job_id=$1 AND batch_no=$2`, e.SourceJobID, c.BatchNo, c.RevertedAt); err != nil {
				return err
			}
		}
		reverts := e.Request.TargetExecutionID
		if c.Status == "REVERTED" && e.SourceJobID != "" {
			id := e.SourceJobID + "_batch_" + strconv.Itoa(c.BatchNo)
			reverts = &id
		}
		tag, err := tx.Exec(ctx, `INSERT INTO migration_attempts(id,target_id,draft_id,draft_revision,request_id,job_id,action_type,
 reverts_execution_id,attempt_no,batch_no,description,sql_text,actor_id,actor_name,base_version,status,started_at,finished_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'SUCCESS',$16,$16) ON CONFLICT(id) DO NOTHING`,
			j.ID+"_batch_"+strconv.Itoa(c.BatchNo), j.TargetID, e.Request.DraftID, e.Request.DraftRevision, j.RequestID, j.ID, j.Action,
			reverts, j.RetryCount+1, c.BatchNo, e.Request.Description, batchSQL(e), e.ActorID, e.ActorName, e.Request.BaseVersion, c.CommittedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			newBatches++
			newRows += c.Rows
		}
	}
	_, err = tx.Exec(ctx, `UPDATE migration_jobs SET total_batches=$2,completed_batches=$3,rows_processed=$4,last_cursor=$5 WHERE id=$1 AND locked_by=$6`, j.ID, state.TotalBatches, state.CompletedBatches, state.RowsProcessed, strconv.FormatInt(state.LastCursor, 10), w.ID)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if w.Metrics != nil {
		w.Metrics.Batches.WithLabelValues(string(j.Action), string(j.Mode)).Add(float64(newBatches))
		w.Metrics.Rows.WithLabelValues(string(j.Action), string(j.Mode)).Add(float64(newRows))
	}
	return nil
}

func transient(err error) bool {
	switch observability.SQLState(err) {
	case "40001", "40P01", "55P03", "53300", "57P03", "08000", "08001", "08003", "08006":
		return true
	}
	var network net.Error
	return errors.As(err, &network)
}
func backoff(retry int) time.Duration { return time.Second * time.Duration(1<<min(max(retry, 0), 6)) }

func failureStatus(j queue.Job, state targetdb.WorkerResult, err error) string {
	if err == nil {
		if j.Action == queue.ActionCancelBatch {
			return "REVERTED"
		}
		return "SUCCESS"
	}
	if errors.Is(err, targetdb.ErrUnknown) {
		return "UNKNOWN"
	}
	if state.CompletedBatches > 0 {
		if j.Action == queue.ActionCancelBatch || j.Action == queue.ActionRollback {
			return "PARTIALLY_REVERTED"
		}
		return "PARTIAL"
	}
	return "FAILED"
}

func (w *Worker) finish(ctx context.Context, j queue.Job, e targetdb.WorkerExecution, state targetdb.WorkerResult, runErr error) error {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	status := failureStatus(j, state, runErr)
	safe := targetdb.WorkerError(runErr)
	if errors.Is(runErr, queue.ErrForbidden) || errors.Is(runErr, queue.ErrApproval) {
		safe = runErr.Error()
	}
	tx, err := w.Queue.Pool.Begin(finishCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(finishCtx)
	if err = w.lockOwnedJob(finishCtx, tx, j.ID); err != nil {
		return err
	}
	var batchFailure *targetdb.BatchExecutionError
	if errors.As(runErr, &batchFailure) {
		_, err = tx.Exec(finishCtx, `INSERT INTO migration_attempts(id,target_id,draft_id,draft_revision,request_id,job_id,action_type,
 reverts_execution_id,attempt_no,batch_no,description,sql_text,actor_id,actor_name,base_version,status,safe_error,finished_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'FAILED',$16,now()) ON CONFLICT(id) DO NOTHING`,
			e.ExecutionID+"_failed_batch_"+strconv.Itoa(batchFailure.BatchNo), j.TargetID, e.Request.DraftID, e.Request.DraftRevision, j.RequestID, j.ID, j.Action,
			e.Request.TargetExecutionID, j.RetryCount+1, batchFailure.BatchNo, e.Request.Description, batchFailure.SQL, e.ActorID, e.ActorName, e.Request.BaseVersion, safe)
		if err != nil {
			return err
		}
	}
	attemptStatus := "SUCCESS"
	if runErr != nil {
		attemptStatus = "FAILED"
	}
	if status == "UNKNOWN" {
		attemptStatus = "UNKNOWN"
	}
	var version *int64
	if state.Version > 0 {
		version = &state.Version
	}
	if runErr == nil && state.ExecutionID != "" && state.ExecutionID != e.ExecutionID {
		_, err = tx.Exec(finishCtx, `UPDATE migration_attempts SET status='SUCCESS',version=$2,finished_at=now(),safe_error='' WHERE id=$1 AND status IN ('RUNNING','UNKNOWN')`, state.ExecutionID, version)
		if err != nil {
			return err
		}
		attemptStatus = "FAILED"
		safe = "rekonsiliasi: execution " + state.ExecutionID + " sudah committed; SQL tidak diulang"
	}
	_, err = tx.Exec(finishCtx, `UPDATE migration_attempts SET status=$2,version=$3,safe_error=$4,finished_at=now() WHERE id=$1 AND status IN ('RUNNING','UNKNOWN')`, e.ExecutionID, attemptStatus, version, safe)
	if err != nil {
		return err
	}
	blocked := ""
	if status == "UNKNOWN" || status == "PARTIAL" || status == "PARTIALLY_REVERTED" || (runErr != nil && j.Mode == core.ExecutionBatch && state.TotalBatches > 0) {
		blocked = status
	}
	if runErr != nil && e.SourceJobID != "" {
		var sourceStatus string
		if err = tx.QueryRow(finishCtx, `SELECT status FROM migration_jobs WHERE id=$1`, e.SourceJobID).Scan(&sourceStatus); err != nil {
			return err
		}
		if sourceStatus == "PARTIAL" || sourceStatus == "UNKNOWN" || sourceStatus == "PARTIALLY_REVERTED" {
			blocked = status
		}
	}
	requestStatus := "COMPLETED"
	if runErr != nil {
		requestStatus = "FAILED"
	}
	var nextRetry *time.Time
	retry := j.RetryCount
	if runErr != nil && transient(runErr) && status != "UNKNOWN" && retry < j.MaxRetries && ctx.Err() == nil {
		retry++
		next := time.Now().Add(backoff(retry - 1))
		nextRetry = &next
		status = "PENDING"
		requestStatus = "ENQUEUED"
		if w.Metrics != nil {
			w.Metrics.Retries.WithLabelValues(string(j.Action), string(j.Mode)).Inc()
		}
	}
	_, err = tx.Exec(finishCtx, `UPDATE migration_jobs SET status=$2,safe_error=$3,finished_at=CASE WHEN $2='PENDING' THEN NULL ELSE now() END,locked_by='',lock_expires_at=NULL,
 retry_count=$4,next_retry_at=$5 WHERE id=$1 AND locked_by=$6`, j.ID, status, safe, retry, nextRetry, w.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(finishCtx, `UPDATE migration_requests SET status=$2,updated_at=now() WHERE id=$1`, j.RequestID, requestStatus)
	if err != nil {
		return err
	}
	if runErr == nil && e.SourceJobID != "" && (j.Action == queue.ActionCancelBatch || j.Action == queue.ActionRollback) {
		_, err = tx.Exec(finishCtx, `UPDATE migration_jobs SET status='REVERTED' WHERE id=$1`, e.SourceJobID)
		if err != nil {
			return err
		}
	}
	if blocked != "" {
		_, err = tx.Exec(finishCtx, `UPDATE target_locks SET blocked_reason=$2,expires_at=now() WHERE job_id=$1`, j.ID, blocked)
	} else {
		_, err = tx.Exec(finishCtx, `DELETE FROM target_locks WHERE job_id=$1`, j.ID)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(finishCtx); err != nil {
		return err
	}
	observability.Event(ctx, "migration_result", status, "error", safe, "sqlstate", observability.SQLState(runErr), "rows_affected", state.RowsProcessed)
	if runErr != nil {
		return fmt.Errorf("job %s: %s", j.ID, safe)
	}
	return nil
}

func (w *Worker) lockOwnedJob(ctx context.Context, tx pgx.Tx, id string) error {
	var owner, status string
	if err := tx.QueryRow(ctx, `SELECT locked_by,status FROM migration_jobs WHERE id=$1 FOR UPDATE`, id).Scan(&owner, &status); err != nil {
		return err
	}
	if owner != w.ID || status != "RUNNING" {
		return queue.ErrConflict
	}
	return nil
}

func (w *Worker) deferJob(ctx context.Context, j queue.Job, cause error) error {
	next := time.Now().Add(w.PollInterval)
	tx, err := w.Queue.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = w.lockOwnedJob(ctx, tx, j.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE migration_jobs SET status='PENDING',next_retry_at=$2,locked_by='',lock_expires_at=NULL,safe_error=$3 WHERE id=$1`, j.ID, next, targetdb.WorkerError(cause))
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	observability.Event(ctx, "queue_deferred", "PENDING", "reason", targetdb.WorkerError(cause))
	return nil
}
