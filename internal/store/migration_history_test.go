package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// TestMigrationHistory memastikan execution yang tersimpan dapat dibaca kembali sebagai history
func TestMigrationHistory(t *testing.T) {
	fixture := newWorkflowTestFixture(t, 0)
	draft := createWorkflowDraft(t, fixture)
	insertWorkflowAttempt(t, fixture, draft, "execution-1", core.ActionUp, nil, time.Now().UTC())

	history, err := fixture.migrations.ListHistory(context.Background(), fixture.target.ID, false, 0)
	must(t, err)
	
	if len(history) != 1 || history[0].ID != "execution-1" || history[0].Status != core.AttemptSuccess {
		t.Fatalf("history tidak sesuai: %+v", history)
	}
}