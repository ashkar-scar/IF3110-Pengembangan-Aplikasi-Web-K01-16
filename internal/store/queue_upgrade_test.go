package store_test

import (
	"context"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"testing"
)

func TestUpgradeLegacyAttemptSchema(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `ALTER TABLE migration_attempts DROP COLUMN request_id,DROP COLUMN job_id,DROP COLUMN action_type,
 DROP COLUMN reverts_execution_id,DROP COLUMN attempt_no,DROP COLUMN batch_no;
 ALTER TABLE migration_attempts ADD UNIQUE(draft_id,draft_revision)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = store.SetupPlatform(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema()
 AND table_name='migration_attempts' AND column_name IN ('request_id','job_id','action_type','reverts_execution_id','attempt_no','batch_no')`).Scan(&count)
	if err != nil || count != 6 {
		t.Fatalf("columns=%d err=%v", count, err)
	}
	var unique bool
	err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='migration_attempts'::regclass AND conname='migration_attempts_draft_id_draft_revision_key')`).Scan(&unique)
	if err != nil || unique {
		t.Fatalf("legacy unique remains: %v %v", unique, err)
	}
}
