package store

import (
	"context"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// Menangani penyimpanan session di database
type SessionRepository struct {
	database *pgxpool.Pool
}

// Membuat repository session
func NewSessionRepository(database *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{database: database}
}

// Create menyimpan session dan hash token secara bersamaan
func (repository *SessionRepository) Create(ctx context.Context, session core.Session, tokenHash []byte) (core.Session, error) {
	transaction, err := repository.database.Begin(ctx)

	if err != nil {
		return core.Session{}, fmt.Errorf("Gagal memulai transaksi session: %w", err)
	}

	// Rollback if gagal
	defer transaction.Rollback(ctx)
	const sessionQuery = `
		INSERT INTO sessions (
			user_id,
			csrf_token,
			created_at,
			last_seen_at,
			absolute_expires_at,
			rotate_after,
			rotation_required
		)

		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err = transaction.QueryRow(
		ctx,
		sessionQuery,
		session.UserID,
		session.CSRFToken,
		session.CreatedAt,
		session.LastSeenAt,
		session.AbsoluteExpiresAt,
		session.RotateAfter,
		session.RotationRequired,
	).Scan(&session.ID)

	// Fail simapn session
	if err != nil {
		return core.Session{}, fmt.Errorf("Gagal menyimpan session: %w", err)
	}

	const tokenQuery = `
		INSERT INTO session_tokens (
			session_id,
			token_hash,
			created_at
		)

		VALUES ($1, $2, $3)
	`

	_, err = transaction.Exec(
		ctx,
		tokenQuery,
		session.ID,
		tokenHash,
		session.CreatedAt,
	)

	if err != nil {
		return core.Session{}, fmt.Errorf("Gagal menyimpan token session: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return core.Session{}, fmt.Errorf("Gagal menyelesaikan transaksi session: %w", err)
	}

	return session, nil
}

// Mencari session dari hash token
func (repository *SessionRepository) FindByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (core.Session, error) {
	const query = `
		SELECT
			s.id,
			s.user_id,
			s.csrf_token,
			s.created_at,
			s.last_seen_at,
			s.absolute_expires_at,
			s.rotate_after,
			s.revoked_at,
			s.rotation_required
		FROM sessions AS s
		JOIN session_tokens AS token
			ON token.session_id = s.id
		WHERE token.token_hash = $1
			AND (
				token.grace_until IS NULL
				OR token.grace_until > $2
			)
	`

	var session core.Session
	err := repository.database.QueryRow(ctx, query, tokenHash, now).Scan(
		&session.ID,
		&session.UserID,
		&session.CSRFToken,
		&session.CreatedAt,
		&session.LastSeenAt,
		&session.AbsoluteExpiresAt,
		&session.RotateAfter,
		&session.RevokedAt,
		&session.RotationRequired,
	)

	if err != nil {
		return core.Session{}, fmt.Errorf("Gagal mencari session: %w", err)
	}

	return session, nil
}

// Memperbarui waktu terakhir session digunakan
func (repository *SessionRepository) UpdateLastSeen(ctx context.Context, sessionID int64, now time.Time) error {
	const query = `
		UPDATE sessions
		SET last_seen_at = $1
		WHERE id = $2
	`

	if _, err := repository.database.Exec(ctx, query, now, sessionID); err != nil {
		return fmt.Errorf("Gagal memperbarui session: %w", err)
	}

	return nil
}

// Revoke session agar tidak dapat digunakan lagi
func (repository *SessionRepository) Revoke(ctx context.Context, sessionID int64, now time.Time) error {
	const query = `
		UPDATE sessions
		SET revoked_at = $1
		WHERE id = $2
			AND revoked_at IS NULL
	`

	if _, err := repository.database.Exec(ctx, query, now, sessionID); err != nil {
		return fmt.Errorf("Gagal mencabut session: %w", err)
	}

	return nil
}
