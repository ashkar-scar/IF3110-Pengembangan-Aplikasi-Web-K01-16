package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

type enqueueResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// UseJobEnqueuer implementasi queue yang disediakan worker.
func (a *API) UseJobEnqueuer(enqueuer queue.JobEnqueuer) {
	a.jobEnqueuer = enqueuer
}

// migrationEnqueueRoutes mendaftarkan endpoint enqueue request
func (a *API) migrationEnqueueRoutes(mux *http.ServeMux) {
	if a.migrations == nil || a.targets == nil {
		return
	}

	mux.HandleFunc("POST /api/databases/{id}/reviews/{reqID}/enqueue", a.RequireDatabasePermission(core.PermissionEdit, a.enqueueMigrationReview))
}

// enqueueMigrationReview memvalidasi request lalu mengirimkannya ke queue worker.
func (a *API) enqueueMigrationReview(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	requestID, ok := pathID(w, r, "reqID")
	if !ok {
		return
	}

	request, err := a.migrations.GetApprovedRequest(r.Context(), target.ID, requestID)
	switch {
	case errors.Is(err, store.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Migration request tidak ditemukan")
		return

	case errors.Is(err, store.ErrRequestNotApproved):
		writeError(w, http.StatusConflict, "APPROVAL_REQUIRED", "Migration request belum disetujui atau sudah masuk queue")
		return

	case err != nil:
		log.Printf("Gagal membaca request untuk enqueue: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Migration request tidak dapat diproses")
		return
	}

	if a.jobEnqueuer == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Queue worker belum tersedia")
		return
	}

	jobID, err := a.jobEnqueuer.Enqueue(r.Context(), queue.EnqueueRequest{
		RequestID: request.ID,
		TargetID:  request.TargetID,
		ActorID:   session.User.ID,
		ActorName: session.User.Username,
		Action:    queue.ActionType(request.Action),
	})

	if err != nil {
		log.Printf("Gagal enqueue migration request %d: %v", request.ID, err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Migration request tidak dapat dimasukkan ke queue")
		return
	}

	if err = a.migrations.MarkRequestEnqueued(r.Context(), target.ID, request.ID); err != nil {
		log.Printf("Job %s dibuat tetapi status request %d gagal diperbarui: %v", jobID, request.ID, err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Job dibuat tetapi status request belum tersinkronisasi")
		return
	}

	writeJSON(w, http.StatusAccepted, enqueueResponse{JobID: jobID, Status: "PENDING"})
}