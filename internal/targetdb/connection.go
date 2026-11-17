package targetdb

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Konfigurasi koneksi ke database target (tanpa password).
type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"sslmode"`
}

// Pool koneksi ke database target PostgreSQL atau SQLite.
type Connection struct {
	DB     *sql.DB
	Driver string
}

// Buka pool koneksi PostgreSQL target lalu cek konektivitasnya.
func OpenPostgres(ctx context.Context, config Config, password string) (*Connection, error) {
	connURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.Username, password),
		Host:   net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		Path:   "/" + config.Database,
	}

	query := connURL.Query()
	query.Set("sslmode", config.SSLMode)
	query.Set("connect_timeout", "5")
	connURL.RawQuery = query.Encode()

	db, err := sql.Open("pgx", connURL.String())
	if err != nil {
		return nil, err
	}

	// Batasi pool per target supaya koneksi ke banyak database tidak cepat habis.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = db.PingContext(pingCtx)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Connection{DB: db, Driver: "postgres"}, nil
}

// Kutip identifier SQL agar aman dari injeksi.
func Quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// Tutup pool koneksi target.
func (conn *Connection) Close() error {
	return conn.DB.Close()
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Nama tabel ledger sesuai DBMS target.
func (conn *Connection) ledger() string {
	if conn.Driver == "sqlite" {
		return "main.schema_migrations"
	}
	return "public.schema_migrations"
}

// Cek apakah tabel ledger schema_migrations sudah ada.
func (conn *Connection) hasLedger(ctx context.Context, q queryer) (bool, error) {
	query := "SELECT to_regclass('public.schema_migrations') IS NOT NULL"
	if conn.Driver == "sqlite" {
		query = "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'schema_migrations')"
	}

	var exists bool
	err := q.QueryRowContext(ctx, query).Scan(&exists)
	return exists, err
}

// Ambil versi migration terakhir, atau 0 jika ledger belum ada.
func (conn *Connection) Version(ctx context.Context) (int64, error) {
	exists, err := conn.hasLedger(ctx, conn.DB)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	query := "SELECT COALESCE(MAX(version), 0) FROM " + conn.ledger()
	var version int64
	err = conn.DB.QueryRowContext(ctx, query).Scan(&version)
	return version, err
}
