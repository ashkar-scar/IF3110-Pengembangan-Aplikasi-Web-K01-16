package targetdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

var (
	ErrVersion = errors.New("versi database telah berubah; muat ulang dan tinjau kembali draft")
	ErrDrift   = errors.New("schema telah berubah sejak SQL dihasilkan; bandingkan ulang schema")
	ErrUnknown = errors.New("hasil commit belum dapat dipastikan; lakukan rekonsiliasi sebelum mencoba kembali")
)

// Payload eksekusi migration yang sudah dikunci.
type Execution struct {
	ID          string
	SQL         string
	ActorID     int64
	ActorName   string
	BaseVersion int64
	Fingerprint string
}

const postgresLedger = `
	CREATE TABLE IF NOT EXISTS public.schema_migrations (
		version BIGINT PRIMARY KEY,
		attempt_id TEXT UNIQUE NOT NULL,
		sql_text TEXT NOT NULL,
		actor_id BIGINT NOT NULL,
		actor_name TEXT NOT NULL,
		executed_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)
`

const sqliteLedger = `
	CREATE TABLE IF NOT EXISTS main.schema_migrations (
		version INTEGER PRIMARY KEY,
		attempt_id TEXT UNIQUE NOT NULL,
		sql_text TEXT NOT NULL,
		actor_id INTEGER NOT NULL,
		actor_name TEXT NOT NULL,
		executed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)
`

// Jalankan SQL migration dalam transaksi terkunci lalu catat ke ledger.
func (conn *Connection) Execute(ctx context.Context, execution Execution) (int64, error) {
	statements, err := sqlguard.Migration(execution.SQL)
	if err != nil {
		return 0, err
	}
	var postgresStatements []sqlguard.PostgresStatement
	if conn.Driver == "postgres" {
		postgresStatements, err = sqlguard.PostgresMigration(execution.SQL)
		if err != nil {
			return 0, err
		}
	}

	tx, err := conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	err = conn.lock(ctx, tx)
	if err != nil {
		return 0, err
	}

	ledgerDDL := postgresLedger
	if conn.Driver == "sqlite" {
		ledgerDDL = sqliteLedger
	}
	_, err = tx.ExecContext(ctx, ledgerDDL)
	if err != nil {
		return 0, err
	}

	// Kembalikan versi lama jika attempt ID ini sudah pernah commit (idempoten).
	lookupQuery := "SELECT version FROM " + conn.ledger() + " WHERE attempt_id = $1"
	var version int64
	err = tx.QueryRowContext(ctx, lookupQuery, execution.ID).Scan(&version)
	if err == nil {
		return version, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	maxVersionQuery := "SELECT COALESCE(MAX(version), 0) FROM " + conn.ledger()
	err = tx.QueryRowContext(ctx, maxVersionQuery).Scan(&version)
	if err != nil {
		return 0, err
	}
	if version != execution.BaseVersion {
		return 0, ErrVersion
	}

	// Cek ulang fingerprint di dalam transaksi agar drift dari luar langsung ditolak.
	if execution.Fingerprint != "" {
		snapshot, err := conn.snapshot(ctx, tx)
		if err != nil {
			return 0, err
		}
		if snapshot.Fingerprint() != execution.Fingerprint {
			return 0, ErrDrift
		}
	}

	for index, statement := range statements {
		if conn.Driver == "postgres" {
			_, err = executePostgresStatement(ctx, tx, postgresStatements[index])
		} else {
			_, err = tx.ExecContext(ctx, statement.SQL)
		}
		if err != nil {
			return 0, statementError(index+1, err)
		}
	}

	version++
	insertLedgerQuery := `
		INSERT INTO ` + conn.ledger() + ` (
			version,
			attempt_id,
			sql_text,
			actor_id,
			actor_name
		)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.ExecContext(
		ctx,
		insertLedgerQuery,
		version,
		execution.ID,
		execution.SQL,
		execution.ActorID,
		execution.ActorName,
	)
	if err != nil {
		return 0, err
	}

	// Jika Commit gagal (misal koneksi putus), status akhir harus dicek via rekonsiliasi.
	err = tx.Commit()
	if err != nil {
		return 0, ErrUnknown
	}

	return version, nil
}

// Ambil advisory lock transaksi PostgreSQL agar migrasi berjalan berurutan.
func (conn *Connection) lock(ctx context.Context, tx *sql.Tx) error {
	if conn.Driver == "sqlite" {
		return nil
	}

	_, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = '30s'; SET LOCAL lock_timeout = '5s'")
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(31103110)")
	return err
}

// Format error aman per nomor statement dengan kode SQLSTATE bila ada.
func statementError(index int, err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return fmt.Errorf("statement %d gagal (SQLSTATE %s); seluruh perubahan dibatalkan", index, state.SQLState())
	}
	return fmt.Errorf("statement %d gagal; seluruh perubahan dibatalkan", index)
}

// Cek apakah suatu attempt ID tercatat commit di ledger target.
func (conn *Connection) Lookup(ctx context.Context, attemptID string) (int64, bool, error) {
	tx, err := conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	err = conn.lock(ctx, tx)
	if err != nil {
		return 0, false, err
	}

	exists, err := conn.hasLedger(ctx, tx)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, nil
	}

	query := "SELECT version FROM " + conn.ledger() + " WHERE attempt_id = $1"
	var version int64
	err = tx.QueryRowContext(ctx, query, attemptID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return version, true, nil
}
