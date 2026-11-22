package api

import (
	"log"
	"net/http"
	"strconv"

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