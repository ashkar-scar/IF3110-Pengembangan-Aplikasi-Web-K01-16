package main

import (
	"context"
	"encoding/base64"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/api"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := observability.Init(ctx, "ruangdb-api")
	if err != nil {
		log.Fatal("Gagal menyiapkan telemetry")
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownTelemetry(flushCtx)
	}()
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
	defer app.Close()
	app.UseUsers(userRepository)
	groupRepository := store.NewGroupRepository(database)
	permissionRepository := store.NewPermissionRepository(database)
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TARGET_CREDENTIAL_KEY"))
	if err != nil {
		log.Fatal("TARGET_CREDENTIAL_KEY harus base64 valid")
	}
	targetRepository, err := store.NewTargetRepository(database, key)
	if err != nil {
		log.Fatal(err)
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = ".local/data"
	}
	app.UseGroups(groupRepository)
	app.UseGrants(permissionRepository)
	app.UseDatabaseRegistry(targetRepository)
	app.UseTargets(api.NewRegistryTargets(targetRepository, permissionRepository), dataDir)
	app.UseMigrations(store.NewMigrationRepository(database))
	app.UseJobs(queue.NewService(queue.NewRepository(database)))
	metrics := observability.NewMetrics(database)
	app.UseMetrics(metrics)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.Handle("/", app.Router())

	address := os.Getenv("APP_ADDR")
	if address == "" {
		address = ":8080"
	}

	// Create server
	server := &http.Server{
		Addr:              address,
		Handler:           observability.HTTP(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Server listening at %s", server.Addr)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	// Jalanin server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to run: %v", err)
	}
}
