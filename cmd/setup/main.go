package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := store.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Database platform tidak dapat dihubungi")
	}
	defer pool.Close()
	if err := store.SetupPlatform(ctx, pool); err != nil {
		log.Fatal("Setup schema platform gagal: ", err)
	}
	username := strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
	if username == "" {
		username = "admin"
	}
	users := store.NewUserRepository(pool)
	existing, err := users.FindByUsername(ctx, username)
	if err == nil {
		if !existing.CanManagePlatform() {
			log.Fatal("Username sudah digunakan akun non-admin atau nonaktif")
		}
		log.Print("Schema siap; akun admin sudah tersedia, password tidak diubah")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatal("Akun admin tidak dapat diperiksa")
	}
	hash, err := auth.CreatePasswordHash(os.Getenv("ADMIN_PASSWORD"))
	if err != nil {
		log.Fatal("ADMIN_PASSWORD wajib berukuran 10-72 byte")
	}
	if _, err = users.Create(ctx, core.User{Username: username, PasswordHash: hash, PlatformAdmin: true, Active: true}); err != nil {
		log.Fatal("Akun admin tidak dapat dibuat")
	}
	log.Print("Schema platform dan akun admin siap")
}
