package worker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	Queue        *queue.Repository
	Targets      *store.TargetRepository
	Metrics      *observability.Metrics
	ID           string
	Concurrency  int
	PollInterval time.Duration
	Lease        time.Duration
}

func New(repository *queue.Repository, targets *store.TargetRepository, metrics *observability.Metrics) *Worker {
	concurrency := min(4, max(1, int(repository.Pool.Stat().MaxConns())-2))
	return &Worker{Queue: repository, Targets: targets, Metrics: metrics, ID: "worker_" + uuid.NewString(), Concurrency: concurrency, PollInterval: time.Second, Lease: 15 * time.Second}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Concurrency < 1 || w.Lease < 3*time.Second || w.PollInterval <= 0 {
		return errors.New("konfigurasi worker tidak valid")
	}
	if int(w.Queue.Pool.Stat().MaxConns()) < w.Concurrency+2 {
		return errors.New("pool platform memerlukan sedikitnya WORKER_CONCURRENCY + 2 koneksi")
	}
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				worked, err := w.RunOne(ctx)
				if err != nil && ctx.Err() == nil {
					slog.ErrorContext(ctx, "worker gagal memproses job", "error", observability.SafeError(err))
				}
				if !worked || err != nil {
					select {
					case <-ctx.Done():
						return
					case <-time.After(w.PollInterval):
					}
				}
			}
		}()
	}
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) claim(ctx context.Context) (queue.Job, *pgxpool.Conn, error) {
	conn, err := w.Queue.Pool.Acquire(ctx)
	if err != nil {
		return queue.Job{}, nil, err
	}
	claimed := false
	defer func() {
		if !claimed {
			conn.Release()
		}
	}()
	rows, err := conn.Query(ctx, `SELECT id,target_id FROM (
 SELECT DISTINCT ON (j.target_id) j.id,j.target_id,j.created_at FROM migration_jobs j
 JOIN migration_requests r ON r.id=j.request_id LEFT JOIN target_locks l ON l.target_id=j.target_id
 WHERE ((j.status='PENDING' AND (j.next_retry_at IS NULL OR j.next_retry_at<=now()))
 OR (j.status='RUNNING' AND j.lock_expires_at<now()))
 AND (l.target_id IS NULL OR l.blocked_reason='' OR l.job_id=j.id
 OR (j.action_type='CANCEL_BATCH' AND EXISTS(SELECT 1 FROM migration_attempts a WHERE a.id=r.target_execution_id AND a.job_id=l.job_id)))
 ORDER BY j.target_id,j.created_at,j.id) candidates ORDER BY created_at,id LIMIT 32`)
	if err != nil {
		return queue.Job{}, nil, err
	}
	type candidate struct {
		id     string
		target int64
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.target); err != nil {
			rows.Close()
			return queue.Job{}, nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return queue.Job{}, nil, err
	}
	for _, c := range candidates {
		var locked bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended(current_schema() || ':ruangdb:target:' || $1::bigint::text,0))`, c.target).Scan(&locked); err != nil {
			claimed = true
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			conn.Hijack().Close(cleanup)
			cancel()
			return queue.Job{}, nil, err
		}
		if !locked {
			continue
		}
		job, err := w.claimLocked(ctx, conn, c.id, c.target)
		if err == nil {
			claimed = true
			return job, conn, nil
		}
		if _, unlockErr := conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended(current_schema() || ':ruangdb:target:' || $1::bigint::text,0))`, c.target); unlockErr != nil {
			conn.Hijack().Close(context.Background())
			claimed = true
			return queue.Job{}, nil, unlockErr
		}
		if !errors.Is(err, queue.ErrConflict) {
			return queue.Job{}, nil, err
		}
	}
	return queue.Job{}, nil, nil
}

