package worker_test

import (
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

func TestWorkerRejectsPostgresIdentifierAndFunctionBypasses(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE safe_rows(id int PRIMARY KEY,note text)`)
	for name, query := range map[string]string{
		"stacked ledger write": `INSERT INTO safe_rows VALUES(1,'first'); DELETE FROM public.U&"schema\005fmigrations"`,
		"worker state write":   `UPDATE U&"ruangdb\005finternal".jobs SET status='SUCCESS'`,
		"escaped function":     `SELECT U&"set\005fconfig"('search_path','ruangdb_internal',true)`,
		"nested dynamic SQL":   `SELECT query_to_xml('DELETE FROM public.schema_migrations RETURNING *',false,false,'')`,
	} {
		t.Run(name, func(t *testing.T) {
			f.run(f.enqueue(f.request(query, "SELECT 1", core.ExecutionNormal)), "FAILED")
			var rows, versions int
			if err := f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM safe_rows`).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if err := f.target.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM public.schema_migrations`).Scan(&versions); err != nil {
				t.Fatal(err)
			}
			if rows != 0 || versions != 0 {
				t.Fatalf("rejected SQL changed database: rows=%d versions=%d", rows, versions)
			}
		})
	}
}

func TestWorkerPreservesSQLLikeLiteralAsData(t *testing.T) {
	f := setup(t)
	f.sql(`CREATE TABLE messages(id int PRIMARY KEY,note text)`)
	query := `INSERT INTO messages VALUES (1, 'x''); DROP TABLE messages; --')`
	f.run(f.enqueue(f.request(query, `DELETE FROM messages WHERE id=1`, core.ExecutionNormal)), "SUCCESS")
	var note string
	if err := f.target.DB.QueryRowContext(f.ctx, `SELECT note FROM messages WHERE id=1`).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "x'); DROP TABLE messages; --" {
		t.Fatalf("literal changed: %q", note)
	}
}

func TestWorkerRejectsUnsafeOriginalDownWithoutChangingActiveMigration(t *testing.T) {
	f := setup(t)
	source := f.enqueue(f.request(`CREATE TABLE keep_table(id int)`, `DELETE FROM public.U&"schema\005fmigrations"`, core.ExecutionNormal))
	f.run(source, "SUCCESS")
	history, err := f.migrations.ListHistory(f.ctx, f.targetID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	execution := history[0].ID
	version, _ := f.target.Version(f.ctx)
	q, err := f.migrations.SubmitHistoryReview(f.ctx, f.targetID, execution, core.ActionRollback, f.actor, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.migrations.ApproveReview(f.ctx, f.targetID, q.ID, f.actor, ""); err != nil {
		t.Fatal(err)
	}
	f.run(f.enqueue(q), "FAILED")
	var active bool
	if err = f.target.DB.QueryRowContext(f.ctx, `SELECT is_active FROM public.schema_migrations WHERE attempt_id=$1`, execution).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("rejected SQL down changed active migration")
	}
}
