package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
)

// queueStub merekam payload enqueue tanpa membutuhkan worker queue asli.
type queueStub struct {
	request queue.EnqueueRequest
}

// Enqueue menyimpan request dan mengembalikan job ID simulasi.
func (stub *queueStub) Enqueue(_ context.Context, request queue.EnqueueRequest) (string, error) {
	stub.request = request
	return "job-1", nil
}

// Retry memenuhi kontrak JobEnqueuer tetapi tidak digunakan pada test enqueue.
func (stub *queueStub) Retry(context.Context, string, int64, string) (string, error) {
	return "", nil
}

// CancelBatch memenuhi kontrak JobEnqueuer tetapi tidak digunakan pada test enqueue.
func (stub *queueStub) CancelBatch(context.Context, string, int64, string) (string, error) {
	return "", nil
}

// TestEnqueueApprovedReview menguji request approved dikirim ke queue lalu berubah menjadi ENQUEUED.
func TestEnqueueApprovedReview(t *testing.T) {
	// Siapkan platform database dan user requester serta leader.
	pool := testutil.Platform(t)
	ctx := context.Background()
	users := store.NewUserRepository(pool)
	requester, err := users.Create(ctx, core.User{Username: "requester", PasswordHash: "unused", Active: true})

	if err != nil {
		t.Fatal(err)
	}

	leader, err := users.Create(ctx, core.User{Username: "leader", PasswordHash: "unused", Active: true})
	if err != nil {
		t.Fatal(err)
	}

	// Buat database target platform yang akan menerima migration request.
	registry, err := store.NewTargetRepository(pool, bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}

	target, err := registry.Create(ctx, core.DatabaseTarget{
		Name: "enqueue", Driver: "postgres", Host: "localhost", Port: 5432,
		Database: "enqueue", Username: "target", SSLMode: "disable", Active: true, Password: "password",
	})

	if err != nil {
		t.Fatal(err)
	}

	// Buat group, tetapkan leader, dan berikan izin EDIT pada target.
	groups := store.NewGroupRepository(pool)
	group, err := groups.Create(ctx, core.Group{Name: "backend"})
	
	if err != nil {
		t.Fatal(err)
	}

	if err = groups.AddMember(ctx, group.ID, leader.ID); err != nil {
		t.Fatal(err)
	}

	if _, err = groups.SetLeader(ctx, group.ID, leader.ID); err != nil {
		t.Fatal(err)
	}

	if err = store.NewPermissionRepository(pool).SetGrant(ctx, core.DatabaseGrant{GroupID: group.ID, DatabaseID: target.ID, Permission: core.PermissionEdit}); err != nil {
		t.Fatal(err)
	}

	// Buat draft, submit review, lalu approve agar request siap di-enqueue.
	migrations := store.NewMigrationRepository(pool)
	draft, err := migrations.CreateDraft(ctx, core.Draft{
		TargetID: target.ID, Description: "enqueue test", SQLUp: "SELECT 1", SQLDown: "SELECT 1",
		ExecutionMode: core.ExecutionNormal, BatchSize: 1000, CreatedBy: requester.ID,
	})

	if err != nil {
		t.Fatal(err)
	}

	review, err := migrations.SubmitReview(ctx, target.ID, draft.ID, requester.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = migrations.ApproveReview(ctx, target.ID, review.ID, leader.ID, "setuju"); err != nil {
		t.Fatal(err)
	}

	// Pasang queue stub dan dependency API untuk menangani endpoint enqueue.
	enqueuer := &queueStub{}
	a := &API{
		targets: fakeTargets{
			targets: map[int64]Target{target.ID: {ID: target.ID, Name: target.Name, Driver: target.Driver}},
			grants:  map[int64]map[int64]core.Permission{requester.ID: {target.ID: core.PermissionEdit}},
		},
		migrations:  migrations,
		jobEnqueuer: enqueuer,
	}

	// Panggil handler enqueue menggunakan target dan review yang sudah approved.
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/databases/%d/reviews/%d/enqueue", target.ID, review.ID), nil)
	r.SetPathValue("id", fmt.Sprint(target.ID))
	r.SetPathValue("reqID", fmt.Sprint(review.ID))
	
	w := httptest.NewRecorder()
	a.enqueueMigrationReview(w, r, auth.AuthenticationResult{User: requester})
	
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	
	// Pastikan payload queue berisi request, target, actor, dan action yang benar.
	if enqueuer.request.RequestID != review.ID || enqueuer.request.TargetID != target.ID || enqueuer.request.ActorID != requester.ID || enqueuer.request.Action != queue.ActionUp {
		t.Fatalf("unexpected enqueue request: %+v", enqueuer.request)
	}
	
	// Pastikan request tidak lagi dianggap APPROVED setelah berhasil masuk queue.
	if _, err = migrations.GetApprovedRequest(ctx, target.ID, review.ID); !errors.Is(err, store.ErrRequestNotApproved) {
		t.Fatalf("request status was not changed to ENQUEUED: %v", err)
	}
}