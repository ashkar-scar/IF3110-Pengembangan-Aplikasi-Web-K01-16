package worker_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testdb"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	t                      *testing.T
	ctx                    context.Context
	pool                   *pgxpool.Pool
	target                 *targetdb.Connection
	migrations             *store.MigrationRepository
	service                *queue.Service
	worker                 *worker.Worker
	actor, targetID, group int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testutil.Platform(t)
	config, err := pgx.ParseConfig(testdb.Target(t))
	if err != nil {
		t.Fatal(err)
	}
	targets, err := store.NewTargetRepository(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	target, err := targets.Create(ctx, core.DatabaseTarget{Name: "worker target", Driver: "postgres", Host: config.Host, Port: int(config.Port), Database: config.Database, Username: config.User, Password: config.Password, SSLMode: "disable", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := targetdb.OpenPostgres(ctx, targetdb.Config{Host: target.Host, Port: target.Port, Database: target.Database, Username: target.Username, SSLMode: target.SSLMode}, config.Password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	f := &fixture{t: t, ctx: ctx, pool: pool, target: conn, targetID: target.ID, migrations: store.NewMigrationRepository(pool), service: queue.NewService(queue.NewRepository(pool))}
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('worker-editor','unused') RETURNING id`).Scan(&f.actor); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO groups(name,leader_id) VALUES('editors',$1) RETURNING id`, f.actor).Scan(&f.group); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO group_members(group_id,user_id) VALUES($1,$2)`, f.group, f.actor)
	f.exec(`INSERT INTO database_grants(group_id,database_id,permission) VALUES($1,$2,'EDIT')`, f.group, f.targetID)
	f.worker = worker.New(f.service.Repository, targets, observability.NewMetrics(pool))
	f.worker.PollInterval = 10 * time.Millisecond
	return f
}
func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, query, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) sql(query string, args ...any) {
	f.t.Helper()
	if _, err := f.target.DB.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) request(up, down string, mode core.ExecutionMode) core.MigrationRequest {
	f.t.Helper()
	version, err := f.target.Version(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	d, err := f.migrations.CreateDraft(f.ctx, core.Draft{TargetID: f.targetID, Description: "integration migration", SQLUp: up, SQLDown: down, ExecutionMode: mode, BatchColumn: "id", BatchSize: 2, CreatedBy: f.actor, BaseVersion: version})
	if err != nil {
		f.t.Fatal(err)
	}
	q, err := f.migrations.SubmitReview(f.ctx, f.targetID, d.ID, f.actor)
	if err != nil {
		f.t.Fatal(err)
	}
	q, err = f.migrations.ApproveReview(f.ctx, f.targetID, q.ID, f.actor, "")
	if err != nil {
		f.t.Fatal(err)
	}
	q, err = f.service.Request(f.ctx, q.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return q
}
func (f *fixture) enqueue(q core.MigrationRequest) string {
	f.t.Helper()
	q, lookupErr := f.service.Request(f.ctx, q.ID)
	if lookupErr != nil {
		f.t.Fatal(lookupErr)
	}
	id, err := f.service.Enqueue(f.ctx, queue.EnqueueRequest{RequestID: q.ID, TargetID: f.targetID, ActorID: f.actor, ActorName: "worker-editor", Action: queue.ActionType(q.Action)})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}
func (f *fixture) run(id, want string) {
	f.t.Helper()
	worked, err := f.worker.RunOne(f.ctx)
	j, e := f.service.Get(f.ctx, id)
	if e != nil || !worked || j.Status != want {
		f.t.Fatalf("worked=%v err=%v job=%+v lookup=%v want=%s", worked, err, j, e, want)
	}
}

func TestNormalRollbackForwardAndDuplicate(t *testing.T) {
	f := setup(t)
	q := f.request("CREATE TABLE products(id bigint primary key)", "DROP TABLE products", core.ExecutionNormal)
	id := f.enqueue(q)
	if again := f.enqueue(q); again != id {
		t.Fatal("duplicate job")
	}
	f.run(id, "SUCCESS")
	attempts, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	execution := attempts[0].ID
	for _, action := range []core.MigrationAction{core.ActionRollback, core.ActionForward} {
		version, _ := f.target.Version(f.ctx)
		q, err = f.migrations.SubmitHistoryReview(f.ctx, f.targetID, execution, action, f.actor, version)
		if err != nil {
			t.Fatal(err)
		}
		q, err = f.migrations.ApproveReview(f.ctx, f.targetID, q.ID, f.actor, "")
		if err != nil {
			t.Fatal(err)
		}
		f.run(f.enqueue(q), "SUCCESS")
	}
	var count int
	if err = f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("ledger=%d err=%v", count, err)
	}
	history, err := f.migrations.ListHistory(f.ctx, f.targetID, true, 0)
	if err != nil || len(history) != 1 || history[0].Action != core.ActionForward {
		t.Fatalf("hide rollback: %+v %v", history, err)
	}
}

const batchUp = `UPDATE customers SET tier = 'GOLD' WHERE id IN (SELECT id FROM customers WHERE id > $1 ORDER BY id LIMIT $2)`
const batchDown = `UPDATE customers SET tier = 'SILVER' WHERE id IN (SELECT id FROM customers WHERE id > $1 AND id <= $2 ORDER BY id LIMIT $3)`

func (f *fixture) batchData() {
	f.sql(`CREATE TABLE customers(id bigint PRIMARY KEY,tier text NOT NULL CHECK(id<5 OR tier<>'GOLD')); INSERT INTO customers SELECT i,'SILVER' FROM generate_series(1,6) i`)
}

func TestBatchPartialResumeAndCancel(t *testing.T) {
	f := setup(t)
	f.batchData()
	id := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(id, "PARTIAL")
	j, _ := f.service.Get(f.ctx, id)
	if j.RowsProcessed != 4 || j.CompletedBatches != 2 {
		t.Fatalf("bad checkpoint %+v", j)
	}
	other := f.enqueue(f.request("CREATE TABLE waiting(id int)", "DROP TABLE waiting", core.ExecutionNormal))
	if worked, err := f.worker.RunOne(f.ctx); worked || err != nil {
		t.Fatalf("partial did not block next job: %v %v", worked, err)
	}
	f.sql(`ALTER TABLE customers DROP CONSTRAINT customers_check`)
	if _, err := f.service.Retry(f.ctx, id, f.actor, ""); err != nil {
		t.Fatal(err)
	}
	f.run(id, "SUCCESS")
	j, _ = f.service.Get(f.ctx, id)
	if j.RowsProcessed != 6 || len(j.Checkpoints) != 3 {
		t.Fatalf("duplicate batches %+v", j)
	}
	// Remove the waiting job so the test can submit the inverse of the completed batch.
	f.exec(`UPDATE migration_jobs SET status='FAILED' WHERE id=$1`, other)
	cancel, err := f.service.CancelBatch(f.ctx, id, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := strconv.ParseInt(cancel, 10, 64)
	q, err := f.migrations.ApproveReview(f.ctx, f.targetID, reqID, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	f.run(f.enqueue(q), "REVERTED")
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM customers WHERE tier='SILVER'`).Scan(&count)
	if count != 6 {
		t.Fatalf("restored=%d", count)
	}
}

func TestApprovalAndPermissionRevalidation(t *testing.T) {
	f := setup(t)
	q := f.request("CREATE TABLE forbidden(id int)", "DROP TABLE forbidden", core.ExecutionNormal)
	if _, err := f.service.Enqueue(f.ctx, queue.EnqueueRequest{RequestID: q.ID, TargetID: f.targetID, ActorID: 999, Action: queue.ActionUp}); !errors.Is(err, queue.ErrForbidden) {
		t.Fatal(err)
	}
	id := f.enqueue(q)
	f.exec(`UPDATE migration_approvals SET decision='PENDING' WHERE request_id=$1`, q.ID)
	f.run(id, "FAILED")
	var table *string
	f.target.DB.QueryRowContext(f.ctx, `SELECT to_regclass('public.forbidden')::text`).Scan(&table)
	if table != nil {
		t.Fatal("unapproved SQL executed")
	}
	if _, err := f.service.Retry(f.ctx, id, f.actor, ""); !errors.Is(err, queue.ErrApproval) {
		t.Fatal(err)
	}
}

func TestRecoveryUsesTargetProof(t *testing.T) {
	f := setup(t)
	q := f.request("CREATE TABLE recovered(id int)", "DROP TABLE recovered", core.ExecutionNormal)
	id := f.enqueue(q)
	session, err := f.target.AcquireWorker(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	e := targetdb.WorkerExecution{JobID: id, ExecutionID: "exec_" + id + "_1", Request: q, ActorID: f.actor, ActorName: "worker-editor"}
	if _, err = session.ExecuteNormal(f.ctx, e); err != nil {
		t.Fatal(err)
	}
	session.Close()
	f.exec(`UPDATE migration_jobs SET status='RUNNING',locked_by='dead',lock_expires_at=now()-interval '1 second' WHERE id=$1`, id)
	f.run(id, "SUCCESS")
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count)
	if count != 1 {
		t.Fatal("replayed committed SQL")
	}
}

func TestWorkersSerializeSameTargetAndParallelizeDifferentTargets(t *testing.T) {
	f := setup(t)
	q := f.request("SELECT 1", "SELECT 1", core.ExecutionNormal)
	// A held target lock simulates another live worker, including a pause between batches.
	session, err := f.target.AcquireWorker(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := f.enqueue(q)
	f.run(id, "PENDING")
	session.Close()
	f.exec(`UPDATE migration_jobs SET next_retry_at=now() WHERE id=$1`, id)
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count)
	if count != 0 {
		t.Fatal("lock bypassed")
	}
	f2 := setup(t)
	id2 := f2.enqueue(f2.request("CREATE TABLE parallel(id int)", "DROP TABLE parallel", core.ExecutionNormal))
	var wg sync.WaitGroup
	wg.Add(2)
	for _, task := range []struct {
		f  *fixture
		id string
	}{{f, id}, {f2, id2}} {
		go func() {
			defer wg.Done()
			task.f.worker.RunOne(task.f.ctx)
		}()
	}
	wg.Wait()
	j, _ := f2.service.Get(f2.ctx, id2)
	if j.Status != "SUCCESS" {
		t.Fatal(fmt.Sprintf("other target: %+v", j))
	}
}

func TestConcurrentWorkersClaimOneJobOnce(t *testing.T) {
	f := setup(t)
	id := f.enqueue(f.request("CREATE TABLE once_only(id bigint primary key)", "DROP TABLE once_only", core.ExecutionNormal))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.worker.RunOne(f.ctx) }()
	}
	wg.Wait()
	j, err := f.service.Get(f.ctx, id)
	if err != nil || j.Status != "SUCCESS" {
		t.Fatalf("%+v %v", j, err)
	}
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count)
	if count != 1 {
		t.Fatalf("commits=%d", count)
	}
}

