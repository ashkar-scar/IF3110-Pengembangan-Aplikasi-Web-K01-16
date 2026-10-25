package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgconn"
)

// Menyediakan operasi user yang dibutuhkan endpoint admin
type AdminUserSource interface {
	Create(ctx context.Context, user core.User) (core.User, error)
	FindAll(ctx context.Context) ([]core.User, error)
}

// User request
type createUserRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	PlatformAdmin bool   `json:"is_platform_admin"`
	Active        *bool  `json:"is_active"`
}

// Memasang repository user untuk endpoint admin
func (a *API) UseUsers(source AdminUserSource) {
	a.users = source
}

// Mendaftarkan endpoint pengelolaan user
func (a *API) userRoutes(mux *http.ServeMux) {
	if a.users == nil {
		return
	}

	mux.HandleFunc("GET /api/admin/users", a.adminOnly(a.listUsers))
	mux.HandleFunc("POST /api/admin/users", a.adminOnly(a.createUser))
}

// Mengirim daftar user kepada admin platform
func (a *API) listUsers(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	users, err := a.users.FindAll(r.Context())
	if err != nil {
		log.Printf("Gagal mengambil daftar user: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Gagal mengambil daftar user")
		return
	}

	writeJSON(w, http.StatusOK, users)
}

// Memvalidasi, melakukan hash password, dan menyimpan user baru
func (a *API) createUser(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	var request createUserRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.Username = strings.TrimSpace(request.Username)

	if request.Username == "" || len(request.Username) > 100 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Username wajib diisi dan maksimal 100 byte")
		return
	}

	if len(request.Password) < auth.MIN_PASSWORD_LENGTH || len(request.Password) > auth.MAX_PASSWORD_LENGTH {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Password harus berukuran 10-72 byte")
		return
	}

	passwordHash, err := auth.CreatePasswordHash(request.Password)
	if err != nil {
		log.Printf("Gagal membuat password hash: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Gagal membuat user")
		return
	}

	active := true
	if request.Active != nil {
		active = *request.Active
	}

	user, err := a.users.Create(r.Context(), core.User{
		Username:      request.Username,
		PasswordHash:  passwordHash,
		PlatformAdmin: request.PlatformAdmin,
		Active:        active,
	})

	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			writeError(w, http.StatusConflict, "CONFLICT", "Username sudah digunakan")
			return
		}

		log.Printf("Gagal menyimpan user: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Gagal membuat user")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}