package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSessionTokenNotCurrent = errors.New("Session token bukan token aktif")
var ErrInvalidSessionForRotation = errors.New("Session tidak dapat dirotasi")

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

// Mengganti token lama dg token baru -> session sama, yg diganti kuncinya
func (repository *SessionRepository) RotateToken(ctx context.Context, sessionID int64, oldTokenHash []byte, newTokenHash []byte, now time.Time, graceUntil time.Time, nextRotation time.Time) error {
	transaction, err := repository.database.Begin(ctx)

	if err != nil {
		return fmt.Errorf("Gagal memulai transaksi rotasi: %w", err)
	}

	// Rollback if fail
	defer transaction.Rollback(ctx)

	// Jadikan token lama berlaku hanya sampai grace period selesai
	const expireOldTokenQuery = `
		UPDATE session_tokens
		SET grace_until = $1
		WHERE session_id = $2
			AND token_hash = $3
			AND grace_until IS NULL
	`

	result, err := transaction.Exec(ctx, expireOldTokenQuery, graceUntil, sessionID, oldTokenHash)

	if err != nil {
		return fmt.Errorf("Gagal memperbarui token lama: %w", err)
	}

	// Pastikan token lama masih merupakan token aktif
	if result.RowsAffected() != 1 {
		return ErrSessionTokenNotCurrent
	}

	// Simpan hash token baru sebagai token aktif
	const insertNewTokenQuery = `
		INSERT INTO session_tokens (
			session_id,
			token_hash,
			created_at,
			grace_until
		)
		VALUES ($1, $2, $3, NULL)
	`

	_, err = transaction.Exec(ctx, insertNewTokenQuery, sessionID, newTokenHash, now)

	if err != nil {
		return fmt.Errorf("Gagal menyimpan token baru: %w", err)
	}

	// Atur jadwal rotasi berikutnya tanpa mengubah absolute expiry
	const updateSessionQuery = `
		UPDATE sessions
		SET
			rotate_after = $1,
			rotation_required = FALSE
		WHERE id = $2
			AND revoked_at IS NULL
			AND absolute_expires_at > $3
	`

	result, err = transaction.Exec(ctx, updateSessionQuery, nextRotation, sessionID, now)

	if err != nil {
		return fmt.Errorf("Gagal memperbarui jadwal rotasi: %w", err)
	}

	// Pastikan session masih tersedia dan belum kedaluwarsa
	if result.RowsAffected() != 1 {
		return ErrInvalidSessionForRotation
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("Gagal menyelesaikan rotasi token: %w", err)
	}

	return nil
}