func TestResumeAfterWorkerRestartUsesAtomicCheckpoint(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE customers(id bigint PRIMARY KEY,tier text NOT NULL); INSERT INTO customers SELECT i,'SILVER' FROM generate_series(1,6) i`)
	q := f.request(batchUp, batchDown, core.ExecutionBatch)
	id := f.enqueue(q)
	session, err := f.target.AcquireWorker(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	e := targetdb.WorkerExecution{JobID: id, ExecutionID: "exec_" + id + "_1", Request: q, ActorID: f.actor, ActorName: "worker-editor"}
	_, err = session.ExecuteBatch(f.ctx, e, func(state targetdb.WorkerResult) error {
		if state.CompletedBatches == 1 {
			return errors.New("simulated worker termination")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected interruption")
	}
	session.Close()
	f.exec(`UPDATE migration_jobs SET status='RUNNING',locked_by='dead-worker',lock_expires_at=now()-interval '1 second' WHERE id=$1`, id)
	f.run(id, "SUCCESS")
	j, _ := f.service.Get(f.ctx, id)
	if j.RowsProcessed != 6 || len(j.Checkpoints) != 3 {
		t.Fatalf("checkpoint replay: %+v", j)
	}
}

func TestCancelPartiallyRevertedCanResume(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE customers(id bigint PRIMARY KEY,tier text NOT NULL); INSERT INTO customers SELECT i,'SILVER' FROM generate_series(1,6) i`)
	id := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(id, "SUCCESS")
	f.sql(`UPDATE customers SET tier='BRONZE' WHERE id=3`)
	requestID, err := f.service.CancelBatch(f.ctx, id, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.ParseInt(requestID, 10, 64)
	q, err := f.migrations.ApproveReview(f.ctx, f.targetID, n, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	cancelID := f.enqueue(q)
	f.run(cancelID, "PARTIALLY_REVERTED")
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM customers WHERE tier='SILVER'`).Scan(&count)
	if count != 2 {
		t.Fatalf("reverse order not preserved: %d", count)
	}
	f.sql(`UPDATE customers SET tier='GOLD' WHERE id=3`)
	if _, err = f.service.Retry(f.ctx, cancelID, f.actor, ""); err != nil {
		t.Fatal(err)
	}
	f.run(cancelID, "REVERTED")
}

func TestWorkerRejectsStaleRequest(t *testing.T) {
	f := setup(t)
	first := f.request("CREATE TABLE first_one(id int)", "DROP TABLE first_one", core.ExecutionNormal)
	stale := f.request("CREATE TABLE stale_one(id int)", "DROP TABLE stale_one", core.ExecutionNormal)
	f.run(f.enqueue(first), "SUCCESS")
	f.run(f.enqueue(stale), "FAILED")
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count)
	if count != 1 {
		t.Fatal("stale request committed")
	}
}

func TestAutomaticRetriesAreLimitedAndPreserveAttempts(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE retry_rows(id int);
 CREATE FUNCTION fail_temporarily() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'temporary' USING ERRCODE='40001'; END$$;
 CREATE TRIGGER transient BEFORE INSERT ON retry_rows FOR EACH ROW EXECUTE FUNCTION fail_temporarily()`)
	id := f.enqueue(f.request("INSERT INTO retry_rows VALUES(1)", "DELETE FROM retry_rows WHERE id=1", core.ExecutionNormal))
	for i := 0; i < 4; i++ {
		want := "PENDING"
		if i == 3 {
			want = "FAILED"
		}
		f.run(id, want)
		if want == "PENDING" {
			job, err := f.service.Get(f.ctx, id)
			if err != nil || job.FinishedAt != nil {
				t.Fatalf("pending retry marked finished: %+v %v", job, err)
			}
		}
		f.exec(`UPDATE migration_jobs SET next_retry_at=now() WHERE id=$1`, id)
	}
	j, _ := f.service.Get(f.ctx, id)
	if j.RetryCount != 3 {
		t.Fatalf("retry_count=%d", j.RetryCount)
	}
	history, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
	if err != nil || len(history) != 4 {
		t.Fatalf("attempts=%d err=%v", len(history), err)
	}
}

