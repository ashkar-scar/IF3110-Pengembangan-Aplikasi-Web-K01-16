package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// TestRollbackAndForward memastikan execution UP dapat diajukan untuk rollback lalu forward.
func TestRollbackAndForward(t *testing.T) {
	fixture := newWorkflowTestFixture(t, 1)
	draft := createWorkflowDraft(t, fixture)
	ctx := context.Background()
	upID := "execution-up"
	
	insertWorkflowAttempt(t, fixture, draft, upID, core.ActionUp, nil, time.Now().UTC())

	rollback, err := fixture.migrations.SubmitHistoryReview(ctx, fixture.target.ID, upID, core.ActionRollback, fixture.requester.ID, 1)
	must(t, err)

	if rollback.Action != core.ActionRollback || rollback.TargetExecutionID == nil || *rollback.TargetExecutionID != upID {
		t.Fatalf("request rollback tidak sesuai: %+v", rollback)
	}

	insertWorkflowAttempt(t, fixture, draft, "execution-rollback", core.ActionRollback, &upID, time.Now().UTC().Add(time.Second))
	forward, err := fixture.migrations.SubmitHistoryReview(ctx, fixture.target.ID, upID, core.ActionForward, fixture.requester.ID, 2)
	must(t, err)

	if forward.Action != core.ActionForward || forward.TargetExecutionID == nil || *forward.TargetExecutionID != upID {
		t.Fatalf("request forward tidak sesuai: %+v", forward)
	}
}