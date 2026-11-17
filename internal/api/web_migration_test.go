package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	editorID int64 = 40
	viewerID int64 = 41
)

type migrationFixture struct {
	api      *API
	targetID int64
	pool     *pgxpool.Pool
}

// Uji pagination riwayat migration di atas 200 entri dengan timestamp sama.
func TestMigrationHistoryPagination(t *testing.T) {
	f := migrationAPI(t)
	ctx := context.Background()

	draft, err := f.api.migrations.CreateDraft(ctx, core.Draft{
		TargetID:    f.targetID,
		Description: "history fixture",
		SQL:         "SELECT 1",
		CreatedBy:   editorID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Identical timestamps exercise the id tie-breaker. Mixed statuses must all
	// remain reachable, including the oldest records past the former 200 cap.
	const seedAttemptsQuery = `
		INSERT INTO migration_attempts (
			id,
			target_id,
			draft_id,
			draft_revision,
			description,
			sql_text,
			actor_id,
			actor_name,
			base_version,
			status,
			started_at
		)
		SELECT
			'history-' || lpad(n::text, 3, '0'),
			$1,
			$2,
			n,
			'entry-' || lpad(n::text, 3, '0'),
			'SELECT 1',
			$3,
			'editor',
			0,
			CASE WHEN n % 2 = 0 THEN 'FAILED' ELSE 'SUCCESS' END,
			'2026-01-01'::timestamptz
		FROM generate_series(1, 205) AS n
	`
	_, err = f.pool.Exec(ctx, seedAttemptsQuery, f.targetID, draft.ID, editorID)
	if err != nil {
		t.Fatal(err)
	}

	first := f.get(viewerID, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first page status %d, want 200", first.Code)
	}
	if !strings.Contains(first.Body.String(), "history_offset=200") {
		t.Fatal("first page missing next-page link history_offset=200")
	}
	if strings.Contains(first.Body.String(), "entry-005") {
		t.Fatal("first page unexpectedly includes entry-005 from second page")
	}

	second := f.get(viewerID, "history_offset=200")
	if second.Code != http.StatusOK {
		t.Fatalf("second page status %d, want 200", second.Code)
	}
	secondBody := second.Body.String()
	if !strings.Contains(secondBody, "entry-001") || strings.Contains(secondBody, "entry-006") {
		t.Fatalf("second page has unexpected entries: %s", secondBody)
	}
	if strings.Contains(secondBody, "history_offset=400") || !strings.Contains(secondBody, "history_offset=0") {
		t.Fatalf("second page has unexpected pagination links: %s", secondBody)
	}

	for _, offset := range []string{"-1", "invalid"} {
		if w := f.get(viewerID, "history_offset="+offset); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid offset %q accepted with status %d", offset, w.Code)
		}
	}

	detail := f.get(viewerID, "history_offset=200&attempt=history-001")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "berhasil") {
		t.Fatalf("old attempt detail unavailable: status=%d", detail.Code)
	}
}

// Pastikan schema sementara test selalu dihapus saat cleanup.
func TestMigrationFixtureCleanup(t *testing.T) {
	observer := testutil.Platform(t)
	var schema string

	t.Run("fixture", func(t *testing.T) {
		f := migrationAPI(t)
		err := f.pool.QueryRow(context.Background(), "SELECT current_schema()").Scan(&schema)
		if err != nil {
			t.Fatal(err)
		}
		redirectQuery(t, f.submit(editorID, newDraft("cleanup fixture", "SELECT 1", "run")))
		if len(f.history(t)) != 1 {
			t.Fatal("expected persisted attempt before cleanup")
		}
	})

	var exists bool
	err := observer.QueryRow(context.Background(), "SELECT EXISTS(SELECT FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("fixture schema %q was not removed during cleanup", schema)
	}
}