func TestExistingM1TargetLedgerIsUpgraded(t *testing.T) {
	f := setup(t)
	_, err := f.target.Execute(f.ctx, targetdb.Execution{ID: "m1_seed", SQL: "CREATE TABLE legacy_data(id int)", ActorID: f.actor, ActorName: "worker-editor"})
	if err != nil {
		t.Fatal(err)
	}
	f.run(f.enqueue(f.request("CREATE TABLE new_data(id int)", "DROP TABLE new_data", core.ExecutionNormal)), "SUCCESS")
	var count int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&count)
	if count != 2 {
		t.Fatal("M1 ledger not preserved")
	}
}

func TestMultipleRollbackAndForwardRemainLinear(t *testing.T) {
	f := setup(t)
	executions := []string{}
	for _, name := range []string{"a", "b", "c"} {
		id := f.enqueue(f.request("CREATE TABLE "+name+"(id int)", "DROP TABLE "+name, core.ExecutionNormal))
		f.run(id, "SUCCESS")
		history, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		executions = append(executions, history[0].ID)
	}
	for _, step := range []struct {
		action    core.MigrationAction
		execution string
	}{{core.ActionRollback, executions[2]}, {core.ActionRollback, executions[1]}, {core.ActionForward, executions[1]}, {core.ActionForward, executions[2]}} {
		version, _ := f.target.Version(f.ctx)
		request, err := f.migrations.SubmitHistoryReview(f.ctx, f.targetID, step.execution, step.action, f.actor, version)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.migrations.ApproveReview(f.ctx, f.targetID, request.ID, f.actor, ""); err != nil {
			t.Fatal(err)
		}
		f.run(f.enqueue(request), "SUCCESS")
	}
}

