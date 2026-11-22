package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
)

type jobStub struct {
	err   error
	calls int
}

func (s *jobStub) Get(context.Context, string) (queue.Job, error) {
	return queue.Job{ID: "job_test", TargetID: 1, Status: "PARTIAL", CanRetry: true, CanCancelBatch: true}, s.err
}
func (s *jobStub) List(context.Context, int64, int) ([]queue.Job, error) { return []queue.Job{}, s.err }
func (s *jobStub) Enqueue(context.Context, queue.EnqueueRequest) (string, error) {
	s.calls++
	return "job_test", s.err
}
func (s *jobStub) Retry(context.Context, string, int64, string) (string, error) {
	s.calls++
	return "job_test", s.err
}
func (s *jobStub) CancelBatch(context.Context, string, int64, string) (string, error) {
	s.calls++
	return "42", s.err
}

func TestJobHandlersEnforcePermissionsAndCSRF(t *testing.T) {
	a := testAPI(t)
	stub := &jobStub{}
	a.UseJobs(stub)
	fake := a.targets.(fakeTargets)
	fake.grants[30] = map[int64]core.Permission{1: core.PermissionEdit}
	for _, tc := range []struct {
		name   string
		user   int64
		csrf   bool
		method string
		want   int
	}{
		{"viewer read", 10, false, "GET", 200}, {"viewer mutation", 10, true, "POST", 403},
		{"editor missing csrf", 30, false, "POST", 403}, {"editor retry", 30, true, "POST", 202},
		{"outsider", 20, true, "GET", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/jobs/job_test/retry", nil)
			r.Header.Set("Content-Type", "application/json")
			r.SetPathValue("jobID", "job_test")
			if tc.csrf {
				r.Header.Set("X-CSRF-Token", "csrf")
			}
			session := auth.AuthenticationResult{User: core.User{ID: tc.user}, Session: core.Session{CSRFToken: "csrf"}}
			w := httptest.NewRecorder()
			before := stub.calls
			if tc.method == "GET" {
				a.getJob(w, r, session)
			} else {
				a.retryJob(w, r, session)
			}
			if w.Code != tc.want {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if tc.want == 403 && stub.calls != before {
				t.Fatal("denied mutation reached queue")
			}
		})
	}
}

func TestJobHandlerErrorsAndCancelReview(t *testing.T) {
	a := testAPI(t)
	stub := &jobStub{}
	a.UseJobs(stub)
	fake := a.targets.(fakeTargets)
	fake.grants[30] = map[int64]core.Permission{1: core.PermissionEdit}
	session := auth.AuthenticationResult{User: core.User{ID: 30}, Session: core.Session{CSRFToken: "csrf"}}
	r := httptest.NewRequest("POST", "/api/jobs/job_test/cancel-batch", nil)
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("jobID", "job_test")
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	a.cancelJob(w, r, session)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Body.String())
	}
	for _, tc := range []struct {
		err  error
		code int
	}{{queue.ErrNotFound, 404}, {queue.ErrApproval, 409}, {queue.ErrForbidden, 403}, {queue.ErrConflict, 409}, {queue.ErrNoGroupLeader, 422}} {
		w := httptest.NewRecorder()
		jobError(w, tc.err)
		if w.Code != tc.code {
			t.Fatal(w.Code)
		}
	}
}

func TestJobMutationRequiresJSONContentType(t *testing.T) {
	a := testAPI(t)
	stub := &jobStub{}
	a.UseJobs(stub)
	fake := a.targets.(fakeTargets)
	fake.grants[30] = map[int64]core.Permission{1: core.PermissionEdit}
	session := auth.AuthenticationResult{User: core.User{ID: 30}, Session: core.Session{CSRFToken: "csrf"}}
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		r := httptest.NewRequest("POST", "/api/jobs/job_test/retry", nil)
		r.SetPathValue("jobID", "job_test")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		a.retryJob(w, r, session)
		if w.Code != 400 || stub.calls != 0 {
			t.Fatalf("content-type=%q code=%d mutations=%d", contentType, w.Code, stub.calls)
		}
	}
}
