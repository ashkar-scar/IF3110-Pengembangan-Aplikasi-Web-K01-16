package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

const (
	editorID int64 = 40
	viewerID int64 = 41
)

type migrationFixture struct {
	api      *API
	targetID int64
	path     string
}

// migrationAPI memakai database platform sungguhan untuk draft dan attempt.
func migrationAPI(t *testing.T) migrationFixture {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DSN")
	if dsn == "" {
		t.Skip("set TEST_PLATFORM_DSN to run migration workflow tests")
	}
	pool, err := store.NewPostgresPool(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(context.Background(), store.MigrationSchema); err != nil {
		t.Fatal(err)
	}
	a := testAPI(t)
	a.migrations = store.NewMigrationRepository(pool)
	fake := a.targets.(fakeTargets)
	id := time.Now().UnixNano()
	target := fake.targets[1]
	target.ID = id
	fake.targets[id] = target
	fake.grants[editorID] = map[int64]core.Permission{id: core.PermissionEdit}
	fake.grants[viewerID] = map[int64]core.Permission{id: core.PermissionView}
	return migrationFixture{api: a, targetID: id, path: fmt.Sprintf("/api/databases/%d", id)}
}

type request struct {
	handler func(http.ResponseWriter, *http.Request, auth.AuthenticationResult)
	user    int64
	method  string
	path    string
	values  map[string]string
	body    any
	noCSRF  bool
}

func (f migrationFixture) do(t *testing.T, req request, status int, into any) {
	t.Helper()
	var body bytes.Buffer
	if req.body != nil {
		json.NewEncoder(&body).Encode(req.body)
	}
	r := httptest.NewRequest(req.method, f.path+req.path, &body)
	r.SetPathValue("id", fmt.Sprint(f.targetID))
	for key, value := range req.values {
		r.SetPathValue(key, value)
	}
	if !req.noCSRF {
		r.Header.Set("X-CSRF-Token", "csrf-token")
	}
	w := httptest.NewRecorder()
	req.handler(w, r, session(req.user, false))
	decode(t, w, status, into)
}

func (f migrationFixture) draft(t *testing.T, description, sql string) core.Draft {
	t.Helper()
	var draft core.Draft
	f.do(t, request{handler: f.api.saveDraft, user: editorID, method: "POST", path: "/drafts", body: draftRequest{Description: description, SQL: sql}}, http.StatusOK, &draft)
	return draft
}

func (f migrationFixture) execute(t *testing.T, draft core.Draft, status int, into any) {
	t.Helper()
	f.do(t, request{handler: f.api.executeDraft, user: editorID, method: "POST", path: fmt.Sprintf("/drafts/%d/execute", draft.ID),
		values: map[string]string{"draft": fmt.Sprint(draft.ID)}, body: executeRequest{Revision: draft.Revision, RequestID: fmt.Sprintf("attempt-%d-%d-%d", f.targetID, draft.ID, draft.Revision)}}, status, into)
}

func (f migrationFixture) history(t *testing.T) []core.Attempt {
	t.Helper()
	var attempts []core.Attempt
	f.do(t, request{handler: f.api.listAttempts, user: viewerID, method: "GET", path: "/migrations"}, http.StatusOK, &attempts)
	return attempts
}

func (f migrationFixture) version(t *testing.T) int64 {
	t.Helper()
	conn, err := f.api.open(context.Background(), f.api.targets.(fakeTargets).targets[f.targetID])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	version, err := conn.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestMigrationLifecycle(t *testing.T) {
	f := migrationAPI(t)
	draft := f.draft(t, "Tambah tabel produk", "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
	if draft.Revision != 1 || draft.BaseVersion != 1 || draft.CreatedBy != editorID {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	draftPath := map[string]string{"draft": fmt.Sprint(draft.ID)}
	edit := draftRequest{Description: "Tambah tabel produk", SQL: draft.SQL + "; INSERT INTO products VALUES (1, 'Buku')", Revision: 1}
	f.do(t, request{handler: f.api.saveDraft, user: editorID, method: "PATCH", path: "/drafts/x", values: draftPath, body: edit}, http.StatusOK, &draft)
	if draft.Revision != 2 {
		t.Fatalf("revision must increase after edit: %+v", draft)
	}
	f.do(t, request{handler: f.api.saveDraft, user: editorID, method: "PATCH", path: "/drafts/x", values: draftPath, body: edit}, http.StatusConflict, nil)

	var drafts []core.Draft
	f.do(t, request{handler: f.api.listDrafts, user: editorID, method: "GET", path: "/drafts"}, http.StatusOK, &drafts)
	if len(drafts) != 1 || drafts[0].ID != draft.ID {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}

	var attempt core.Attempt
	f.execute(t, draft, http.StatusOK, &attempt)
	if attempt.Status != core.AttemptSuccess || attempt.Version == nil || *attempt.Version != 2 || attempt.ActorID != editorID || attempt.FinishedAt == nil {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}
	var again core.Attempt
	f.execute(t, draft, http.StatusOK, &again)
	if again.ID != attempt.ID || f.version(t) != 2 {
		t.Fatalf("repeating the same revision must not run the migration twice: %+v", again)
	}
	f.do(t, request{handler: f.api.saveDraft, user: editorID, method: "PATCH", path: "/drafts/x", values: draftPath, body: draftRequest{Description: "Ubah", SQL: "SELECT 1", Revision: 2}}, http.StatusConflict, nil)

	history := f.history(t)
	if len(history) != 1 || history[0].ID != attempt.ID {
		t.Fatalf("unexpected history: %+v", history)
	}
	var detail core.Attempt
	f.do(t, request{handler: f.api.getAttempt, user: viewerID, method: "GET", path: "/migrations/x", values: map[string]string{"attempt": attempt.ID}}, http.StatusOK, &detail)
	if detail.SQL != draft.SQL {
		t.Fatalf("attempt must keep the executed SQL: %+v", detail)
	}
	var page targetdb.RowPage
	decode(t, call(f.api.explore, session(viewerID, false), "GET", f.path+"/rows?table=products", fmt.Sprint(f.targetID)), http.StatusOK, &page)
	if len(page.Rows) != 1 || page.Rows[0][1] != "Buku" {
		t.Fatalf("migration result not visible in the explorer: %+v", page)
	}
}

func TestFailedMigrationIsRecordedAndRolledBack(t *testing.T) {
	f := migrationAPI(t)
	draft := f.draft(t, "Migration gagal", "ALTER TABLE customers ADD COLUMN phone TEXT; INSERT INTO missing VALUES (1)")
	var failure struct {
		Error errorDetail `json:"error"`
	}
	f.execute(t, draft, http.StatusUnprocessableEntity, &failure)
	if failure.Error.Code != "MIGRATION_FAILED" || !strings.Contains(failure.Error.Message, "statement 2") {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	history := f.history(t)
	if len(history) != 1 || history[0].Status != core.AttemptFailed || history[0].Version != nil || !strings.Contains(history[0].Error, "statement 2") {
		t.Fatalf("failed attempt not recorded: %+v", history)
	}
	if f.version(t) != 1 {
		t.Fatal("failed migration changed the target version")
	}
	fixed := draftRequest{Description: draft.Description, SQL: "ALTER TABLE customers ADD COLUMN phone TEXT", Revision: draft.Revision}
	f.do(t, request{handler: f.api.saveDraft, user: editorID, method: "PATCH", path: "/drafts/x", values: map[string]string{"draft": fmt.Sprint(draft.ID)}, body: fixed}, http.StatusOK, &draft)
	var attempt core.Attempt
	f.execute(t, draft, http.StatusOK, &attempt)
	if attempt.Status != core.AttemptSuccess || len(f.history(t)) != 2 {
		t.Fatalf("fixed draft must be executable as a new revision: %+v", attempt)
	}
}

func TestStaleDraftGetsVersionConflict(t *testing.T) {
	f := migrationAPI(t)
	first := f.draft(t, "Draft pertama", "CREATE TABLE a (id INTEGER)")
	second := f.draft(t, "Draft kedua", "CREATE TABLE b (id INTEGER)")
	f.execute(t, first, http.StatusOK, nil)
	var failure struct {
		Error errorDetail `json:"error"`
	}
	f.execute(t, second, http.StatusConflict, &failure)
	if failure.Error.Code != "VERSION_CONFLICT" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if history := f.history(t); history[0].Status != core.AttemptFailed || f.version(t) != 2 {
		t.Fatalf("stale draft must fail without changing the target: %+v", history)
	}
}

func TestMigrationPermissionsAndValidation(t *testing.T) {
	f := migrationAPI(t)
	draft := f.draft(t, "Draft editor", "CREATE TABLE c (id INTEGER)")
	draftPath := map[string]string{"draft": fmt.Sprint(draft.ID)}
	execute := executeRequest{Revision: 1, RequestID: "attempt-permission-check"}
	cases := []struct {
		name   string
		req    request
		status int
	}{
		{"viewer cannot list drafts", request{handler: f.api.listDrafts, user: viewerID, method: "GET"}, http.StatusForbidden},
		{"viewer cannot save drafts", request{handler: f.api.saveDraft, user: viewerID, method: "POST", body: draftRequest{Description: "abc", SQL: "SELECT 1"}}, http.StatusForbidden},
		{"viewer cannot execute", request{handler: f.api.executeDraft, user: viewerID, method: "POST", values: draftPath, body: execute}, http.StatusForbidden},
		{"stranger cannot read history", request{handler: f.api.listAttempts, user: 99, method: "GET"}, http.StatusForbidden},
		{"missing CSRF token", request{handler: f.api.saveDraft, user: editorID, method: "POST", body: draftRequest{Description: "abc", SQL: "SELECT 1"}, noCSRF: true}, http.StatusForbidden},
		{"description too short", request{handler: f.api.saveDraft, user: editorID, method: "POST", body: draftRequest{Description: "a", SQL: "SELECT 1"}}, http.StatusUnprocessableEntity},
		{"empty SQL", request{handler: f.api.saveDraft, user: editorID, method: "POST", body: draftRequest{Description: "abc", SQL: "  "}}, http.StatusUnprocessableEntity},
		{"unknown field", request{handler: f.api.saveDraft, user: editorID, method: "POST", body: map[string]string{"description": "abc", "sql": "SELECT 1", "base_version": "9"}}, http.StatusBadRequest},
		{"short request id", request{handler: f.api.executeDraft, user: editorID, method: "POST", values: draftPath, body: executeRequest{Revision: 1, RequestID: "short"}}, http.StatusUnprocessableEntity},
		{"wrong revision", request{handler: f.api.executeDraft, user: editorID, method: "POST", values: draftPath, body: executeRequest{Revision: 5, RequestID: "attempt-wrong-revision"}}, http.StatusConflict},
		{"unknown draft", request{handler: f.api.getDraft, user: editorID, method: "GET", values: map[string]string{"draft": "999999999"}}, http.StatusNotFound},
		{"unknown attempt", request{handler: f.api.getAttempt, user: viewerID, method: "GET", values: map[string]string{"attempt": "missing"}}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.do(t, tc.req, tc.status, nil)
		})
	}
	if len(f.history(t)) != 0 || f.version(t) != 1 {
		t.Fatal("rejected requests must not create attempts or change the target")
	}
}

func TestReconcileResolvesUnknownAttempts(t *testing.T) {
	f := migrationAPI(t)
	ctx := context.Background()
	editor := core.User{ID: editorID, Username: "editor"}
	target := f.api.targets.(fakeTargets).targets[f.targetID]

	committed := f.draft(t, "Commit tanpa jawaban", "CREATE TABLE d (id INTEGER)")
	attempt, _, _, err := f.api.migrations.BeginAttempt(ctx, f.targetID, committed.ID, 1, fmt.Sprintf("lost-%d", f.targetID), editor)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := f.api.open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Execute(ctx, targetdb.Execution{ID: attempt.ID, SQL: attempt.SQL, ActorID: editorID, ActorName: "editor", BaseVersion: 1}); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if _, err = f.api.migrations.FinishAttempt(ctx, attempt.ID, core.AttemptUnknown, nil, targetdb.ErrUnknown.Error()); err != nil {
		t.Fatal(err)
	}

	missing := f.draft(t, "Tidak pernah commit", "CREATE TABLE e (id INTEGER)")
	lost, _, _, err := f.api.migrations.BeginAttempt(ctx, f.targetID, missing.ID, 1, fmt.Sprintf("missing-%d", f.targetID), editor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.api.migrations.FinishAttempt(ctx, lost.ID, core.AttemptUnknown, nil, targetdb.ErrUnknown.Error()); err != nil {
		t.Fatal(err)
	}

	running := f.draft(t, "Masih berjalan", "CREATE TABLE g (id INTEGER)")
	fresh, _, _, err := f.api.migrations.BeginAttempt(ctx, f.targetID, running.ID, 1, fmt.Sprintf("running-%d", f.targetID), editor)
	if err != nil {
		t.Fatal(err)
	}

	reconcile := func(id string, status int) core.Attempt {
		var result core.Attempt
		f.do(t, request{handler: f.api.reconcile, user: editorID, method: "POST", values: map[string]string{"attempt": id}}, status, &result)
		return result
	}
	if result := reconcile(attempt.ID, http.StatusOK); result.Status != core.AttemptSuccess || result.Version == nil || *result.Version != 2 {
		t.Fatalf("committed attempt must become SUCCESS: %+v", result)
	}
	if result := reconcile(lost.ID, http.StatusOK); result.Status != core.AttemptFailed {
		t.Fatalf("attempt missing from the ledger must become FAILED: %+v", result)
	}
	if result := reconcile(fresh.ID, http.StatusAccepted); result.Status != core.AttemptRunning {
		t.Fatalf("recent RUNNING attempt must be left alone: %+v", result)
	}
	f.do(t, request{handler: f.api.reconcile, user: viewerID, method: "POST", values: map[string]string{"attempt": attempt.ID}}, http.StatusForbidden, nil)
}
