package main

import (
	"log"
	"net/http"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/api"
)

func main() {
	// Create new API
	app := api.Create()

	// Create server
	server := &http.Server{
		Addr:              ":8080",
		Handler:           app.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Server running in: http://localhost%s", server.Addr)

	// Jalanin server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to run: %v", err)
	}
}