func TestOneWorkerProcessesDifferentTargetsInParallel(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE slow_rows(id int)`)
	config, err := pgx.ParseConfig(testdb.Target(t))
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.worker.Targets.Create(f.ctx, core.DatabaseTarget{Name: "second target", Driver: "postgres", Host: config.Host, Port: int(config.Port), Database: config.Database, Username: config.User, Password: config.Password, SSLMode: "disable", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	otherConn, err := targetdb.OpenPostgres(f.ctx, targetdb.Config{Host: record.Host, Port: record.Port, Database: record.Database, Username: record.Username, SSLMode: "disable"}, config.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer otherConn.Close()
	other := *f
	other.target = otherConn
	other.targetID = record.ID
	f.exec(`INSERT INTO database_grants(group_id,database_id,permission) VALUES($1,$2,'EDIT')`, f.group, record.ID)
	firstID := f.enqueue(f.request("INSERT INTO slow_rows VALUES(1)", "DELETE FROM slow_rows WHERE id=1", core.ExecutionNormal))
	secondID := other.enqueue(other.request("CREATE TABLE independent(id int)", "DROP TABLE independent", core.ExecutionNormal))
	blocker, err := f.target.DB.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.ExecContext(f.ctx, `LOCK TABLE slow_rows IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.worker.RunOne(f.ctx); done <- err }()
	running := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		j, err := f.service.Get(f.ctx, firstID)
		if err == nil && j.Status == "RUNNING" {
			running = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !running {
		t.Fatal("first job was not picked up")
	}
	other.run(secondID, "SUCCESS")
	select {
	case err := <-done:
		t.Fatalf("first job ended before table lock was released: %v", err)
	default:
	}
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	j, _ := f.service.Get(f.ctx, firstID)
	if j.Status != "SUCCESS" {
		t.Fatal(j.Status)
	}
}
