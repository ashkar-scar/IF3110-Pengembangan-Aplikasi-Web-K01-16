package targetdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

//go:embed worker_schema.sql
var workerSchema string

var ErrLinear = errors.New("posisi linear atau snapshot migration tidak sesuai")
var ErrBatchUnsafe = errors.New("batch tidak dapat diproses atau dibatalkan dengan aman")
var ErrTargetBlocked = errors.New("database masih memiliki batch atau hasil eksekusi yang belum diselesaikan")
var ErrWorkerBusy = errors.New("target sedang dikunci worker lain")

type WorkerExecution struct {
	JobID       string
	ExecutionID string
	SourceJobID string
	Request     core.MigrationRequest
	ActorID     int64
	ActorName   string
}

type WorkerResult struct {
	ExecutionID      string
	Status           string
	Version          int64
	TotalBatches     int
	CompletedBatches int
	RowsProcessed    int64
	LastCursor       int64
}

type WorkerSession struct {
	connection *sql.Conn
	owner      *Connection
	ctx        context.Context
}

func (conn *Connection) AcquireWorker(ctx context.Context) (*WorkerSession, error) {
	if conn.Driver != "postgres" {
		return nil, errors.New("worker hanya mendukung PostgreSQL")
	}
	session, err := conn.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	err = session.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(31103110)").Scan(&acquired)
	if err != nil {
		session.Raw(func(any) error { return driver.ErrBadConn })
		session.Close()
		return nil, err
	}
	if !acquired {
		session.Close()
		return nil, ErrWorkerBusy
	}
	if _, err = session.ExecContext(ctx, workerSchema); err != nil {
		cleanup := &WorkerSession{session, conn, ctx}
		cleanup.Close()
		return nil, err
	}
	return &WorkerSession{session, conn, ctx}, nil
}

func (s *WorkerSession) Close() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	if _, err := s.connection.ExecContext(ctx, "SELECT pg_advisory_unlock(31103110)"); err != nil {
		s.connection.Raw(func(any) error { return driver.ErrBadConn })
		observability.Event(ctx, "lock_release", "FAILED")
	} else {
		observability.Event(ctx, "lock_release", "SUCCESS")
	}
	s.connection.Close()
}

func (s *WorkerSession) State(ctx context.Context, jobID string) (WorkerResult, error) {
	var r WorkerResult
	err := s.connection.QueryRowContext(ctx, `SELECT execution_id,status,version,total_batches,completed_batches,rows_processed,last_cursor
 FROM ruangdb_internal.jobs WHERE job_id=$1`, jobID).Scan(&r.ExecutionID, &r.Status, &r.Version, &r.TotalBatches, &r.CompletedBatches, &r.RowsProcessed, &r.LastCursor)
	return r, err
}

func (s *WorkerSession) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='120s'"); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func workerVersion(ctx context.Context, tx *sql.Tx) (int64, error) {
	var version int64
	err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM public.schema_migrations").Scan(&version)
	return version, err
}

func activeExecution(ctx context.Context, tx *sql.Tx) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT attempt_id FROM public.schema_migrations WHERE is_active AND action_type IN ('UP','FORWARD') ORDER BY version DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return id, err
}

func (s *WorkerSession) validate(ctx context.Context, tx *sql.Tx, e WorkerExecution) (string, error) {
	if err := checkTargetBlockers(ctx, tx, e); err != nil {
		return "", err
	}
	q := e.Request
	version, err := workerVersion(ctx, tx)
	if err != nil {
		return "", err
	}
	if version != q.BaseVersion {
		return "", ErrVersion
	}
	parent, err := activeExecution(ctx, tx)
	if err != nil {
		return "", err
	}
	if q.Action == core.ActionUp {
		if q.Fingerprint != "" {
			snapshot, err := s.owner.snapshot(ctx, tx)
			if err != nil {
				return "", err
			}
			if snapshot.Fingerprint() != q.Fingerprint {
				return "", ErrDrift
			}
		}
		return parent, nil
	}
	if q.TargetExecutionID == nil || q.IsIrreversible {
		return "", ErrLinear
	}
	var up, down, oldParent string
	var irreversible bool
	var draft int64
	var revision int
	err = tx.QueryRowContext(ctx, `SELECT sql_up,sql_down,irreversible,parent_execution_id,draft_id,draft_revision FROM ruangdb_internal.jobs
 WHERE execution_id=$1 AND status IN ('SUCCESS','REVERTED') AND action_type IN ('UP','FORWARD')`, *q.TargetExecutionID).Scan(&up, &down, &irreversible, &oldParent, &draft, &revision)
	if err != nil || irreversible || up != q.SQLSnapshotUp || down != q.SQLSnapshotDown || draft != q.DraftID || revision != q.DraftRevision {
		return "", ErrLinear
	}
	if q.Action == core.ActionRollback {
		if parent != *q.TargetExecutionID {
			return "", ErrLinear
		}
		return oldParent, nil
	}
	parentLineage, err := canonicalExecution(ctx, tx, parent)
	if err != nil {
		return "", err
	}
	oldLineage, err := canonicalExecution(ctx, tx, oldParent)
	if err != nil {
		return "", err
	}
	if q.Action != core.ActionForward || parentLineage != oldLineage {
		return "", ErrLinear
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.reverts_attempt_id,j.parent_execution_id FROM public.schema_migrations r
 JOIN ruangdb_internal.jobs j ON j.execution_id=r.reverts_attempt_id
	WHERE r.action_type='ROLLBACK' ORDER BY r.version DESC`)
	if err != nil {
		return "", err
	}
	type rollback struct{ id, parent string }
	candidates := []rollback{}
	for rows.Next() {
		var candidate rollback
		if err = rows.Scan(&candidate.id, &candidate.parent); err != nil {
			rows.Close()
			return "", err
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		lineage, err := canonicalExecution(ctx, tx, candidate.parent)
		if err != nil {
			return "", err
		}
		if lineage == parentLineage {
			if candidate.id != *q.TargetExecutionID {
				return "", ErrLinear
			}
			return parent, nil
		}
	}
	return "", ErrLinear
}

func canonicalExecution(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	var canonical string
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE chain AS (
 SELECT attempt_id,action_type,reverts_attempt_id FROM public.schema_migrations WHERE attempt_id=$1
 UNION ALL SELECT m.attempt_id,m.action_type,m.reverts_attempt_id FROM public.schema_migrations m
 JOIN chain c ON m.attempt_id=c.reverts_attempt_id WHERE c.action_type='FORWARD')
 SELECT attempt_id FROM chain WHERE action_type='UP' LIMIT 1`, id).Scan(&canonical)
	return canonical, err
}

