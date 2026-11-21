package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

type draftRequest struct {
	Description        string             `json:"description"`
	SQLUp              string             `json:"sql_up"`
	SQLDown            string             `json:"sql_down"`
	IsIrreversible     bool               `json:"is_irreversible"`
	IrreversibleReason string             `json:"irreversible_reason"`
	ExecutionMode      core.ExecutionMode `json:"execution_mode"`
	BatchColumn        string             `json:"batch_column"`
	BatchSize          int                `json:"batch_size"`
	Fingerprint        string             `json:"fingerprint"`
}

type reviewDecisionRequest struct {
	Note string `json:"note"`
}

// migrationDraftRoutes mendaftarkan endpoint untuk membaca, membuat, dan mengubah draft migration
func (a *API) migrationDraftRoutes(mux *http.ServeMux) {
	if a.migrations == nil || a.targets == nil {
		return
	}
	mux.HandleFunc("GET /api/databases/{id}/drafts", a.RequireDatabasePermission(core.PermissionView, a.listMigrationDrafts))
	mux.HandleFunc("POST /api/databases/{id}/drafts", a.RequireDatabasePermission(core.PermissionEdit, a.createMigrationDraft))
	mux.HandleFunc("PUT /api/databases/{id}/drafts/{draftID}", a.RequireDatabasePermission(core.PermissionEdit, a.updateMigrationDraft))
	mux.HandleFunc("POST /api/databases/{id}/drafts/{draftID}/submit-review", a.RequireDatabasePermission(core.PermissionEdit, a.submitMigrationDraftReview))
	mux.HandleFunc("GET /api/databases/{id}/reviews", a.RequireDatabasePermission(core.PermissionView, a.listMigrationReviews))
	mux.HandleFunc("POST /api/databases/{id}/reviews/{reqID}/approve", a.RequireDatabasePermission(core.PermissionView, a.approveMigrationReview))
	mux.HandleFunc("POST /api/databases/{id}/reviews/{reqID}/reject", a.RequireDatabasePermission(core.PermissionView, a.rejectMigrationReview))
}

// listMigrationReviews mengirim request review dan approval group untuk satu database kepada forntend.
func (a *API) listMigrationReviews(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	reviews, err := a.migrations.ListReviews(r.Context(), target.ID, session.User.ID)
	if err != nil {
		log.Printf("Gagal membaca migration review: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Migration review tidak dapat dimuat")
		return
	}

	writeJSON(w, http.StatusOK, reviews)
}

// reviewDecisionError mengubah error keputusan review menjadi response HTTP.
func reviewDecisionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Migration review tidak ditemukan")

	case errors.Is(err, store.ErrReviewFinal):
		writeError(w, http.StatusConflict, "CONFLICT", "Migration review sudah memiliki keputusan final")

	case errors.Is(err, store.ErrNotGroupLeader):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Anda bukan leader yang dapat memutuskan review ini")

	default:
		log.Printf("Gagal menyimpan keputusan migration review: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Keputusan review tidak dapat disimpan")
	}
}

// approveMigrationReview menyetujui review sebagai leader group pengguna saat ini.
func (a *API) approveMigrationReview(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	requestID, ok := pathID(w, r, "reqID")
	if !ok {
		return
	}

	var input reviewDecisionRequest
	if !readJSON(w, r, &input) {
		return
	}

	request, err := a.migrations.ApproveReview(r.Context(), target.ID, requestID, session.User.ID, input.Note)
	if err != nil {
		reviewDecisionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, request)
}

// rejectMigrationReview menolak review sebagai leader dengan alasan yang wajib diisi.
func (a *API) rejectMigrationReview(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	requestID, ok := pathID(w, r, "reqID")
	if !ok {
		return
	}

	var input reviewDecisionRequest
	if !readJSON(w, r, &input) {
		return
	}

	input.Note = strings.TrimSpace(input.Note)
	if input.Note == "" {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Alasan penolakan wajib diisi")
		return
	}

	request, err := a.migrations.RejectReview(r.Context(), target.ID, requestID, session.User.ID, input.Note)
	if err != nil {
		reviewDecisionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, request)
}

