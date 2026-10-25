package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
)

// API menampung seluruh dependency dan handler API
type API struct {
	authService   *auth.Service
	secureCookies bool
	targets       TargetSource
	dataDir       string
}

// Bikin objek API baru dengan service yang dibutuhkan
func Create(authService *auth.Service, secureCookies bool) *API {
	return &API{
		authService:   authService,
		secureCookies: secureCookies,
	}
}

// Membuat daftar route yang ada di aplikasi
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("GET /api/auth/current-user", a.currentUser)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	a.explorerRoutes(mux)
	return mux
}

// Handler request GET /health -> buat memastikan server hidup
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ok",
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Gagal menulis health response: %v", err)
	}
}
