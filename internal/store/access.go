package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// Error not found
var ErrNotFound = errors.New("Resource tidak ditemukan")

// Mengunci satu group selama transaksi perubahan akses berlangsung
func lockGroup(ctx context.Context, tx pgx.Tx, groupID int64) error {
	const query = `
		SELECT id
		FROM groups
		WHERE id = $1
		FOR UPDATE
	`

	var lockedGroupID int64
	err := tx.QueryRow(ctx, query, groupID).Scan(&lockedGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	return err
}

// Memperbarui session seluruh anggota ketika akses group berubah
func changeGroupSessions(ctx context.Context, tx pgx.Tx, groupID int64, revokeSessions bool) error {
	if revokeSessions {
		const revokeQuery = `
			UPDATE sessions
			SET revoked_at = NOW()
			WHERE revoked_at IS NULL
			  	AND user_id IN (
					SELECT user_id
					FROM group_members
					WHERE group_id = $1
			  	)
		`

		_, err := tx.Exec(ctx, revokeQuery, groupID)
		return err
	}

	// Rotation digunakan ketika user memperoleh akses yang lebih tinggi
	const rotateQuery = `
		UPDATE sessions
		SET rotation_required = TRUE
		WHERE revoked_at IS NULL
			AND user_id IN (
				SELECT user_id
				FROM group_members
				WHERE group_id = $1
		  	)
	`

	_, err := tx.Exec(ctx, rotateQuery, groupID)
	return err
}