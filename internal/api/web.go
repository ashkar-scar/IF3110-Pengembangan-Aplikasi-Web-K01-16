package api

import (
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/web"
)

// Setiap halaman di-parse bersama layout.html, sehingga semua halaman memakai kerangka yang sama
var templates = map[string]*template.Template{}

func init() {
	pages := []string{"login", "databases", "explorer", "migrations", "admin", "error"}
	for _, name := range pages {
		templates[name] = template.Must(template.New(name).Funcs(templateFuncs).ParseFS(web.Files, "templates/layout.html", "templates/"+name+".html"))
	}
}

var templateFuncs = template.FuncMap{
	"constraintKind": func(kind string) string {
		switch kind {
		case "p":
			return "PRIMARY KEY"
		case "f":
			return "FOREIGN KEY"
		case "u":
			return "UNIQUE"
		case "c":
			return "CHECK"
		}
		return kind
	},
}

type base struct {
	Title string
	User  core.User
	CSRF  string
}

func newBase(title string, session auth.AuthenticationResult) base {
	return base{Title: title, User: session.User, CSRF: session.Session.CSRFToken}
}

type errorPage struct {
	base
	Message string
}

// Routing
func (a *API) webRoutes(mux *http.ServeMux) {
	static, _ := fs.Sub(web.Files, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.submitLogin)
	mux.HandleFunc("POST /logout", a.loggedIn(a.submitLogout))
	mux.HandleFunc("GET /admin", a.loggedIn(a.adminPage))

	if a.targets == nil {
		return
	}

	mux.HandleFunc("GET /databases", a.loggedIn(a.databasesPage))
	mux.HandleFunc("GET /databases/{id}", a.loggedIn(a.explorerPage))

	if a.migrations == nil {
		return
	}

	mux.HandleFunc("GET /databases/{id}/migrations", a.loggedIn(a.migrationsPage))
	mux.HandleFunc("POST /databases/{id}/migrations", a.loggedIn(a.submitMigration))
	mux.HandleFunc("POST /databases/{id}/migrations/{attempt}/reconcile", a.loggedIn(a.reconcileAttempt))
}

func (a *API) loggedIn(next func(http.ResponseWriter, *http.Request, auth.AuthenticationResult)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.pageSession(w, r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !validFormToken(r, session) {
			a.showError(w, http.StatusForbidden, session, "Form sudah kedaluwarsa. Muat ulang halaman lalu coba lagi.")
			return
		}

		next(w, r, session)
	}
}

func validFormToken(r *http.Request, session auth.AuthenticationResult) bool {
	token := r.FormValue("csrf_token")
	return token != "" && token == session.Session.CSRFToken
}

func (a *API) pageSession(w http.ResponseWriter, r *http.Request) (auth.AuthenticationResult, bool) {
	cookie, err := r.Cookie(SESSION_COOKIE_NAME)
	if err != nil {
		return auth.AuthenticationResult{}, false
	}

	session, err := a.authService.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidSession) {
			log.Printf("Gagal memeriksa session: %v", err)
		}
		a.clearSessionCookie(w)
		return auth.AuthenticationResult{}, false
	}
	if session.NewSessionToken != "" {
		a.setSessionCookie(w, session.NewSessionToken, session.Session.AbsoluteExpiresAt)
	}
	
	return session, true
}

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates[name].ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("Gagal menampilkan halaman %s: %v", name, err)
	}
}

func (a *API) showError(w http.ResponseWriter, status int, session auth.AuthenticationResult, message string) {
	render(w, status, "error", errorPage{base: newBase("Halaman tidak tersedia", session), Message: message})
}