func checkTargetBlockers(ctx context.Context, tx queryer, e WorkerExecution) error {
	var blocked bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ruangdb_internal.jobs
 WHERE status='RUNNING' AND job_id<>$1 AND job_id<>$2)`, e.JobID, e.SourceJobID).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return ErrTargetBlocked
	}
	return nil
}

func (s *WorkerSession) CheckBlockers(ctx context.Context, e WorkerExecution) error {
	return checkTargetBlockers(ctx, s.connection, e)
}

func insertWorkerState(ctx context.Context, tx *sql.Tx, e WorkerExecution, parent string) error {
	q := e.Request
	_, err := tx.ExecContext(ctx, `INSERT INTO ruangdb_internal.jobs(job_id,execution_id,draft_id,draft_revision,action_type,sql_up,sql_down,irreversible,parent_execution_id,source_job_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, e.JobID, e.ExecutionID, q.DraftID, q.DraftRevision, q.Action, q.SQLSnapshotUp, q.SQLSnapshotDown, q.IsIrreversible, parent, e.SourceJobID)
	return err
}

func finishLedger(ctx context.Context, tx *sql.Tx, e WorkerExecution) (int64, error) {
	version, err := workerVersion(ctx, tx)
	if err != nil {
		return 0, err
	}
	version++
	q := e.Request
	query := q.SQLSnapshotUp
	active := true
	if q.Action == core.ActionRollback {
		query = q.SQLSnapshotDown
		active = false
		if _, err = tx.ExecContext(ctx, `UPDATE public.schema_migrations SET is_active=false WHERE attempt_id=$1 AND is_active`, q.TargetExecutionID); err != nil {
			return 0, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.schema_migrations(version,attempt_id,sql_text,actor_id,actor_name,draft_id,draft_revision,action_type,reverts_attempt_id,is_active)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, version, e.ExecutionID, query, e.ActorID, e.ActorName, q.DraftID, q.DraftRevision, q.Action, q.TargetExecutionID, active)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET status='SUCCESS',version=$2,execution_id=$3 WHERE job_id=$1`, e.JobID, version, e.ExecutionID)
	return version, err
}

func (s *WorkerSession) ExecuteNormal(ctx context.Context, e WorkerExecution) (WorkerResult, error) {
	if result, err := s.State(ctx, e.JobID); err == nil {
		return result, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	query := e.Request.SQLSnapshotUp
	if e.Request.Action == core.ActionRollback {
		query = e.Request.SQLSnapshotDown
	}
	statements, err := sqlguard.PostgresMigration(query)
	if err != nil {
		return WorkerResult{}, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return WorkerResult{}, err
	}
	defer tx.Rollback()
	parent, err := s.validate(ctx, tx, e)
	if err != nil {
		return WorkerResult{}, err
	}
	if err = insertWorkerState(ctx, tx, e, parent); err != nil {
		return WorkerResult{}, err
	}
	var rows int64
	for _, statement := range statements {
		result, err := executePostgresStatement(ctx, tx, statement)
		if err != nil {
			return WorkerResult{}, err
		}
		n, _ := result.RowsAffected()
		rows += n
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET rows_processed=$2 WHERE job_id=$1`, e.JobID, rows); err != nil {
		return WorkerResult{}, err
	}
	version, err := finishLedger(ctx, tx, e)
	if err != nil {
		return WorkerResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkerResult{}, ErrUnknown
	}
	observability.Event(ctx, "migration_commit", "SUCCESS", "execution_id", e.ExecutionID, "version", version)
	return s.State(ctx, e.JobID)
}

func WorkerError(err error) string {
	for _, safe := range []error{ErrVersion, ErrDrift, ErrUnknown, ErrLinear, ErrBatchUnsafe, ErrTargetBlocked, ErrWorkerBusy} {
		if errors.Is(err, safe) {
			return safe.Error()
		}
	}
	return observability.SafeError(err)
}

func (s *WorkerSession) Checkpoints(ctx context.Context, jobID string) ([]BatchCheckpoint, error) {
	rows, err := s.connection.QueryContext(ctx, `SELECT batch_no,cursor_start,cursor_end,rows_affected,status,committed_at,reverted_at
 FROM ruangdb_internal.batches WHERE job_id=$1 ORDER BY batch_no`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BatchCheckpoint{}
	for rows.Next() {
		var c BatchCheckpoint
		if err = rows.Scan(&c.BatchNo, &c.Start, &c.End, &c.Rows, &c.Status, &c.CommittedAt, &c.RevertedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func executionReference(e WorkerExecution) string {
	if e.Request.TargetExecutionID == nil {
		return ""
	}
	return *e.Request.TargetExecutionID
}
func batchError(reason string) error { return fmt.Errorf("%w: %s", ErrBatchUnsafe, reason) }
