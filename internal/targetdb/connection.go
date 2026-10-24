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

type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"sslmode"`
}

type Connection struct {
	DB     *sql.DB
	Driver string
}

func OpenPostgres(ctx context.Context, c Config, password string) (*Connection, error) {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.Username, password), Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Database}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = db.PingContext(ping); err != nil {
		db.Close()
		return nil, err
	}
	return &Connection{DB: db, Driver: "postgres"}, nil
}

func Quote(value string) string    { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func (c *Connection) Close() error { return c.DB.Close() }

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (c *Connection) ledger() string {
	return "public.schema_migrations"
}

func (c *Connection) hasLedger(ctx context.Context, q queryer) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, "SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&exists)
	return exists, err
}

func (c *Connection) Version(ctx context.Context) (int64, error) {
	exists, err := c.hasLedger(ctx, c.DB)
	if err != nil || !exists {
		return 0, err
	}
	var version int64
	err = c.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM "+c.ledger()).Scan(&version)
	return version, err
}