// Siapkan fixture API migration dengan platform DB dan target SQLite.
func migrationAPI(t *testing.T) migrationFixture {
	t.Helper()
	pool := testutil.Platform(t)

	a := testAPI(t)
	a.migrations = store.NewMigrationRepository(pool)
	fake := a.targets.(fakeTargets)
	id := time.Now().UnixNano()
	target := fake.targets[1]
	target.ID = id
	fake.targets[id] = target
	fake.grants[editorID] = map[int64]core.Permission{id: core.PermissionEdit}
	fake.grants[viewerID] = map[int64]core.Permission{id: core.PermissionView}

	return migrationFixture{api: a, targetID: id, pool: pool}
}

// Kirim request GET halaman migration.
func (f migrationFixture) get(user int64, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", fmt.Sprintf("/databases/%d/migrations?%s", f.targetID, query), nil)
	r.SetPathValue("id", fmt.Sprint(f.targetID))
	w := httptest.NewRecorder()
	f.api.migrationsPage(w, r, session(user, false))
	return w
}

// Kirim request POST form migration.
func (f migrationFixture) submit(user int64, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", fmt.Sprintf("/databases/%d/migrations", f.targetID), strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", fmt.Sprint(f.targetID))
	w := httptest.NewRecorder()
	f.api.submitMigration(w, r, session(user, false))
	return w
}

// redirectQuery membaca query string dari header Location setelah redirect
func redirectQuery(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected a redirect, got %d: %s", w.Code, w.Body.String())
	}

	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	return location.Query()
}

// Bentuk payload form draft migration.
func newDraft(description, sql, action string) url.Values {
	return url.Values{"description": {description}, "sql": {sql}, "action": {action}}
}

// Ambil seluruh riwayat attempt milik target fixture.
func (f migrationFixture) history(t *testing.T) []core.Attempt {
	t.Helper()
	attempts, err := f.api.migrations.ListAttempts(context.Background(), f.targetID)
	if err != nil {
		t.Fatal(err)
	}
	return attempts
}

// Baca versi target database pada fixture saat ini.
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

// Uji alur simpan, edit, eksekusi, kirim ulang idempoten, dan kunci draft.
func TestSaveEditAndRunMigration(t *testing.T) {
	f := migrationAPI(t)

	saveForm := newDraft("Tambah tabel produk", "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL)", "save")
	query := redirectQuery(t, f.submit(editorID, saveForm))
	if query.Get("saved") != "1" {
		t.Fatalf("unexpected redirect: %v", query)
	}
	expectPage(t, f.get(editorID, "draft="+query.Get("draft")+"&saved=1"), http.StatusOK, "Draft disimpan", "Revisi 1", "CREATE TABLE products")

	runForm := newDraft(
		"Tambah tabel produk",
		"CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL); INSERT INTO products VALUES (1, 'Buku')",
		"run",
	)
	runForm.Set("draft_id", query.Get("draft"))
	runForm.Set("revision", "1")
	query = redirectQuery(t, f.submit(editorID, runForm))
	if query.Get("draft") != "" || query.Get("attempt") == "" {
		t.Fatalf("successful run must clear the editor and show the attempt: %v", query)
	}
	expectPage(t, f.get(editorID, "attempt="+query.Get("attempt")), http.StatusOK, "berhasil", "versi 2", "SUCCESS")

	history := f.history(t)
	if len(history) != 1 {
		t.Fatalf("got %d history entries, want 1", len(history))
	}
	if history[0].Revision != 2 || history[0].ActorID != editorID || f.version(t) != 2 {
		t.Fatalf("unexpected history or version: %+v, target version=%d", history[0], f.version(t))
	}
	expectPage(t, page(f.api.explorerPage, viewerID, fmt.Sprintf("/databases/%d?table=products", f.targetID)), http.StatusOK, "<td>Buku</td>")

	// Tombol yang ditekan dua kali mengirim form yang sama. Hasilnya attempt yang sama, bukan eksekusi kedua
	runForm.Set("revision", "2")
	again := redirectQuery(t, f.submit(editorID, runForm))
	if again.Get("attempt") != query.Get("attempt") || len(f.history(t)) != 1 || f.version(t) != 2 {
		t.Fatalf("repeating the same submission must not run the migration twice: %v", again)
	}

	runForm.Set("sql", "DROP TABLE products")
	expectPage(t, f.submit(editorID, runForm), http.StatusUnprocessableEntity, "sudah dijalankan")
}

