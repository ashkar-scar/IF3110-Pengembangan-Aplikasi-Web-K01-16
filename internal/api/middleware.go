package api

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// Menerima request yang sudah memiliki user dan session valid
type authenticatedHandler func(http.ResponseWriter, *http.Request, auth.AuthenticationResult)

// Memastikan request browser berasal dari origin aplikasi sendiri
func (a *API) validOrigin(r *http.Request) bool {
	// Browser menandai request dari website lain sebagai cross-site
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}

	origin := r.Header.Get("Origin")
	// Client nonbrowser seperti CLI dapat tidak mengirim header Origin
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || len(r.Header.Values("Origin")) != 1 {
		return false
	}

	// Origin hanya boleh berisi scheme dan host
	if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}

	// Production hanya menerima HTTPS sedangkan development boleh memakai HTTP
	validScheme := parsed.Scheme == "https" || (parsed.Scheme == "http" && !a.secureCookies)
	if !validScheme {
		return false
	}

	return strings.EqualFold(parsed.Host, r.Host)
}

// Memeriksa Origin dan token CSRF pada request yang mengubah data
func (a *API) validCSRFRequest(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) bool {
	if !requiresCSRF(r.Method) {
		return true
	}

	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "INVALID_CSRF", "Origin request tidak valid")
		return false
	}

	token := r.Header.Get("X-CSRF-Token")
	if token == "" || token != session.Session.CSRFToken {
		writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF token tidak valid")
		return false
	}

	return true
}

// Membatasi handler berdasarkan permission user pada database target
func (a *API) RequireDatabasePermission(required core.Permission, next authenticatedHandler) http.HandlerFunc {
	return a.authenticated(func(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
		if !a.validCSRFRequest(w, r, session) {
			return
		}

		// Mmastikan target tersedia dan permission mencukupi
		if _, ok := a.allowedTarget(w, r, session.User, required); !ok {
			return
		}

		next(w, r, session)
	})
}

// Memeriksa cookie dan session sebelum menjalankan handler
func (a *API) authenticated(next authenticatedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cookie membawa token session yang dikirim saat login
		cookie, err := r.Cookie(SESSION_COOKIE_NAME)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session tidak ditemukan")
			return
		}

		// Authenticate memeriksa user, masa berlaku, revoke, dan rotation
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

		// Kirim token baru ketika service melakukan session rotation
		if result.NewSessionToken != "" {
			a.setSessionCookie(w, result.NewSessionToken, result.Session.AbsoluteExpiresAt)
		}

		next(w, r, result)
	}
}

// Membatasi handler untuk admin dan memeriksa CSRF pada perubahan data
func (a *API) adminOnly(next authenticatedHandler) http.HandlerFunc {
	return a.authenticated(func(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
		if !session.User.CanManagePlatform() {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "Hanya admin platform yang dapat mengakses fitur ini")
			return
		}

		if !a.validCSRFRequest(w, r, session) {
			return
		}

		next(w, r, session)
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