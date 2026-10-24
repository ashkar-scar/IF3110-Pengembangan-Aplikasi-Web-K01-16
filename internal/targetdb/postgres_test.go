package targetdb

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQuote(t *testing.T) {
	for input, want := range map[string]string{"orders": `"orders"`, "Order Detail": `"Order Detail"`, `a"b`: `"a""b"`} {
		if got := Quote(input); got != want {
			t.Errorf("Quote(%q) = %s, want %s", input, got, want)
		}
	}
}

func postgresConfig(t *testing.T) (Config, string) {
	t.Helper()
	dsn := os.Getenv("TEST_TARGET_DSN")
	if dsn == "" {
		t.Skip("set TEST_TARGET_DSN to run PostgreSQL integration tests")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	return Config{Host: u.Hostname(), Port: port, Database: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(), SSLMode: "disable"}, password
}

func TestOpenPostgresAndVersion(t *testing.T) {
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	version, err := conn.Version(context.Background())
	if err != nil || version < 0 {
		t.Fatalf("version %d, error %v", version, err)
	}
}

func TestOpenPostgresFailsSafely(t *testing.T) {
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatalf("target database must be reachable before testing failures: %v", err)
	}
	conn.Close()
	_, err = OpenPostgres(context.Background(), config, "wrong-password")
	if err == nil || strings.Contains(err.Error(), "wrong-password") {
		t.Fatalf("wrong password must fail without echoing it: %v", err)
	}
	if !strings.Contains(err.Error(), "password authentication failed") {
		t.Fatalf("expected an authentication failure, got: %v", err)
	}
	config.Port = 1
	start := time.Now()
	if _, err = OpenPostgres(context.Background(), config, "irrelevant"); err == nil {
		t.Fatal("closed port accepted")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("unreachable target took too long to fail")
	}
}