// Pastikan migration gagal di-rollback dan draft tetap bisa diperbaiki lagi.
func TestFailedMigrationIsRolledBackAndCanBeFixed(t *testing.T) {
	f := migrationAPI(t)

	form := newDraft("Migration gagal", "ALTER TABLE customers ADD COLUMN phone TEXT; INSERT INTO missing VALUES (1)", "run")
	query := redirectQuery(t, f.submit(editorID, form))
	if query.Get("draft") == "" {
		t.Fatal("failed draft must stay open in the editor")
	}
	expectPage(t, f.get(editorID, query.Encode()), http.StatusOK, "gagal: statement 2 gagal", "FAILED", "ALTER TABLE customers")

	if f.version(t) != 1 {
		t.Fatalf("failed migration changed target version to %d, want 1", f.version(t))
	}
	expectPage(t, page(f.api.explorerPage, viewerID, fmt.Sprintf("/databases/%d?table=customers&view=columns", f.targetID)), http.StatusOK)

	form.Set("draft_id", query.Get("draft"))
	form.Set("revision", "1")
	form.Set("sql", "ALTER TABLE customers ADD COLUMN phone TEXT")
	redirectQuery(t, f.submit(editorID, form))

	history := f.history(t)
	if len(history) != 2 || history[0].Status != core.AttemptSuccess || f.version(t) != 2 {
		t.Fatalf("fixed draft must run as a new revision: %+v, version=%d", history, f.version(t))
	}
}

// Pastikan draft kedua dengan base version lama gagal saat target sudah maju.
func TestStaleDraftFailsWithVersionConflict(t *testing.T) {
	f := migrationAPI(t)
	first := newDraft("Draft pertama", "CREATE TABLE a (id INTEGER)", "save")
	second := newDraft("Draft kedua", "CREATE TABLE b (id INTEGER)", "save")
	first.Set("draft_id", redirectQuery(t, f.submit(editorID, first)).Get("draft"))
	second.Set("draft_id", redirectQuery(t, f.submit(editorID, second)).Get("draft"))

	// Kedua draft disimpan saat database di versi 1. Setelah draft pertama berjalan, draft kedua sudah basi.
	for _, form := range []url.Values{first, second} {
		form.Set("revision", "1")
		form.Set("action", "run")
		redirectQuery(t, f.submit(editorID, form))
	}

	history := f.history(t)
	if len(history) != 2 {
		t.Fatalf("got %d attempts, want 2", len(history))
	}
	if history[0].Status != core.AttemptFailed || !strings.Contains(history[0].Error, "versi database telah berubah") {
		t.Fatalf("second draft must fail because it was based on version 1: %+v", history[0])
	}
}

// Pastikan form migration menolak input tidak valid dan sembunyikan editor dari viewer.
func TestMigrationFormRejectsInvalidRequests(t *testing.T) {
	f := migrationAPI(t)
	expectPage(t, f.submit(editorID, newDraft("a", "SELECT 1", "save")), http.StatusUnprocessableEntity, "Deskripsi harus")
	expectPage(t, f.submit(editorID, newDraft("Kosong", "  ", "save")), http.StatusUnprocessableEntity, "SQL tidak boleh kosong")
	expectPage(t, f.submit(viewerID, newDraft("Viewer", "CREATE TABLE d (id INTEGER)", "run")), http.StatusForbidden)
	expectPage(t, f.submit(99, newDraft("Orang asing", "CREATE TABLE d (id INTEGER)", "run")), http.StatusForbidden)

	w := f.get(viewerID, "")
	expectPage(t, w, http.StatusOK, "Riwayat migration")
	if strings.Contains(w.Body.String(), `name="sql"`) {
		t.Fatal("viewer must not see the migration editor")
	}
	if len(f.history(t)) != 0 || f.version(t) != 1 {
		t.Fatal("rejected requests must not create attempts or change the target")
	}
}

