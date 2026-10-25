package targetdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

var ErrVersion = errors.New("versi database telah berubah; muat ulang dan tinjau kembali draft")
var ErrDrift = errors.New("schema telah berubah sejak SQL dihasilkan; bandingkan ulang schema")
var ErrUnknown = errors.New("hasil commit belum dapat dipastikan; lakukan rekonsiliasi sebelum mencoba kembali")

// TODO(askhar): samakan tipe ID dengan primary key tabel migration_attempts.
type Execution struct {
	ID          string
	SQL         string
	ActorID     int64
	ActorName   string
	BaseVersion int64
	Fingerprint string
}

const postgresLedger = `CREATE TABLE IF NOT EXISTS public.schema_migrations (
version BIGINT PRIMARY KEY, attempt_id TEXT UNIQUE NOT NULL, sql_text TEXT NOT NULL, actor_id BIGINT NOT NULL, actor_name TEXT NOT NULL, executed_at TIMESTAMPTZ NOT NULL DEFAULT now())`

const sqliteLedger = `CREATE TABLE IF NOT EXISTS main.schema_migrations (
version INTEGER PRIMARY KEY, attempt_id TEXT UNIQUE NOT NULL, sql_text TEXT NOT NULL, actor_id INTEGER NOT NULL, actor_name TEXT NOT NULL, executed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`

func (c *Connection) Execute(ctx context.Context, execution Execution) (int64, error) {
	statements, err := sqlguard.Migration(execution.SQL)
	if err != nil {
		return 0, err
	}
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = c.lock(ctx, tx); err != nil {
		return 0, err
	}
	ledgerDDL := postgresLedger
	if c.Driver == "sqlite" {
		ledgerDDL = sqliteLedger
	}
	if _, err = tx.ExecContext(ctx, ledgerDDL); err != nil {
		return 0, err
	}
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT version FROM "+c.ledger()+" WHERE attempt_id=$1", execution.ID).Scan(&version)
	if err == nil {
		return version, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM "+c.ledger()).Scan(&version); err != nil {
		return 0, err
	}
	if version != execution.BaseVersion {
		return 0, ErrVersion
	}
	if execution.Fingerprint != "" {
		snapshot, err := c.snapshot(ctx, tx)
		if err != nil {
			return 0, err
		}
		if snapshot.Fingerprint() != execution.Fingerprint {
			return 0, ErrDrift
		}
	}
	for i, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement.SQL); err != nil {
			return 0, statementError(i+1, err)
		}
	}
	version++
	_, err = tx.ExecContext(ctx, "INSERT INTO "+c.ledger()+"(version,attempt_id,sql_text,actor_id,actor_name) VALUES($1,$2,$3,$4,$5)",
		version, execution.ID, execution.SQL, execution.ActorID, execution.ActorName)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, ErrUnknown
	}
	return version, nil
}

func (c *Connection) lock(ctx context.Context, tx *sql.Tx) error {
	if c.Driver == "sqlite" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout='30s'; SET LOCAL lock_timeout='5s'"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(31103110)")
	return err
}

func statementError(index int, err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return fmt.Errorf("statement %d gagal (SQLSTATE %s); seluruh perubahan dibatalkan", index, state.SQLState())
	}
	return fmt.Errorf("statement %d gagal; seluruh perubahan dibatalkan", index)
}

func (c *Connection) Lookup(ctx context.Context, attempt string) (int64, bool, error) {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	if err = c.lock(ctx, tx); err != nil {
		return 0, false, err
	}
	exists, err := c.hasLedger(ctx, tx)
	if err != nil || !exists {
		return 0, false, err
	}
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT version FROM "+c.ledger()+" WHERE attempt_id=$1", attempt).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return version, err == nil, err
}
