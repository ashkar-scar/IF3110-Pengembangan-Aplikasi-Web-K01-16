// Package testdb provides isolated PostgreSQL databases for integration tests.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Target creates an empty database owned by the target role. The administrator
// needs CREATEDB and permission to create a database for that role.
func Target(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_TARGET_DSN")
	if dsn == "" {
		t.Skip("set TEST_TARGET_DSN to run PostgreSQL integration tests")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	adminDSN := os.Getenv("TEST_TARGET_ADMIN_DSN")
	if adminDSN == "" {
		adminDSN = os.Getenv("TEST_PLATFORM_DSN")
	}
	if adminDSN == "" {
		adminDSN = dsn
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "target_test_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted+" OWNER "+pgx.Identifier{config.User}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatalf("create isolated target database (configure TEST_TARGET_ADMIN_DSN with CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("clean up target database: %v", err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		query := u.Query()
		query.Del("dbname")
		u.RawQuery = query.Encode()
		return u.String()
	}
	return dsn + " dbname=" + name
}
