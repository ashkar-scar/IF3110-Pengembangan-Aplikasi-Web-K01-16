package api

import (
	"context"
	"net/http"
	"strings"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// User editor interface
type userEditor interface {
	Update(context.Context, core.User) (core.User, error)
}

// group editor interface
type groupEditor interface {
	Rename(context.Context, int64, string) error
	Delete(context.Context, int64) error
}

// renamegroup request 
type renameGroupRequest struct {
	Name string `json:"name"`
}

// Mendaftarkan route edit jika repository mendukung operasinya
func (a *API) editRoutes(mux *http.ServeMux) {
	if _, ok := a.users.(userEditor); ok {
		mux.HandleFunc("PUT /api/admin/users/{id}", a.adminOnly(a.updateUser))
	}

	if _, ok := a.groups.(groupEditor); ok {
		mux.HandleFunc("PUT /api/admin/groups/{id}", a.adminOnly(a.renameGroup))
		mux.HandleFunc("DELETE /api/admin/groups/{id}", a.adminOnly(a.deleteGroup))
	}
}

// Memperbarui akun user tanpa mengirim password hash ke client
func (a *API) updateUser(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	id, ok := pathID(w, r, "id")

	if !ok {
		return
	}

	var request createUserRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || len(request.Username) > 100 || request.Active == nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Isi username, is_active, dan is_platform_admin.")
		return
	}

	// Cegah admin mengunci atau menurunkan hak akunnya sendiri
	if id == session.User.ID && (!*request.Active || !request.PlatformAdmin) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Admin tidak dapat menonaktifkan atau menurunkan hak akunnya sendiri.")
		return
	}

	// Password kosong berarti password lama tetap digunakan
	hash := ""
	var err error
	if request.Password != "" {
		hash, err = auth.CreatePasswordHash(request.Password)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Password harus 10-72 byte.")
			return
		}
	}

	user, err := a.users.(userEditor).Update(r.Context(), core.User{
		ID:            id,
		Username:      request.Username,
		PasswordHash:  hash,
		Active:        *request.Active,
		PlatformAdmin: request.PlatformAdmin,
	})

	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// Mengubah nama group
func (a *API) renameGroup(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	id, ok := pathID(w, r, "id")

	if !ok {
		return
	}

	var request renameGroupRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 100 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Nama group wajib diisi dan maksimal 100 byte.")
		return
	}

	if err := a.groups.(groupEditor).Rename(r.Context(), id, request.Name); err != nil {
		adminError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Menghapus group beserta membership dan grant terkait
func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	id, ok := pathID(w, r, "id")

	if !ok {
		return
	}

	if err := a.groups.(groupEditor).Delete(r.Context(), id); err != nil {
		adminError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
