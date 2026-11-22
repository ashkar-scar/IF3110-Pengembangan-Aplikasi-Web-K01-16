package worker_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/jackc/pgx/v5"
)

func TestCancelPartialBatchAfterExistingMigration(t *testing.T) {
	f := setup(t)
	f.run(f.enqueue(f.request("CREATE TABLE previous_migration(id int)", "DROP TABLE previous_migration", core.ExecutionNormal)), "SUCCESS")
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	id, err := f.service.CancelBatch(f.ctx, source, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := strconv.ParseInt(id, 10, 64)
	request, err := f.migrations.ApproveReview(f.ctx, f.targetID, requestID, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	f.run(f.enqueue(request), "REVERTED")
	var count int
	if err = f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM customers WHERE tier='SILVER'`).Scan(&count); err != nil || count != 6 {
		t.Fatalf("restored=%d err=%v", count, err)
	}
}

func TestOldWorkerCannotFinalizeReplacementOwnership(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE ownership_test(id int)`)
	id := f.enqueue(f.request("INSERT INTO ownership_test VALUES(1)", "DELETE FROM ownership_test WHERE id=1", core.ExecutionNormal))
	blocker, err := f.target.DB.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.ExecContext(f.ctx, `LOCK TABLE ownership_test IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.worker.RunOne(f.ctx); done <- err }()
	started := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		var count int
		if err = f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM migration_attempts WHERE job_id=$1 AND status='RUNNING'`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			started = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !started {
		t.Fatal("execution attempt did not start")
	}
	f.exec(`UPDATE migration_jobs SET locked_by='replacement-worker' WHERE id=$1`, id)
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, queue.ErrConflict) {
		t.Fatalf("stale owner error=%v", err)
	}
	var owner, status, requestStatus string
	if err = f.pool.QueryRow(f.ctx, `SELECT j.locked_by,j.status,r.status FROM migration_jobs j JOIN migration_requests r ON r.id=j.request_id WHERE j.id=$1`, id).Scan(&owner, &status, &requestStatus); err != nil {
		t.Fatal(err)
	}
	if owner != "replacement-worker" || status != "RUNNING" || requestStatus != "ENQUEUED" {
		t.Fatalf("stale owner changed replacement: %s %s %s", owner, status, requestStatus)
	}
	f.exec(`UPDATE migration_jobs SET lock_expires_at=now()-interval '1 second' WHERE id=$1`, id)
	f.run(id, "SUCCESS")
	var rows int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM ownership_test`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("double commit after ownership recovery: %d", rows)
	}
}

func TestUnqualifiedBatchCannotUpdateShadowTable(t *testing.T) {
	f := setup(t)
	var username string
	if err := f.target.DB.QueryRowContext(f.ctx, `SELECT current_user`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{username}.Sanitize()
	f.sql(`CREATE SCHEMA ` + schema + `; CREATE TABLE public.customers(id bigint PRIMARY KEY,tier text NOT NULL);
 INSERT INTO public.customers VALUES(1,'SILVER'),(2,'SILVER');
 CREATE TABLE ` + schema + `.customers(id bigint PRIMARY KEY,tier text NOT NULL);
 INSERT INTO ` + schema + `.customers VALUES(1,'SHADOW'),(2,'SHADOW')`)
	id := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(id, "SUCCESS")
	var publicGold, shadowUnchanged int
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.customers WHERE tier='GOLD'`).Scan(&publicGold)
	f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM `+schema+`.customers WHERE tier='SHADOW'`).Scan(&shadowUnchanged)
	if publicGold != 2 || shadowUnchanged != 2 {
		t.Fatalf("public gold=%d shadow unchanged=%d", publicGold, shadowUnchanged)
	}
}

func TestHideRollbackPreservesPartiallyRevertedExecution(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE customers(id bigint PRIMARY KEY,tier text NOT NULL); INSERT INTO customers SELECT i,'SILVER' FROM generate_series(1,6) i`)
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "SUCCESS")
	history, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	execution := ""
	for _, a := range history {
		if a.JobID != nil && *a.JobID == source && a.BatchNo == nil {
			execution = a.ID
		}
	}
	f.sql(`UPDATE customers SET tier='BRONZE' WHERE id=3`)
	version, _ := f.target.Version(f.ctx)
	q, err := f.migrations.SubmitHistoryReview(f.ctx, f.targetID, execution, core.ActionRollback, f.actor, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.migrations.ApproveReview(f.ctx, f.targetID, q.ID, f.actor, ""); err != nil {
		t.Fatal(err)
	}
	rollback := f.enqueue(q)
	f.run(rollback, "PARTIALLY_REVERTED")
	filtered, err := f.migrations.ListHistory(f.ctx, f.targetID, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range filtered {
		if a.ID == execution {
			found = true
		}
	}
	if !found {
		t.Fatal("partial rollback hid the source execution before the entire rollback committed")
	}
}

func TestPartialPhysicalDatabaseDefersAliasWithoutConsumingRetry(t *testing.T) {
	f := setup(t)
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	record, err := f.worker.Targets.FindByID(f.ctx, f.targetID)
	if err != nil {
		t.Fatal(err)
	}
	record.ID = 0
	record.Name = "alias of partial target"
	aliasRecord, err := f.worker.Targets.Create(f.ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO database_grants(group_id,database_id,permission) VALUES($1,$2,'EDIT')`, f.group, aliasRecord.ID)
	alias := *f
	alias.targetID = aliasRecord.ID
	aliasJob := alias.enqueue(alias.request("CREATE TABLE after_partial(id int)", "DROP TABLE after_partial", core.ExecutionNormal))
	alias.run(aliasJob, "PENDING")
	job, err := alias.service.Get(f.ctx, aliasJob)
	if err != nil {
		t.Fatal(err)
	}
	if job.RetryCount != 0 || job.NextRetryAt == nil {
		t.Fatalf("busy job consumed SQL retry budget: %+v", job)
	}
	var attempts int
	if err = f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM migration_attempts WHERE job_id=$1`, aliasJob).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("deferred SQL was recorded as executed: %d %v", attempts, err)
	}
	f.exec(`UPDATE migration_jobs SET next_retry_at=now()+interval '1 hour' WHERE id=$1`, aliasJob)
	requestID, err := f.service.CancelBatch(f.ctx, source, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.ParseInt(requestID, 10, 64)
	q, err := f.migrations.ApproveReview(f.ctx, f.targetID, n, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	f.run(f.enqueue(q), "REVERTED")
	f.exec(`UPDATE migration_jobs SET next_retry_at=now() WHERE id=$1`, aliasJob)
	alias.run(aliasJob, "SUCCESS")
}

func TestCancelBatchReportsMissingGroupLeader(t *testing.T) {
	f := setup(t)
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	f.exec(`UPDATE groups SET leader_id=NULL WHERE id=$1`, f.group)
	if _, err := f.service.CancelBatch(f.ctx, source, f.actor, ""); !errors.Is(err, queue.ErrNoGroupLeader) {
		t.Fatalf("missing leader error=%v", err)
	}
}

func TestPendingCancelPreventsSourceResumeUntilRejected(t *testing.T) {
	f := setup(t)
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	requestID, err := f.service.CancelBatch(f.ctx, source, f.actor, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Retry(f.ctx, source, f.actor, ""); !errors.Is(err, queue.ErrConflict) {
		t.Fatalf("source resumed under active cancellation review: %v", err)
	}
	job, err := f.service.Get(f.ctx, source)
	if err != nil || job.CanRetry {
		t.Fatalf("can_retry=%v err=%v", job.CanRetry, err)
	}
	id, _ := strconv.ParseInt(requestID, 10, 64)
	if _, err = f.migrations.RejectReview(f.ctx, f.targetID, id, f.actor, "continue the original migration"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Retry(f.ctx, source, f.actor, ""); err != nil {
		t.Fatal(err)
	}
}

func TestBatchHistoryPreservesExactDMLAndFailedBatch(t *testing.T) {
	f := setup(t)
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	history, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	committed, failed := 0, 0
	for _, a := range history {
		if a.JobID == nil || *a.JobID != source || a.BatchNo == nil {
			continue
		}
		if !strings.HasSuffix(a.SQL, `RETURNING "id"`) {
			t.Fatalf("batch history omitted actual RETURNING query: %q", a.SQL)
		}
		if a.Status == core.AttemptSuccess {
			committed++
		}
		if a.Status == core.AttemptFailed && *a.BatchNo == 3 {
			failed++
		}
	}
	if committed != 2 || failed != 1 {
		t.Fatalf("committed=%d failed batch=%d", committed, failed)
	}
}

func TestManualRetryRemainsAvailableAfterAutomaticBudget(t *testing.T) {
	f := setup(t)
	f.batchData()
	source := f.enqueue(f.request(batchUp, batchDown, core.ExecutionBatch))
	f.run(source, "PARTIAL")
	f.exec(`UPDATE migration_jobs SET retry_count=max_retries WHERE id=$1`, source)
	f.sql(`ALTER TABLE customers DROP CONSTRAINT customers_check`)
	if _, err := f.service.Retry(f.ctx, source, f.actor, ""); err != nil {
		t.Fatalf("manual recovery blocked by automatic retry budget: %v", err)
	}
	f.run(source, "SUCCESS")
}