func (w *Worker) claimLocked(ctx context.Context, conn *pgxpool.Conn, id string, targetID int64) (queue.Job, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return queue.Job{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE migration_jobs SET status='RUNNING',locked_by=$2,lock_expires_at=now()+$3::interval,
 started_at=COALESCE(started_at,now()) WHERE id=$1 AND (status='PENDING' OR (status='RUNNING' AND lock_expires_at<now()))`, id, w.ID, w.Lease.String())
	if err != nil {
		return queue.Job{}, err
	}
	if tag.RowsAffected() == 0 {
		return queue.Job{}, queue.ErrConflict
	}
	tag, err = tx.Exec(ctx, `INSERT INTO target_locks(target_id,job_id,worker_id,expires_at) VALUES($1,$2,$3,now()+$4::interval)
 ON CONFLICT(target_id) DO UPDATE SET job_id=excluded.job_id,worker_id=excluded.worker_id,expires_at=excluded.expires_at
 WHERE target_locks.blocked_reason='' OR target_locks.job_id=excluded.job_id OR EXISTS(
 SELECT 1 FROM migration_requests r JOIN migration_jobs j ON j.request_id=r.id
 JOIN migration_attempts a ON a.id=r.target_execution_id
 WHERE j.id=$2 AND r.action_type='CANCEL_BATCH' AND a.job_id=target_locks.job_id)`, targetID, id, w.ID, w.Lease.String())
	if err != nil {
		return queue.Job{}, err
	}
	if tag.RowsAffected() == 0 {
		return queue.Job{}, queue.ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return queue.Job{}, err
	}
	return w.Queue.Get(ctx, id)
}

func (w *Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, id string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(w.Lease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beatCtx, stop := context.WithTimeout(ctx, w.Lease/3)
			tag, err := w.Queue.Pool.Exec(beatCtx, `UPDATE migration_jobs SET lock_expires_at=now()+$3::interval WHERE id=$1 AND locked_by=$2 AND status='RUNNING'`, id, w.ID, w.Lease.String())
			if err == nil && tag.RowsAffected() == 1 {
				tag, err = w.Queue.Pool.Exec(beatCtx, `UPDATE target_locks SET expires_at=now()+$3::interval WHERE job_id=$1 AND worker_id=$2`, id, w.ID, w.Lease.String())
				if err == nil && tag.RowsAffected() != 1 {
					err = queue.ErrConflict
				}
			} else if err == nil {
				err = queue.ErrConflict
			}
			stop()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	job, platformLock, err := w.claim(ctx)
	if err != nil {
		return false, err
	}
	if platformLock == nil {
		return false, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := platformLock.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended(current_schema() || ':ruangdb:target:' || $1::bigint::text,0))`, job.TargetID); err != nil {
			platformLock.Hijack().Close(unlockCtx)
		} else {
			platformLock.Release()
		}
	}()
	ctx = observability.Extract(ctx, job.TraceContext)
	ctx, span := observability.Start(ctx, "worker.process")
	defer func() { observability.End(span, err) }()
	ctx = observability.WithFields(ctx, "job_id", job.ID, "target_id", job.TargetID, "actor_id", job.CreatedBy, "attempt_no", job.RetryCount+1)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go w.heartbeat(ctx, cancel, job.ID, done)
	defer func() { cancel(); <-done }()
	observability.Event(ctx, "worker_pickup", "RUNNING")
	err = w.process(ctx, job)
	return true, err
}

