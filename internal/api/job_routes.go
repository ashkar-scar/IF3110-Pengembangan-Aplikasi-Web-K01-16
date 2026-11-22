package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
)

type JobSource interface {
	queue.JobEnqueuer
	Get(context.Context, string) (queue.Job, error)
	List(context.Context, int64, int) ([]queue.Job, error)
}

func (a *API) UseJobs(source JobSource) { a.jobs = source; a.UseJobEnqueuer(source) }
func (a *API) jobRoutes(mux *http.ServeMux) {
	if a.jobs == nil || a.targets == nil {
		return
	}
	mux.HandleFunc("GET /api/databases/{id}/jobs", a.RequireDatabasePermission(core.PermissionView, a.listJobs))
	mux.HandleFunc("GET /api/jobs/{jobID}", a.authenticated(a.getJob))
	mux.HandleFunc("POST /api/jobs/{jobID}/retry", a.authenticated(a.retryJob))
	mux.HandleFunc("POST /api/jobs/{jobID}/cancel-batch", a.authenticated(a.cancelJob))
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, s.User, core.PermissionView)
	if !ok {
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		var err error
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			writeError(w, 400, "INVALID_REQUEST", "Offset tidak valid")
			return
		}
	}
	jobs, err := a.jobs.List(r.Context(), target.ID, offset)
	if err != nil {
		jobError(w, err)
		return
	}
	more := len(jobs) > 200
	if more {
		jobs = jobs[:200]
	}
	if !target.Permission.Can(core.PermissionEdit) {
		for i := range jobs {
			jobs[i].CanRetry = false
			jobs[i].CanCancelBatch = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "has_more": more})
}

func (a *API) authorizedJob(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult, permission core.Permission) (queue.Job, bool) {
	if !a.validCSRFRequest(w, r, s) {
		return queue.Job{}, false
	}
	if requiresCSRF(r.Method) && !jobJSONRequest(w, r) {
		return queue.Job{}, false
	}
	j, err := a.jobs.Get(r.Context(), r.PathValue("jobID"))
	if err != nil {
		jobError(w, err)
		return j, false
	}
	r.SetPathValue("id", strconv.FormatInt(j.TargetID, 10))
	target, ok := a.allowedTarget(w, r, s.User, permission)
	if !target.Permission.Can(core.PermissionEdit) {
		j.CanRetry = false
		j.CanCancelBatch = false
	}
	return j, ok
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult) {
	j, ok := a.authorizedJob(w, r, s, core.PermissionView)
	if !ok {
		return
	}
	writeJSON(w, 200, j)
}
func (a *API) retryJob(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult) {
	j, ok := a.authorizedJob(w, r, s, core.PermissionEdit)
	if !ok {
		return
	}
	id, err := a.jobs.Retry(r.Context(), j.ID, s.User.ID, s.User.Username)
	if err != nil {
		jobError(w, err)
		return
	}
	writeJSON(w, 202, enqueueResponse{JobID: id, Status: "PENDING"})
}
func (a *API) cancelJob(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult) {
	j, ok := a.authorizedJob(w, r, s, core.PermissionEdit)
	if !ok {
		return
	}
	id, err := a.jobs.CancelBatch(r.Context(), j.ID, s.User.ID, s.User.Username)
	if err != nil {
		jobError(w, err)
		return
	}
	requestID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		jobError(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"request_id": requestID})
}
func jobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, queue.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Job tidak ditemukan")
	case errors.Is(err, queue.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Izin EDIT diperlukan")
	case errors.Is(err, queue.ErrApproval):
		writeError(w, 409, "APPROVAL_REQUIRED", "Persetujuan seluruh leader aktif diperlukan")
	case errors.Is(err, queue.ErrConflict):
		writeError(w, 409, "CONFLICT", "Status job tidak mengizinkan operasi ini")
	case errors.Is(err, queue.ErrNoGroupLeader):
		writeError(w, 422, "NO_GROUP_LEADER", "Setiap group berizin wajib memiliki leader aktif")
	default:
		writeError(w, 500, "INTERNAL_ERROR", "Operasi job gagal")
	}
}

func jobJSONRequest(w http.ResponseWriter, r *http.Request) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		writeError(w, 400, "INVALID_REQUEST", "Content-Type harus application/json")
		return false
	}
	return true
}
