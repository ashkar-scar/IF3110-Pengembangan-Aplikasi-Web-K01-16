package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

// TestInvalidSQLDoesNotCreateReviewOrLockDraft memastikan SQL invalid ditolak sebelum masuk review.
func TestInvalidSQLDoesNotCreateReviewOrLockDraft(t *testing.T) {
	tests := []struct {
		name    string
		sqlUp   string
		sqlDown string
	}{
		{name: "invalid SQL up", sqlUp: "bukan sql", sqlDown: "DROP TABLE users"},
		{name: "invalid SQL down", sqlUp: "CREATE TABLE users (id BIGINT)", sqlDown: "r"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkflowTestFixture(t, 1)
			draft, err := fixture.migrations.CreateDraft(context.Background(), core.Draft{
				TargetID: fixture.target.ID, Description: "SQL invalid",
				SQLUp: test.sqlUp, SQLDown: test.sqlDown,
				ExecutionMode: core.ExecutionNormal, BatchSize: 1000, CreatedBy: fixture.requester.ID,
			})
			
			must(t, err)

			_, err = fixture.migrations.SubmitReview(context.Background(), fixture.target.ID, draft.ID, fixture.requester.ID)
			if !errors.Is(err, store.ErrDraftInvalid) {
				t.Fatalf("error submit review = %v, ingin ErrDraftInvalid", err)
			}

			saved, err := fixture.migrations.GetDraft(context.Background(), fixture.target.ID, draft.ID)
			must(t, err)

			if saved.IsLocked {
				t.Fatal("draft invalid ikut terkunci")
			}

			reviews, err := fixture.migrations.ListReviews(context.Background(), fixture.target.ID, fixture.requester.ID)
			must(t, err)
			if len(reviews) != 0 {
				t.Fatalf("review invalid ikut dibuat: %+v", reviews)
			}
		})
	}
}