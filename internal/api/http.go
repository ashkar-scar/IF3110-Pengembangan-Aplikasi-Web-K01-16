package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// API menampung seluruh dependency dan handler API
type API struct {
}

// Bikin objek API baru
func Create() *API {
	return &API{}
}

// Membuat daftar route yang ada di aplikasi
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
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
