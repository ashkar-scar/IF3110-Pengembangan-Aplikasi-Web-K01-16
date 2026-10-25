package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
)

type loginPage struct {
	base
	Username string
	Error    string
}

func (a *API) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.pageSession(w, r); ok {
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
		return
	}

	render(w, http.StatusOK, "login", loginPage{base: base{Title: "Masuk"}})
}

func (a *API) submitLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	result, err := a.authService.Login(r.Context(), username, r.FormValue("password"))
	if err != nil {
		status, message := http.StatusUnauthorized, "Username atau password salah"
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			log.Printf("Login gagal karena error internal: %v", err)
			status, message = http.StatusInternalServerError, "Terjadi kesalahan pada server"
		}
		render(w, status, "login", loginPage{base: base{Title: "Masuk"}, Username: username, Error: message})
		return
	}

	a.setSessionCookie(w, result.SessionToken, result.ExpiresAt)
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (a *API) submitLogout(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	cookie, _ := r.Cookie(SESSION_COOKIE_NAME)
	if err := a.authService.Logout(r.Context(), cookie.Value); err != nil {
		log.Printf("Gagal logout: %v", err)
	}
	
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