func (w *Worker) process(ctx context.Context, job queue.Job) error {
	started := time.Now()
	attempted := false
	defer func() {
		if attempted && w.Metrics != nil {
			w.Metrics.Duration.WithLabelValues(string(job.Action), string(job.Mode)).Observe(time.Since(started).Seconds())
		}
	}()
	request, err := w.Queue.Request(ctx, job.RequestID)
	if err != nil {
		return err
	}
	var actor string
	if err = w.Queue.Pool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, job.CreatedBy).Scan(&actor); err != nil {
		return err
	}
	execution := targetdb.WorkerExecution{JobID: job.ID, ExecutionID: "exec_" + job.ID + "_" + strconv.Itoa(job.RetryCount+1), Request: request, ActorID: job.CreatedBy, ActorName: actor}
	if (request.Action == core.ActionRollback || request.Action == core.ActionCancelBatch) && request.TargetExecutionID != nil {
		if err = w.Queue.Pool.QueryRow(ctx, `SELECT COALESCE(job_id,'') FROM migration_attempts WHERE id=$1 AND target_id=$2`, *request.TargetExecutionID, job.TargetID).Scan(&execution.SourceJobID); err != nil {
			return err
		}
	}
	ctx = observability.WithFields(ctx, "actor_name", actor, "execution_id", execution.ExecutionID, "draft_id", request.DraftID)
	var unresolved bool
	if err = w.Queue.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM migration_attempts WHERE job_id=$1 AND batch_no IS NULL AND status IN ('RUNNING','UNKNOWN'))`, job.ID).Scan(&unresolved); err != nil {
		return err
	}
	startAttempt := func() error {
		err := w.startAttempt(ctx, job, execution)
		if err == nil {
			attempted = true
			if w.Metrics != nil {
				queuedAt := job.CreatedAt
				if job.RetryCount > 0 && job.NextRetryAt != nil {
					queuedAt = *job.NextRetryAt
				}
				w.Metrics.QueueWait.WithLabelValues(string(job.Action), string(job.Mode)).Observe(max(0, time.Since(queuedAt).Seconds()))
			}
		}
		return err
	}
	failBeforeExecution := func(cause error) error {
		if err := startAttempt(); err != nil {
			return err
		}
		return w.finish(ctx, job, execution, targetdb.WorkerResult{}, cause)
	}
	record, err := w.Targets.FindByID(ctx, job.TargetID)
	if err != nil || !record.Active || record.Driver != "postgres" {
		if err == nil {
			err = queue.ErrForbidden
		}
		if unresolved {
			err = targetdb.ErrUnknown
		}
		return failBeforeExecution(err)
	}
	conn, err := targetdb.OpenPostgres(ctx, targetdb.Config{Host: record.Host, Port: record.Port, Database: record.Database, Username: record.Username, SSLMode: record.SSLMode}, record.Password)
	if err != nil {
		if unresolved {
			err = targetdb.ErrUnknown
		}
		return failBeforeExecution(err)
	}
	defer conn.Close()
	conn.ObservePool(w.Metrics)
	lockCtx, lockSpan := observability.Start(ctx, "lock.acquire")
	lockStart := time.Now()
	session, err := conn.AcquireWorker(lockCtx)
	observability.End(lockSpan, err)
	if w.Metrics != nil {
		w.Metrics.LockWait.WithLabelValues(string(job.Action), string(job.Mode)).Observe(time.Since(lockStart).Seconds())
	}
	if err != nil {
		if errors.Is(err, targetdb.ErrWorkerBusy) {
			return w.deferJob(ctx, job, err)
		}
		if unresolved {
			err = targetdb.ErrUnknown
		}
		return failBeforeExecution(err)
	}
	defer session.Close()
	observability.Event(ctx, "lock_acquire", "SUCCESS")
	state, stateErr := session.State(ctx, job.ID)
	if errors.Is(stateErr, sql.ErrNoRows) {
		if err = session.CheckBlockers(ctx, execution); err != nil {
			if errors.Is(err, targetdb.ErrTargetBlocked) {
				return w.deferJob(ctx, job, err)
			}
			return failBeforeExecution(targetdb.ErrUnknown)
		}
	}
	if err = startAttempt(); err != nil {
		return err
	}
	if stateErr == nil {
		if err = w.syncProgress(ctx, job, execution, session, state); err != nil {
			return w.finish(ctx, job, execution, state, targetdb.ErrUnknown)
		}
		if state.Status == "SUCCESS" || state.Status == "REVERTED" {
			return w.finish(ctx, job, execution, state, nil)
		}
	} else if !errors.Is(stateErr, sql.ErrNoRows) {
		return w.finish(ctx, job, execution, state, targetdb.ErrUnknown)
	}
	if err = queue.Authorize(ctx, w.Queue.Pool, job.CreatedBy, job.TargetID); err == nil {
		err = queue.ValidateApproval(ctx, w.Queue.Pool, job.RequestID)
	}
	if err != nil {
		return w.finish(ctx, job, execution, state, err)
	}
	progress := func(result targetdb.WorkerResult) error {
		if err := queue.Authorize(ctx, w.Queue.Pool, job.CreatedBy, job.TargetID); err != nil {
			return err
		}
		if err := queue.ValidateApproval(ctx, w.Queue.Pool, job.RequestID); err != nil {
			return err
		}
		return w.syncProgress(ctx, job, execution, session, result)
	}
	if request.ExecutionMode == core.ExecutionBatch {
		state, err = session.ExecuteBatch(ctx, execution, progress)
	} else {
		state, err = session.ExecuteNormal(ctx, execution)
	}
	if err != nil {
		reconcileCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		recovered, lookupErr := session.State(reconcileCtx, job.ID)
		stop()
		if lookupErr == nil {
			state = recovered
			if state.Status == "SUCCESS" || state.Status == "REVERTED" {
				err = nil
			}
		} else if !errors.Is(lookupErr, sql.ErrNoRows) {
			err = targetdb.ErrUnknown
		}
	}
	syncCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	if syncErr := w.syncProgress(syncCtx, job, execution, session, state); syncErr != nil {
		err = targetdb.ErrUnknown
	}
	return w.finish(ctx, job, execution, state, err)
}
