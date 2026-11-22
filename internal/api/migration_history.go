package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

type historyResponse struct {
	History              []core.Attempt `json:"history"`
	HasMore              bool           `json:"has_more"`
	CanRollbackExecution *string        `json:"can_rollback_exec_id"`
	CanForwardExecution  *string        `json:"can_forward_exec_id"`
}

// migrationHistoryRoutes mendaftarkan endpoint JSON migration history.
func (a *API) migrationHistoryRoutes(mux *http.ServeMux) {
	if a.migrations == nil || a.targets == nil {
		return
	}

	mux.HandleFunc("GET /api/databases/{id}/history", a.RequireDatabasePermission(core.PermissionView, a.migrationHistory))
	mux.HandleFunc("POST /api/databases/{id}/history/{execID}/submit-rollback", a.RequireDatabasePermission(core.PermissionEdit, a.submitMigrationRollback))
	mux.HandleFunc("POST /api/databases/{id}/history/{execID}/submit-forward", a.RequireDatabasePermission(core.PermissionEdit, a.submitMigrationForward))
}

// submitMigrationRollback mengajukan rollback execution ke handler untuk melalui approval group
func (a *API) submitMigrationRollback(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	a.submitMigrationHistoryReview(w, r, session, core.ActionRollback)
}

// submitMigrationForward mengajukan ulang execution FORWARD ke handler yang sebelumnya berhasil di-rollback
func (a *API) submitMigrationForward(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	a.submitMigrationHistoryReview(w, r, session, core.ActionForward)
}

// submitMigrationHistoryReview membuat request rollback atau forward tanpa mengeksekusi SQL (handler utama rollbakc dan forward)
func (a *API) submitMigrationHistoryReview(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult, action core.MigrationAction) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	executionID := strings.TrimSpace(r.PathValue("execID"))
	if executionID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Execution migration tidak ditemukan")
		return
	}

	connection, err := a.open(r.Context(), target)
	if err != nil {
		targetError(w, err)
		return
	}

	defer connection.Close()
	baseVersion, err := connection.Version(r.Context())
	if err != nil {
		targetError(w, err)
		return
	}

	request, err := a.migrations.SubmitHistoryReview(r.Context(), target.ID, executionID, action, session.User.ID, baseVersion,)
	switch {
	case errors.Is(err, store.ErrAttemptNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Execution migration tidak ditemukan")

	case errors.Is(err, store.ErrHistoryActionNotAllowed):
		writeError(w, http.StatusConflict, "CONFLICT", "Execution tidak dapat diajukan untuk aksi ini")

	case errors.Is(err, store.ErrHistoryReviewExists):
		writeError(w, http.StatusConflict, "CONFLICT", "Pengajuan untuk execution ini masih aktif")

	case errors.Is(err, store.ErrNoGroupLeader):
		writeError(w, http.StatusUnprocessableEntity, "NO_GROUP_LEADER", "Setiap group berizin wajib memiliki leader aktif")

	case errors.Is(err, store.ErrNoApprovalGroup):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Database belum memiliki group berizin")

	case err != nil:
		log.Printf("Gagal submit %s migration: %v", action, err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Pengajuan migration tidak dapat dibuat")

	default:
		writeJSON(w, http.StatusCreated, request)
	}
}

// migrationHistory mengirim data history, pagination, dan execution yang dapat dirollback atau forward.
func (a *API) migrationHistory(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	query := r.URL.Query()
	offset := 0
	if rawOffset := query.Get("offset"); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)

		if err != nil || parsed < 0 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Offset tidak valid")
			return
		}

		offset = parsed
	}

	hideRollback := false
	if rawHide := query.Get("hide_rollback"); rawHide != "" {
		parsed, err := strconv.ParseBool(rawHide)

		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "hide_rollback harus true atau false")
			return
		}

		hideRollback = parsed
	}

	history, err := a.migrations.ListHistory(r.Context(), target.ID, hideRollback, offset)
	if err != nil {
		log.Printf("Gagal membaca migration history: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Migration history tidak dapat dimuat")
		return
	}

	hasMore := len(history) > store.AttemptsPerPage
	if hasMore {
		history = history[:store.AttemptsPerPage]
	}

	canRollback, canForward, err := a.migrations.HistoryActions(r.Context(), target.ID)
	if err != nil {
		log.Printf("Gagal membaca aksi migration history: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Aksi migration history tidak dapat ditentukan")
		return
	}

	writeJSON(w, http.StatusOK, historyResponse{
		History:              history,
		HasMore:              hasMore,
		CanRollbackExecution: canRollback,
		CanForwardExecution:  canForward,
	})
}