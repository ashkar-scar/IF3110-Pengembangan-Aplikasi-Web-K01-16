package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

const reconcileGrace = 2 * time.Minute

type migrationsPage struct {
	base
	Target         Target
	CanEdit        bool
	Drafts         []core.Draft
	Draft          core.Draft
	Notice         string
	Error          string
	Result         *core.Attempt
	Plan           *planView
	History        []core.Attempt
	HistoryOffset  int
	HasMoreHistory bool
}

func (p migrationsPage) PrevHistoryOffset() int { return max(p.HistoryOffset-store.AttemptsPerPage, 0) }
func (p migrationsPage) NextHistoryOffset() int { return p.HistoryOffset + store.AttemptsPerPage }

// TODO(faiz): panggil dari main.go bersama UseTargets setelah tabel migration dibuat di database platform.
func (a *API) UseMigrations(repository *store.MigrationRepository) {
	a.migrations = repository
}

// GET /databases/{id}/migrations
func (a *API) migrationsPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.pageTarget(w, r, session, core.PermissionView)
	if !ok {
		return
	}

	offset, err := strconv.Atoi(r.URL.Query().Get("history_offset"))
	if r.URL.Query().Has("history_offset") && (err != nil || offset < 0) {
		a.showError(w, http.StatusBadRequest, session, "Offset riwayat tidak valid.")
		return
	}
	page, err := a.newMigrationsPage(r.Context(), target, session, offset)
	if err != nil {
		log.Printf("Gagal membaca migration: %v", err)
		a.showError(w, http.StatusInternalServerError, session, "Riwayat migration tidak dapat dimuat.")
		return
	}

	query := r.URL.Query()
	if id, err := strconv.ParseInt(query.Get("draft"), 10, 64); err == nil && page.CanEdit {
		if draft, err := a.migrations.GetDraft(r.Context(), target.ID, id); err == nil {
			page.Draft = draft
		}
	}
	if id := query.Get("attempt"); id != "" {
		if attempt, err := a.migrations.GetAttempt(r.Context(), target.ID, id); err == nil {
			page.Result = &attempt
		}
	}
	if query.Get("saved") == "1" {
		page.Notice = "Draft disimpan."
	}

	render(w, http.StatusOK, "migrations", page)
}

func (a *API) newMigrationsPage(ctx context.Context, target Target, session auth.AuthenticationResult, offsets ...int) (migrationsPage, error) {
	page := migrationsPage{
		base:    newBase("Migration "+target.Name, session),
		Target:  target,
		CanEdit: target.Permission.Can(core.PermissionEdit),
	}

	var err error
	if page.CanEdit {
		if page.Drafts, err = a.migrations.ListDrafts(ctx, target.ID); err != nil {
			return page, err
		}
	}

	if len(offsets) > 0 {
		page.HistoryOffset = max(offsets[0], 0)
	}
	page.History, err = a.migrations.ListAttempts(ctx, target.ID, page.HistoryOffset)
	page.HasMoreHistory = len(page.History) > store.AttemptsPerPage
	if page.HasMoreHistory {
		page.History = page.History[:store.AttemptsPerPage]
	}
	return page, err
}