// submitMigrationDraftReview membuat request approval dan mengunci snapshot draft.
func (a *API) submitMigrationDraftReview(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	draftID, ok := pathID(w, r, "draftID")
	if !ok {
		return
	}

	request, err := a.migrations.SubmitReview(r.Context(), target.ID, draftID, session.User.ID)
	switch {
	case errors.Is(err, store.ErrDraftNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Draft migration tidak ditemukan")

	case errors.Is(err, store.ErrDraftImmutable), errors.Is(err, store.ErrDraftConflict):
		writeError(w, http.StatusConflict, "IMMUTABLE", "Draft sudah diajukan atau dieksekusi")

	case errors.Is(err, store.ErrDraftInvalid):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Draft belum memenuhi syarat untuk diajukan")

	case errors.Is(err, store.ErrNoGroupLeader):
		writeError(w, http.StatusUnprocessableEntity, "NO_GROUP_LEADER", "Setiap group berizin wajib memiliki leader aktif")

	case errors.Is(err, store.ErrNoApprovalGroup):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Database belum memiliki group berizin")

	case err != nil:
		log.Printf("Gagal submit review migration: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Draft tidak dapat diajukan untuk review")

	default:
		writeJSON(w, http.StatusCreated, request)
	}
}

// validateDraftRequest membersihkan input dan memastikan aturan draft
func validateDraftRequest(request *draftRequest) (string, bool) {
	request.Description = strings.TrimSpace(request.Description)
	request.SQLUp = strings.TrimSpace(request.SQLUp)
	request.SQLDown = strings.TrimSpace(request.SQLDown)
	request.IrreversibleReason = strings.TrimSpace(request.IrreversibleReason)
	request.BatchColumn = strings.TrimSpace(request.BatchColumn)

	if len(request.Description) < 2 || len(request.Description) > 200 {
		return "Deskripsi harus 2 sampai 200 karakter", false
	}

	if request.SQLUp == "" || len(request.SQLUp) > 65536 {
		return "SQL up wajib diisi dan maksimal 64 KiB", false
	}

	if request.IsIrreversible {
		if request.IrreversibleReason == "" {
			return "Migration irreversible wajib memiliki alasan", false
		}

	} else if request.SQLDown == "" {
		return "Migration reversible wajib memiliki SQL down", false
	}

	switch request.ExecutionMode {
	case "":
		request.ExecutionMode = core.ExecutionNormal

	case core.ExecutionNormal:
		request.BatchColumn = ""

	case core.ExecutionBatch:
		if request.BatchColumn == "" {
			return "Mode batch wajib memiliki batch_column", false
		}

	default:
		return "execution_mode harus NORMAL atau BATCH", false
	}

	if request.BatchSize == 0 {
		request.BatchSize = 1000
	}

	if request.BatchSize < 1 || request.BatchSize > 100000 {
		return "batch_size harus antara 1 dan 100000", false
	}

	return "", true
}

// draftFromRequest mengubah payload JSON menjadi model draft dengan data aman dari backend
func draftFromRequest(request draftRequest, targetID, actorID int64) core.Draft {
	return core.Draft{
		TargetID:           targetID,
		Description:        request.Description,
		SQL:                request.SQLUp,
		SQLUp:              request.SQLUp,
		SQLDown:            request.SQLDown,
		IsIrreversible:     request.IsIrreversible,
		IrreversibleReason: request.IrreversibleReason,
		ExecutionMode:      request.ExecutionMode,
		BatchColumn:        request.BatchColumn,
		BatchSize:          request.BatchSize,
		Fingerprint:        request.Fingerprint,
		CreatedBy:          actorID,
	}
}

// listMigrationDrafts mengirim seluruh draft milik database yang boleh dilihat pengguna
func (a *API) listMigrationDrafts(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}

	drafts, err := a.migrations.ListDrafts(r.Context(), target.ID)
	if err != nil {
		log.Printf("Gagal membaca draft migration: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Draft migration tidak dapat dimuat")
		return
	}

	writeJSON(w, http.StatusOK, drafts)
}

// createMigrationDraft memvalidasi payload lalu menyimpan draft baru untuk database target
func (a *API) createMigrationDraft(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	var request draftRequest
	if !readJSON(w, r, &request) {
		return
	}

	if message, valid := validateDraftRequest(&request); !valid {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", message)
		return
	}

	draft := draftFromRequest(request, target.ID, session.User.ID)
	conn, err := a.open(r.Context(), target)
	if err != nil {
		targetError(w, err)
		return
	}

	defer conn.Close()
	draft.BaseVersion, err = conn.Version(r.Context())
	if err != nil {
		targetError(w, err)
		return
	}

	draft, err = a.migrations.CreateDraft(r.Context(), draft)
	if err != nil {
		log.Printf("Gagal membuat draft migration: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Draft migration tidak dapat dibuat")
		return
	}

	writeJSON(w, http.StatusCreated, draft)
}

// updateMigrationDraft memperbarui draft yang belum terkunci dengan pemeriksaan konflik revisi
func (a *API) updateMigrationDraft(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	draftID, ok := pathID(w, r, "draftID")
	if !ok {
		return
	}

	saved, err := a.migrations.GetDraft(r.Context(), target.ID, draftID)
	if errors.Is(err, store.ErrDraftNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Draft migration tidak ditemukan")
		return
	}

	if err != nil {
		log.Printf("Gagal membaca draft migration: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Draft migration tidak dapat dibaca")
		return
	}

	if saved.IsLocked {
		writeError(w, http.StatusConflict, "IMMUTABLE", "Draft yang sudah diajukan atau dieksekusi tidak dapat diubah")
		return
	}

	var request draftRequest
	if !readJSON(w, r, &request) {
		return
	}

	if message, valid := validateDraftRequest(&request); !valid {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", message)
		return
	}

	draft := draftFromRequest(request, target.ID, saved.CreatedBy)
	draft.ID = saved.ID
	draft.Revision = saved.Revision
	conn, err := a.open(r.Context(), target)
	if err != nil {
		targetError(w, err)
		return
	}

	defer conn.Close()
	draft.BaseVersion, err = conn.Version(r.Context())
	if err != nil {
		targetError(w, err)
		return
	}

	draft, err = a.migrations.UpdateDraft(r.Context(), draft)
	if errors.Is(err, store.ErrDraftConflict) {
		writeError(w, http.StatusConflict, "CONFLICT", "Draft sudah berubah atau telah dikunci; muat ulang data")
		return
	}

	if err != nil {
		log.Printf("Gagal memperbarui draft migration: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Draft migration tidak dapat diperbarui")
		return
	}

	writeJSON(w, http.StatusOK, draft)
}