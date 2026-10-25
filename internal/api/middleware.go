package api

import (
	"errors"
	"log"
	"net/http"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
)

// Menerima request yang sudah memiliki user dan session valid
type authenticatedHandler func(http.ResponseWriter, *http.Request, auth.AuthenticationResult)

// Memeriksa cookie dan session sebelum menjalankan handler
func (a *API) authenticated(next authenticatedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SESSION_COOKIE_NAME)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session tidak ditemukan")
			return
		}

		result, err := a.authService.Authenticate(r.Context(), cookie.Value)
		if errors.Is(err, auth.ErrInvalidSession) {
			a.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Session tidak valid")
			return
		}

		if err != nil {
			log.Printf("Gagal memeriksa session: %v", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
			return
		}

		if result.NewSessionToken != "" {
			a.setSessionCookie(w, result.NewSessionToken, result.Session.AbsoluteExpiresAt)
		}

		next(w, r, result)
	}
}

// Membatasi handler untuk admin dan memeriksa CSRF pada perubahan data
func (a *API) adminOnly(next authenticatedHandler) http.HandlerFunc {
	return a.authenticated(func(w http.ResponseWriter, r *http.Request, result auth.AuthenticationResult) {
		if !result.User.CanManagePlatform() {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "Hanya admin platform yang dapat mengakses fitur ini")
			return
		}

		if requiresCSRF(r.Method) {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" || token != result.Session.CSRFToken {
				writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF token tidak valid")
				return
			}
		}

		next(w, r, result)
	})
}

// Menandai method HTTP yang mengubah data
func requiresCSRF(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}