// POST /databases/{id}/migrations
func (a *API) submitMigration(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.pageTarget(w, r, session, core.PermissionEdit)
	if !ok {
		return
	}

	page, err := a.newMigrationsPage(r.Context(), target, session)
	if err != nil {
		log.Printf("Gagal membaca migration: %v", err)
		a.showError(w, http.StatusInternalServerError, session, "Riwayat migration tidak dapat dimuat.")
		return
	}

	draftID, _ := strconv.ParseInt(r.FormValue("draft_id"), 10, 64)
	revision, _ := strconv.Atoi(r.FormValue("revision"))
	page.Draft = core.Draft{
		ID:          draftID,
		TargetID:    target.ID,
		Description: strings.TrimSpace(r.FormValue("description")),
		SQL:         r.FormValue("sql"),
		Revision:    revision,
		Fingerprint: r.FormValue("fingerprint"),
		CreatedBy:   session.User.ID,
	}
	action := r.FormValue("action")
	if action != "save" && action != "run" && action != "plan" {
		page.Error = "Aksi migration tidak valid."
		render(w, 422, "migrations", page)
		return
	}

	if action == "plan" {
		plan, err := a.explain(r.Context(), target, page.Draft.SQL)
		if err != nil {
			page.Error = err.Error()
			render(w, http.StatusUnprocessableEntity, "migrations", page)
			return
		}

		page.Plan = &plan
		render(w, http.StatusOK, "migrations", page)
		return
	}

	draft, err := a.saveDraftIfChanged(r.Context(), target, page.Draft)
	if err != nil {
		page.Error = err.Error()
		render(w, http.StatusUnprocessableEntity, "migrations", page)
		return
	}

	if action != "run" {
		redirectToMigrations(w, r, target, fmt.Sprintf("draft=%d&saved=1", draft.ID))
		return
	}

	attempt, err := a.runDraft(r.Context(), target, draft, session.User)
	if err != nil {
		page.Draft = draft
		page.Error = err.Error()
		render(w, http.StatusUnprocessableEntity, "migrations", page)
		return
	}

	if attempt.Status == core.AttemptSuccess {
		redirectToMigrations(w, r, target, "attempt="+attempt.ID)
		return
	}

	redirectToMigrations(w, r, target, fmt.Sprintf("draft=%d&attempt=%s", draft.ID, attempt.ID))
}

// POST /databases/{id}/migrations/{attempt}/reconcile
func (a *API) reconcileAttempt(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.pageTarget(w, r, session, core.PermissionEdit)
	if !ok {
		return
	}

	attempt, err := a.migrations.GetAttempt(r.Context(), target.ID, r.PathValue("attempt"))
	if err != nil {
		a.showError(w, http.StatusNotFound, session, "Riwayat migration tidak ditemukan.")
		return
	}

	if err := a.reconcile(r.Context(), target, attempt); err != nil {
		log.Printf("Rekonsiliasi %s gagal: %v", attempt.ID, err)
		a.showError(w, http.StatusServiceUnavailable, session, "Database target tidak dapat diakses untuk rekonsiliasi. Coba lagi nanti.")
		return
	}

	redirectToMigrations(w, r, target, "attempt="+attempt.ID)
}

func redirectToMigrations(w http.ResponseWriter, r *http.Request, target Target, query string) {
	http.Redirect(w, r, fmt.Sprintf("/databases/%d/migrations?%s", target.ID, query), http.StatusSeeOther)
}

func (a *API) saveDraftIfChanged(ctx context.Context, target Target, form core.Draft) (core.Draft, error) {
	if form.ID != 0 {
		saved, err := a.migrations.GetDraft(ctx, target.ID, form.ID)
		if err == nil && saved.Revision == form.Revision && saved.Description == form.Description && saved.SQL == form.SQL {
			return saved, nil
		}
	}

	return a.saveDraft(ctx, target, form)
}

func (a *API) saveDraft(ctx context.Context, target Target, draft core.Draft) (core.Draft, error) {
	if len(draft.Description) < 2 || len(draft.Description) > 200 {
		return draft, errors.New("Deskripsi harus 2 sampai 200 karakter.")
	}

	if strings.TrimSpace(draft.SQL) == "" || len(draft.SQL) > 65536 {
		return draft, errors.New("SQL tidak boleh kosong dan maksimal 64 KiB.")
	}

	conn, err := a.open(ctx, target)
	if err != nil {
		log.Printf("Database target tidak dapat diakses: %v", err)
		return draft, errors.New("Database target tidak dapat diakses.")
	}
	defer conn.Close()

	if draft.BaseVersion, err = conn.Version(ctx); err != nil {
		log.Printf("Gagal membaca versi database target: %v", err)
		return draft, errors.New("Versi database target tidak dapat dibaca.")
	}

	if draft.ID == 0 {
		draft, err = a.migrations.CreateDraft(ctx, draft)
	} else {
		draft, err = a.migrations.UpdateDraft(ctx, draft)
	}
	if errors.Is(err, store.ErrDraftConflict) {
		return draft, errors.New("Draft sudah berubah atau sudah dijalankan. Buka ulang draft atau buat draft baru.")
	}
	if err != nil {
		log.Printf("Gagal menyimpan draft: %v", err)
		return draft, errors.New("Draft tidak dapat disimpan.")
	}

	return draft, nil
}

