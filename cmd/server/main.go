package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/api"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

func main() {
	// GEt alamat PostgreSQL dari env
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL not found")
	}

	// Batasi proses koneksi maksimal 10 detik
	startupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Open and check databse connection
	database, err := store.NewPostgresPool(startupContext, databaseURL)
	if err != nil {
		log.Fatalf("Connection to PostgreSQL failed: %v", err)
	}

	defer database.Close() // Close connection to database

	// Buat repository untuk user dan session
	userRepository := store.NewUserRepository(database)
	sessionRepository := store.NewSessionRepository(database)

	// Buat service autentikasi
	authService := auth.NewService(userRepository, sessionRepository)

	// Cookie menggunakan Secure pada environment production
	secureCookies := os.Getenv("APP_ENV") == "production"

	// Buat API dengan service autentikasi.
	app := api.Create(authService, secureCookies)

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
