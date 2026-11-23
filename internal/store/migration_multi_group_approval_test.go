package store_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workflowTestFixture menyimpan data yang dipakai bersama oleh test workflow migration.
type workflowTestFixture struct {
	pool       *pgxpool.Pool
	migrations *store.MigrationRepository
	target     core.DatabaseTarget
	requester  core.User
	leaders    []core.User
}

// newWorkflowTestFixture membuat target database beserta group dan leader sesuai jumlah yang dibutuhkan.
func newWorkflowTestFixture(t *testing.T, groupCount int) workflowTestFixture {
	t.Helper()
	ctx := context.Background()
	pool := testutil.Platform(t)
	targets, err := store.NewTargetRepository(pool, bytes.Repeat([]byte{1}, 32))
	must(t, err)

	target, err := targets.Create(ctx, core.DatabaseTarget{
		Name: "workflow-test", Driver: "postgres", Host: "localhost", Port: 5432,
		Database: "workflow", Username: "target", Password: "password", SSLMode: "disable", Active: true,
	})

	must(t, err)

	fixture := workflowTestFixture{
		pool:       pool,
		migrations: store.NewMigrationRepository(pool),
		target:     target,
		requester:  user(t, pool, "requester"),
	}

	groups := store.NewGroupRepository(pool)
	grants := store.NewPermissionRepository(pool)
	for i := 0; i < groupCount; i++ {
		leader := user(t, pool, fmt.Sprintf("leader-%d", i))
		group, createErr := groups.Create(ctx, core.Group{Name: fmt.Sprintf("group-%d", i)})

		must(t, createErr)
		must(t, groups.AddMember(ctx, group.ID, leader.ID))

		_, setErr := groups.SetLeader(ctx, group.ID, leader.ID)

		must(t, setErr)
		must(t, grants.SetGrant(ctx, core.DatabaseGrant{GroupID: group.ID, DatabaseID: target.ID, Permission: core.PermissionEdit}))
		fixture.leaders = append(fixture.leaders, leader)
	}

	return fixture
}

// createWorkflowDraft membuat draft valid yang siap dipakai pada test workflow.
func createWorkflowDraft(t *testing.T, fixture workflowTestFixture) core.Draft {
	t.Helper()
	draft, err := fixture.migrations.CreateDraft(context.Background(), core.Draft{
		TargetID: fixture.target.ID, Description: "Tambah kolom email",
		SQLUp: "ALTER TABLE users ADD COLUMN email TEXT", SQLDown: "ALTER TABLE users DROP COLUMN email",
		ExecutionMode: core.ExecutionNormal, BatchSize: 1000, CreatedBy: fixture.requester.ID,
	})

	must(t, err)
	return draft
}

// insertWorkflowAttempt menyiapkan satu execution untuk test history, rollback, dan forward.
func insertWorkflowAttempt(t *testing.T, fixture workflowTestFixture, draft core.Draft, id string, action core.MigrationAction, revertsID *string, started time.Time) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO migration_attempts (
			id, target_id, draft_id, draft_revision, action_type, reverts_execution_id,
			description, sql_text, actor_id, actor_name, base_version, version,
			status, started_at, finished_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'SUCCESS',$13,$13)
	`, id, fixture.target.ID, draft.ID, draft.Revision, action, revertsID,
		draft.Description, draft.SQLUp, fixture.requester.ID, fixture.requester.Username,
		draft.BaseVersion, draft.BaseVersion+1, started)
		
	must(t, err)
}

// TestMultiGroupApproval memastikan request baru approved setelah semua group leader menyetujui
func TestMultiGroupApproval(t *testing.T) {
	fixture := newWorkflowTestFixture(t, 2)
	ctx := context.Background()
	draft := createWorkflowDraft(t, fixture)
	request, err := fixture.migrations.SubmitReview(ctx, fixture.target.ID, draft.ID, fixture.requester.ID)

	must(t, err)
	if len(request.Approvals) != 2 {
		t.Fatalf("jumlah approval = %d, ingin 2", len(request.Approvals))
	}

	request, err = fixture.migrations.ApproveReview(ctx, fixture.target.ID, request.ID, fixture.leaders[0].ID, "setuju")
	must(t, err)
	if request.Status != core.RequestPendingReview {
		t.Fatalf("status setelah satu approval = %s", request.Status)
	}

	request, err = fixture.migrations.ApproveReview(ctx, fixture.target.ID, request.ID, fixture.leaders[1].ID, "setuju")
	must(t, err)
	if request.Status != core.RequestApproved {
		t.Fatalf("status setelah semua approval = %s", request.Status)
	}
}