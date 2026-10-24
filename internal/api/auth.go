package api

import (
	"encoding/json"
	"errors"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"log"
	"net/http"
	"time"
)

const SESSION_COOKIE_NAME = "yuzuki_akmal"

// JSON yang diterima endpoint login
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JSON yang dikirim setelah login berhasil
type loginResponse struct {
	User      core.User `json:"user"`
	CSRFToken string    `json:"csrf_token"`
}

// Menyimpan informasi error API
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Membungkus error sesuai format response API
type errorResponse struct {
	Error errorDetail `json:"error"`
}

// Login memeriksa kredensial dan membuat cookie sessionn
func (api *API) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest

	// Batasi ukuran body request maksimal satu megabyte
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	// Tolak JSON yang tidak valid
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Format request tidak valid")
		return
	}

	// Jalankan proses login melalui service
	result, err := api.authService.Login(r.Context(), request.Username, request.Password)

	if err != nil {
		// Username tidak ada dan password salah menghasilkan response sama
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Username atau password salah")
			return
		}

		log.Printf("Login gagal karena error internal: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
		return
	}

	// Token session asli hanya dikirim melalui cookie
	api.setSessionCookie(w, result.SessionToken, result.ExpiresAt)

	// Response hanya berisi user dan CSRF token
	writeJSON(w, http.StatusOK, loginResponse{
		User:      result.User,
		CSRFToken: result.CSRFToken,
	})
}

// logout mencabut session dan menghapus cookie
func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	// Ambil token session dari cookie
	cookie, err := r.Cookie(SESSION_COOKIE_NAME)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session tidak ditemukan")
		return
	}

	// Pastikan session masih valid dan user masih aktif
	result, err := api.authService.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidSession) {
			api.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Session tidak valid")
			return
		}

		log.Printf("Gagal memeriksa session: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
		return
	}

	// Cocokkan CSRF token dari header dengan session
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" || csrfToken != result.Session.CSRFToken {
		// Simpan token baru jika Authenticate melakukan rotasi
		if result.NewSessionToken != "" {
			api.setSessionCookie(w, result.NewSessionToken, result.Session.AbsoluteExpiresAt)
		}

		writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF token tidak valid")
		return
	}

	// Revoke session agar token ga bisa digunakan lagi
	if err := api.authService.Logout(r.Context(), cookie.Value); err != nil {
		log.Printf("Gagal logout: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Gagal melakukan logout")
		return
	}

	// Hapus cookie setelah session berhasil dicabut
	api.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Mengembalikan user dari session cookie yang valid
func (api *API) currentUser(w http.ResponseWriter, r *http.Request) {
	// Ambil token session dari cookie
	cookie, err := r.Cookie(SESSION_COOKIE_NAME)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session tidak ditemukan")
			return
		}

		writeError(w, http.StatusBadRequest, "INVALID_COOKIE", "Cookie tidak valid")
		return
	}

	// Periksa session dan ambil user pemiliknya
	result, err := api.authService.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidSession) {
			api.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Session tidak valid")
			return
		}

		log.Printf("Gagal memeriksa session: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
		return
	}

	// Perbarui cookie jika token dirotasi
	if result.NewSessionToken != "" {
		api.setSessionCookie(w, result.NewSessionToken, result.Session.AbsoluteExpiresAt)
	}

	writeJSON(w, http.StatusOK, loginResponse{
		User:      result.User,
		CSRFToken: result.Session.CSRFToken,
	})
}

// setSessionCookie mengirim token melalui cookie yang terlindungi
func (api *API) setSessionCookie(w http.ResponseWriter, sessionToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SESSION_COOKIE_NAME,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   api.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

// Menghapus cookie session dari browser
func (api *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SESSION_COOKIE_NAME,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   api.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// Mengirim response dalam format JSON
func writeJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Gagal menulis JSON response: %v", err)
	}
}

// Mengirim error
func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
	})
}
