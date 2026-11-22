package worker

import (
	"errors"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		action  queue.ActionType
		batches int
		err     error
		want    string
	}{
		{queue.ActionUp, 0, nil, "SUCCESS"}, {queue.ActionUp, 0, errors.New("fail"), "FAILED"},
		{queue.ActionUp, 1, errors.New("fail"), "PARTIAL"}, {queue.ActionRollback, 1, errors.New("fail"), "PARTIALLY_REVERTED"},
		{queue.ActionCancelBatch, 1, nil, "REVERTED"}, {queue.ActionUp, 1, targetdb.ErrUnknown, "UNKNOWN"},
	} {
		if got := failureStatus(queue.Job{Action: tc.action}, targetdb.WorkerResult{CompletedBatches: tc.batches}, tc.err); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}
func TestRetryIsBoundedAndSelective(t *testing.T) {
	if !transient(&pgconn.PgError{Code: "40001"}) || transient(&pgconn.PgError{Code: "42601"}) || transient(targetdb.ErrUnknown) {
		t.Fatal("incorrect retry classification")
	}
	if backoff(0) != time.Second || backoff(100) > time.Minute*2 {
		t.Fatal("unbounded retry delay")
	}
}
