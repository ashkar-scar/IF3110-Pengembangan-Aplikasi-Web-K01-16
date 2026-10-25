package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Platform(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DSN")
	if dsn == "" {
		t.Skip("set TEST_PLATFORM_DSN to run platform integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	nameBytes := make([]byte, 12)
	if _, err = rand.Read(nameBytes); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "backend_a_" + hex.EncodeToString(nameBytes)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		defer admin.Close()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("clean up platform fixture: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = store.SetupPlatform(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