// Pastikan preview EXPLAIN tidak mengubah data atau membuat attempt.
func TestExecutionPlanDoesNotRunQuery(t *testing.T) {
	f := migrationAPI(t)
	expectPage(t, f.submit(editorID, newDraft("", "DELETE FROM orders WHERE customer_id = 1", "plan")), http.StatusOK, "Execution plan", "EXPLAIN QUERY PLAN", "orders")
	expectPage(t, page(f.api.explorerPage, viewerID, fmt.Sprintf("/databases/%d?table=orders", f.targetID)), http.StatusOK, "Baris 1 sampai 3")
	expectPage(t, f.submit(editorID, newDraft("", "DROP TABLE orders", "plan")), http.StatusUnprocessableEntity, "plan hanya tersedia")
	if len(f.history(t)) != 0 {
		t.Fatal("plan preview must not create attempts")
	}
}

// Uji rekonsiliasi manual untuk attempt UNKNOWN yang commit maupun gagal.
func TestReconcileResolvesUnknownAttempts(t *testing.T) {
	f := migrationAPI(t)
	ctx := context.Background()
	editor := core.User{ID: editorID, Username: "editor"}
	target := f.api.targets.(fakeTargets).targets[f.targetID]

	saveDraft := func(name string) core.Draft {
		draft, err := f.api.migrations.CreateDraft(ctx, core.Draft{
			TargetID:    f.targetID,
			Description: name,
			SQL:         "CREATE TABLE " + name + " (id INTEGER)",
			CreatedBy:   editorID,
			BaseVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return draft
	}

	begin := func(draft core.Draft) core.Attempt {
		attempt, _, _, err := f.api.migrations.BeginAttempt(ctx, f.targetID, draft.ID, 1, newAttemptID(), editor)
		if err != nil {
			t.Fatal(err)
		}
		return attempt
	}

	committed := begin(saveDraft("committed"))
	conn, err := f.api.open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Execute(ctx, targetdb.Execution{
		ID:          committed.ID,
		SQL:         committed.SQL,
		ActorID:     editorID,
		ActorName:   "editor",
		BaseVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	lost := begin(saveDraft("lost"))
	for _, id := range []string{committed.ID, lost.ID} {
		_, err = f.api.migrations.FinishAttempt(ctx, id, core.AttemptUnknown, nil, targetdb.ErrUnknown.Error())
		if err != nil {
			t.Fatal(err)
		}
	}
	running := begin(saveDraft("running"))

	reconcile := func(user int64, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", nil)
		r.SetPathValue("id", fmt.Sprint(f.targetID))
		r.SetPathValue("attempt", id)
		w := httptest.NewRecorder()
		f.api.reconcileAttempt(w, r, session(user, false))
		return w
	}

	expectPage(t, reconcile(viewerID, committed.ID), http.StatusForbidden)
	for _, id := range []string{committed.ID, lost.ID, running.ID} {
		redirectQuery(t, reconcile(editorID, id))
	}

	status := map[string]core.Attempt{}
	for _, attempt := range f.history(t) {
		status[attempt.ID] = attempt
	}
	if result := status[committed.ID]; result.Status != core.AttemptSuccess || result.Version == nil || *result.Version != 2 {
		t.Errorf("committed attempt must become SUCCESS: %+v", result)
	}
	if status[lost.ID].Status != core.AttemptFailed {
		t.Errorf("attempt missing from the ledger must become FAILED: %+v", status[lost.ID])
	}
	if status[running.ID].Status != core.AttemptRunning {
		t.Errorf("recent RUNNING attempt must be left alone: %+v", status[running.ID])
	}
}
