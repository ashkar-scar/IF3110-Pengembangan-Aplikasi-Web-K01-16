package api

import (
	"context"
	"encoding/json"
	"errors"
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

type draftRequest struct {
	Description string `json:"description"`
	SQL         string `json:"sql"`
	Revision    int    `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

type executeRequest struct {
	Revision  int    `json:"revision"`
	RequestID string `json:"request_id"`
}

// TODO(faiz): panggil dari main.go bersama UseTargets setelah tabel migration dibuat di database platform.
func (a *API) UseMigrations(repository *store.MigrationRepository) {
	a.migrations = repository
}

// Routing
func (a *API) migrationRoutes(mux *http.ServeMux) {
	if a.targets == nil || a.migrations == nil {
		return
	}

	mux.HandleFunc("GET /api/databases/{id}/drafts", a.signedIn(a.listDrafts))
	mux.HandleFunc("POST /api/databases/{id}/drafts", a.signedIn(a.saveDraft))
	mux.HandleFunc("GET /api/databases/{id}/drafts/{draft}", a.signedIn(a.getDraft))
	mux.HandleFunc("PATCH /api/databases/{id}/drafts/{draft}", a.signedIn(a.saveDraft))
	mux.HandleFunc("POST /api/databases/{id}/drafts/{draft}/execute", a.signedIn(a.executeDraft))
	mux.HandleFunc("GET /api/databases/{id}/migrations", a.signedIn(a.listAttempts))
	mux.HandleFunc("GET /api/databases/{id}/migrations/{attempt}", a.signedIn(a.getAttempt))
	mux.HandleFunc("POST /api/databases/{id}/migrations/{attempt}/reconcile", a.signedIn(a.reconcile))
}

func validCSRF(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) bool {
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || token != session.Session.CSRFToken {
		writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF token tidak valid")
		return false
	}

	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Format request tidak valid")
		return false
	}

	return true
}

func internalError(w http.ResponseWriter, message string, err error) {
	log.Printf("%s: %v", message, err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
}

func draftID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("draft"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Draft tidak ditemukan")
		return 0, false
	}

	return id, true
}

func (a *API) listDrafts(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	drafts, err := a.migrations.ListDrafts(r.Context(), target.ID)
	if err != nil {
		internalError(w, "Gagal membaca draft", err)
		return
	}

	writeJSON(w, http.StatusOK, drafts)
}

func (a *API) getDraft(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	id, ok := draftID(w, r)
	if !ok {
		return
	}

	draft, err := a.migrations.GetDraft(r.Context(), target.ID, id)
	if errors.Is(err, store.ErrDraftNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Draft tidak ditemukan")
		return
	}

	if err != nil {
		internalError(w, "Gagal membaca draft", err)
		return
	}

	writeJSON(w, http.StatusOK, draft)
}

func (a *API) saveDraft(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !validCSRF(w, r, session) {
		return
	}

	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	var input draftRequest
	if !readJSON(w, r, &input) {
		return
	}

	input.Description = strings.TrimSpace(input.Description)
	if len(input.Description) < 2 || len(input.Description) > 200 || strings.TrimSpace(input.SQL) == "" || len(input.SQL) > 65536 || len(input.Fingerprint) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Deskripsi harus 2-200 karakter dan SQL tidak boleh kosong atau melebihi 64 KiB")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := a.open(ctx, target)
	if err != nil {
		targetError(w, err)
		return
	}
	defer conn.Close()

	version, err := conn.Version(ctx)
	if err != nil {
		targetError(w, err)
		return
	}

	draft := core.Draft{TargetID: target.ID, Description: input.Description, SQL: input.SQL, CreatedBy: session.User.ID, BaseVersion: version, Fingerprint: input.Fingerprint, Revision: input.Revision}
	if r.PathValue("draft") == "" {
		draft, err = a.migrations.CreateDraft(r.Context(), draft)
	} else {
		if draft.ID, ok = draftID(w, r); !ok {
			return
		}
		draft, err = a.migrations.UpdateDraft(r.Context(), draft)
	}
	if errors.Is(err, store.ErrDraftConflict) {
		writeError(w, http.StatusConflict, "DRAFT_CONFLICT", "Draft berubah, sudah diterapkan, atau sedang dieksekusi. Muat ulang atau buat draft baru")
		return
	}
	if err != nil {
		internalError(w, "Gagal menyimpan draft", err)
		return
	}

	writeJSON(w, http.StatusOK, draft)
}

func (a *API) executeDraft(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !validCSRF(w, r, session) {
		return
	}

	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	id, ok := draftID(w, r)
	if !ok {
		return
	}

	var input executeRequest
	if !readJSON(w, r, &input) {
		return
	}
	if len(input.RequestID) < 16 || len(input.RequestID) > 80 || input.Revision < 1 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Request ID dan revisi wajib diisi")
		return
	}

	attempt, fingerprint, created, err := a.migrations.BeginAttempt(r.Context(), target.ID, id, input.Revision, input.RequestID, session.User)
	switch {
	case errors.Is(err, store.ErrDraftNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Draft tidak ditemukan")
		return
	case errors.Is(err, store.ErrDraftConflict):
		writeError(w, http.StatusConflict, "DRAFT_CONFLICT", "Revisi draft tidak sesuai. Muat ulang draft")
		return
	case err != nil:
		internalError(w, "Gagal mencatat attempt", err)
		return
	}

	if !created {
		writeJSON(w, http.StatusOK, attempt)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	conn, err := a.open(ctx, target)
	if err != nil {
		a.finish(attempt.ID, core.AttemptFailed, nil, "Database target tidak tersedia")
		targetError(w, err)
		return
	}
	defer conn.Close()

	version, runErr := conn.Execute(ctx, targetdb.Execution{
		ID:          attempt.ID,
		SQL:         attempt.SQL,
		ActorID:     attempt.ActorID,
		ActorName:   attempt.ActorName,
		BaseVersion: attempt.BaseVersion,
		Fingerprint: fingerprint,
	})

	status, message, applied := core.AttemptSuccess, "", &version
	switch {
	case runErr == nil:
	case errors.Is(runErr, targetdb.ErrUnknown):
		status, message, applied = core.AttemptUnknown, runErr.Error(), nil
	case errors.Is(runErr, targetdb.ErrVersion), errors.Is(runErr, targetdb.ErrDrift), strings.HasPrefix(runErr.Error(), "statement "):
		status, message, applied = core.AttemptFailed, runErr.Error(), nil
	default:
		status, message, applied = core.AttemptFailed, "Migration gagal; seluruh perubahan dibatalkan", nil
		log.Printf("Migration %s gagal: %v", attempt.ID, runErr)
	}

	attemptID := attempt.ID
	attempt, err = a.finish(attemptID, status, applied, message)
	if err != nil {
		log.Printf("Gagal memperbarui attempt %s: %v", attemptID, err)
		writeError(w, http.StatusServiceUnavailable, "RECONCILE_REQUIRED", "Hasil migration perlu direkonsiliasi; jangan ulangi SQL sebagai draft baru")
		return
	}

	switch {
	case status == core.AttemptUnknown:
		writeJSON(w, http.StatusAccepted, attempt)
	case errors.Is(runErr, targetdb.ErrVersion), errors.Is(runErr, targetdb.ErrDrift):
		writeError(w, http.StatusConflict, "VERSION_CONFLICT", message)
	case runErr != nil:
		writeError(w, http.StatusUnprocessableEntity, "MIGRATION_FAILED", message)
	default:
		writeJSON(w, http.StatusOK, attempt)
	}
}

func (a *API) finish(id string, status core.AttemptStatus, version *int64, message string) (core.Attempt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return a.migrations.FinishAttempt(ctx, id, status, version, message)
}

func (a *API) listAttempts(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	attempts, err := a.migrations.ListAttempts(r.Context(), target.ID)
	if err != nil {
		internalError(w, "Gagal membaca riwayat migration", err)
		return
	}

	writeJSON(w, http.StatusOK, attempts)
}

func (a *API) getAttempt(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	attempt, err := a.migrations.GetAttempt(r.Context(), target.ID, r.PathValue("attempt"))
	if errors.Is(err, store.ErrAttemptNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Riwayat migration tidak ditemukan")
		return
	}

	if err != nil {
		internalError(w, "Gagal membaca riwayat migration", err)
		return
	}

	writeJSON(w, http.StatusOK, attempt)
}

func (a *API) reconcile(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !validCSRF(w, r, session) {
		return
	}

	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	attempt, err := a.migrations.GetAttempt(r.Context(), target.ID, r.PathValue("attempt"))
	if errors.Is(err, store.ErrAttemptNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Riwayat migration tidak ditemukan")
		return
	}
	if err != nil {
		internalError(w, "Gagal membaca riwayat migration", err)
		return
	}

	if attempt.Status != core.AttemptUnknown && attempt.Status != core.AttemptRunning {
		writeJSON(w, http.StatusOK, attempt)
		return
	}
	if attempt.Status == core.AttemptRunning && time.Since(attempt.StartedAt) < reconcileGrace {
		writeJSON(w, http.StatusAccepted, attempt)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	conn, err := a.open(ctx, target)
	if err != nil {
		targetError(w, err)
		return
	}
	defer conn.Close()

	version, found, err := conn.Lookup(ctx, attempt.ID)
	if err != nil {
		targetError(w, err)
		return
	}
	if found {
		attempt, err = a.finish(attempt.ID, core.AttemptSuccess, &version, "")
	} else {
		attempt, err = a.finish(attempt.ID, core.AttemptFailed, nil, "Tidak ada commit untuk attempt ini pada ledger database target")
	}
	if err != nil {
		internalError(w, "Gagal memperbarui hasil rekonsiliasi", err)
		return
	}
	
	writeJSON(w, http.StatusOK, attempt)
}
