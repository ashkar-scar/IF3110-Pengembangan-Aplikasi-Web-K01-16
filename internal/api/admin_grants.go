package api

import (
	"context"
	"net/http"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

type AdminGrantSource interface {
	FindAll(context.Context) ([]core.DatabaseGrant, error)
	SetGrant(context.Context, core.DatabaseGrant) error
	RemoveGrant(context.Context, int64, int64) error
}

// Memasang repository grant pada API
func (a *API) UseGrants(source AdminGrantSource) {
	a.grants = source
}

// Mendaftarkan route pengelolaan grant untuk admin
func (a *API) grantRoutes(mux *http.ServeMux) {
	if a.grants == nil {
		return
	}

	mux.HandleFunc("GET /api/admin/grants", a.adminOnly(a.listGrants))
	mux.HandleFunc("PUT /api/admin/groups/{id}/grants/{targetID}", a.adminOnly(a.setGrant))
	mux.HandleFunc("DELETE /api/admin/groups/{id}/grants/{targetID}", a.adminOnly(a.removeGrant))
}

// Mengirim seluruh grant untuk halaman administrasi
func (a *API) listGrants(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	grants, err := a.grants.FindAll(r.Context())
	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, grants)
}

// Menetapkan permission group untuk satu database
func (a *API) setGrant(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	targetID, ok := pathID(w, r, "targetID")
	if !ok {
		return
	}

	var request struct {
		Permission string `json:"permission"`
	}

	if !readJSON(w, r, &request) {
		return
	}

	permission, err := core.ParsePermission(request.Permission)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Permission harus VIEW atau EDIT")
		return
	}

	grant := core.DatabaseGrant{
		GroupID:    groupID,
		DatabaseID: targetID,
		Permission: permission,
	}

	if err := a.grants.SetGrant(r.Context(), grant); err != nil {
		adminError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Menghapus permission group untuk satu database
func (a *API) removeGrant(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	targetID, ok := pathID(w, r, "targetID")
	if !ok {
		return
	}

	if err := a.grants.RemoveGrant(r.Context(), groupID, targetID); err != nil {
		adminError(w, err)
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}
