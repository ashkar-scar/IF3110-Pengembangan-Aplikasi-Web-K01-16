package targetdb

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testdb"
)

// Pastikan Quote membungkus identifier dan meng-escape kutip ganda.
func TestQuote(t *testing.T) {
	cases := map[string]string{
		"orders":       `"orders"`,
		"Order Detail": `"Order Detail"`,
		`a"b`:          `"a""b"`,
	}
	for input, want := range cases {
		if got := Quote(input); got != want {
			t.Errorf("Quote(%q) = %s, want %s", input, got, want)
		}
	}
}

// Urai DSN target test menjadi Config dan password.
func postgresConfig(t *testing.T) (Config, string) {
	t.Helper()
	dsn := testdb.Target(t)

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()

	config := Config{
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		Username: parsed.User.Username(),
		SSLMode:  "disable",
	}
	return config, password
}

// Uji koneksi PostgreSQL dan pembacaan versi awal.
func TestOpenPostgresAndVersion(t *testing.T) {
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	version, err := conn.Version(context.Background())
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if version < 0 {
		t.Fatalf("got negative version %d", version)
	}
}

// Pastikan OpenPostgres gagal cepat tanpa membocorkan password di pesan error.
func TestOpenPostgresFailsSafely(t *testing.T) {
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatalf("target database must be reachable before testing failures: %v", err)
	}
	conn.Close()

	_, err = OpenPostgres(context.Background(), config, "wrong-password")
	if err == nil {
		t.Fatal("expected authentication error for wrong password")
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Fatalf("wrong password must not be echoed in error: %v", err)
	}
	if !strings.Contains(err.Error(), "password authentication failed") {
		t.Fatalf("expected password authentication failure, got: %v", err)
	}

	config.Port = 1
	start := time.Now()
	_, err = OpenPostgres(context.Background(), config, "irrelevant")
	if err == nil {
		t.Fatal("closed port accepted")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("unreachable target took too long to fail")
	}
}
