package api

import (
	"context"
	"net/http"
	"strings"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

type AdminGroupSource interface {
	Create(context.Context, core.Group) (core.Group, error)
	FindAll(context.Context) ([]core.Group, error)
	FindMembers(context.Context, int64) ([]core.User, error)
	AddMember(context.Context, int64, int64) error
	RemoveMember(context.Context, int64, int64) error
}

type groupRequest struct {
	Name string `json:"name"`
}

type memberRequest struct {
	UserID int64 `json:"user_id"`
}

// Memasang repository group pada API
func (a *API) UseGroups(source AdminGroupSource) {
	a.groups = source
}

// Mendaftarkan route group dan membership untuk admin
func (a *API) groupRoutes(mux *http.ServeMux) {
	if a.groups == nil {
		return
	}

	mux.HandleFunc("GET /api/admin/groups", a.adminOnly(a.listGroups))
	mux.HandleFunc("POST /api/admin/groups", a.adminOnly(a.createGroup))
	mux.HandleFunc("GET /api/admin/groups/{id}/members", a.adminOnly(a.listMembers))
	mux.HandleFunc("POST /api/admin/groups/{id}/members", a.adminOnly(a.addMember))
	mux.HandleFunc("DELETE /api/admin/groups/{id}/members/{userID}", a.adminOnly(a.removeMember))
}

// Mengirim seluruh group yang tersimpan
func (a *API) listGroups(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groups, err := a.groups.FindAll(r.Context())

	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, groups)
}

// Membuat group baru setelah nama divalidasi
func (a *API) createGroup(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	var request groupRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 100 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Nama group wajib diisi dan maksimal 100 byte")
		return
	}

	group, err := a.groups.Create(r.Context(), core.Group{Name: request.Name})
	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, group)
}

// Mengirim seluruh anggota dari satu group
func (a *API) listMembers(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groupID, ok := pathID(w, r, "id")
	
	if !ok {
		return
	}

	members, err := a.groups.FindMembers(r.Context(), groupID)
	
	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, members)
}

// Menambahkan user sebagai anggota group
func (a *API) addMember(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var request memberRequest
	if !readJSON(w, r, &request) {
		return
	}

	if request.UserID < 1 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "User ID tidak valid")
		return
	}

	if err := a.groups.AddMember(r.Context(), groupID, request.UserID); err != nil {
		adminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Menghapus user dari keanggotaan group
func (a *API) removeMember(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	groupID, ok := pathID(w, r, "id")

	if !ok {
		return
	}

	userID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}

	if err := a.groups.RemoveMember(r.Context(), groupID, userID); err != nil {
		adminError(w, err)
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}