func (a *API) runDraft(ctx context.Context, target Target, draft core.Draft, user core.User) (core.Attempt, error) {
	attempt, fingerprint, created, err := a.migrations.BeginAttempt(ctx, target.ID, draft.ID, draft.Revision, newAttemptID(), user)
	if errors.Is(err, store.ErrDraftConflict) {
		return attempt, errors.New("Revisi draft sudah berubah. Buka ulang draft.")
	}
	if err != nil {
		log.Printf("Gagal mencatat attempt: %v", err)
		return attempt, errors.New("Migration tidak dapat dimulai.")
	}
	if !created {
		return attempt, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := a.open(ctx, target)
	if err != nil {
		log.Printf("Database target tidak dapat diakses: %v", err)
		return a.finish(attempt.ID, core.AttemptFailed, nil, "Database target tidak dapat diakses")
	}
	defer conn.Close()

	version, err := conn.Execute(ctx, targetdb.Execution{
		ID:          attempt.ID,
		SQL:         attempt.SQL,
		ActorID:     attempt.ActorID,
		ActorName:   attempt.ActorName,
		BaseVersion: attempt.BaseVersion,
		Fingerprint: fingerprint,
	})
	switch {
	case err == nil:
		return a.finish(attempt.ID, core.AttemptSuccess, &version, "")
	case errors.Is(err, targetdb.ErrUnknown):
		return a.finish(attempt.ID, core.AttemptUnknown, nil, err.Error())
	case errors.Is(err, targetdb.ErrVersion), errors.Is(err, targetdb.ErrDrift), strings.HasPrefix(err.Error(), "statement "):
		return a.finish(attempt.ID, core.AttemptFailed, nil, err.Error())
	default:
		log.Printf("Migration %s gagal: %v", attempt.ID, err)
		return a.finish(attempt.ID, core.AttemptFailed, nil, "Migration gagal; seluruh perubahan dibatalkan")
	}
}

func (a *API) finish(id string, status core.AttemptStatus, version *int64, message string) (core.Attempt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	attempt, err := a.migrations.FinishAttempt(ctx, id, status, version, message)
	if err != nil {
		log.Printf("Gagal menyimpan hasil attempt %s: %v", id, err)
		return attempt, errors.New("Hasil migration belum tercatat. Lakukan rekonsiliasi dari riwayat.")
	}
	return attempt, nil
}

// reconcile memastikan hasil attempt UNKNOWN (atau RUNNING yang tertinggal) dengan mencarinya di ledger database target
func (a *API) reconcile(ctx context.Context, target Target, attempt core.Attempt) error {
	if attempt.Status != core.AttemptUnknown && attempt.Status != core.AttemptRunning {
		return nil
	}
	if attempt.Status == core.AttemptRunning && time.Since(attempt.StartedAt) < reconcileGrace {
		return nil
	}
	conn, err := a.open(ctx, target)
	if err != nil {
		return err
	}
	defer conn.Close()

	version, found, err := conn.Lookup(ctx, attempt.ID)
	if err != nil {
		return err
	}
	if found {
		_, err = a.finish(attempt.ID, core.AttemptSuccess, &version, "")
	} else {
		_, err = a.finish(attempt.ID, core.AttemptFailed, nil, "Tidak ada commit untuk attempt ini pada ledger database target")
	}

	return err
}

func newAttemptID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
