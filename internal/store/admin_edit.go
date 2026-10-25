package store

import (
	"context"
	"errors"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
)

// Memperbarui akun user dan menyesuaikan session aktifnya
func (repository *UserRepository) Update(ctx context.Context, user core.User) (core.User, error) {
	// Transaksi menjaga perubahan user dan session tetap konsisten
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return core.User{}, err
	}

	defer tx.Rollback(ctx)

	const findUserQuery = `
		SELECT
			username,
			password_hash,
			is_platform_admin,
			is_active
		FROM users
		WHERE id = $1
		FOR UPDATE
	`

	// Kunci user agar perubahan akun tidak berjalan bersamaan
	var previousUser core.User
	err = tx.QueryRow(ctx, findUserQuery, user.ID).Scan(&previousUser.Username, &previousUser.PasswordHash, &previousUser.PlatformAdmin, &previousUser.Active)

	if errors.Is(err, pgx.ErrNoRows) {
		return core.User{}, ErrNotFound
	}

	if err != nil {
		return core.User{}, err
	}

	// Password kosong berarti password lama tetap digunakan
	passwordChanged := user.PasswordHash != ""
	if !passwordChanged {
		user.PasswordHash = previousUser.PasswordHash
	}

	const updateUserQuery = `
		UPDATE users
		SET
			username = $2,
			password_hash = $3,
			is_platform_admin = $4,
			is_active = $5
		WHERE id = $1
		RETURNING created_at
	`

	err = tx.QueryRow(ctx, updateUserQuery, user.ID, user.Username, user.PasswordHash, user.PlatformAdmin, user.Active,).Scan(&user.CreatedAt)
	
	if err != nil {
		return core.User{}, err
	}

	const revokeSessionsQuery = `
		UPDATE sessions
		SET revoked_at = NOW()
		WHERE user_id = $1
		  AND revoked_at IS NULL
	`

	const rotateSessionsQuery = `
		UPDATE sessions
		SET rotation_required = TRUE
		WHERE user_id = $1
		  AND revoked_at IS NULL
	`

	// Cabut session ketika perubahan membuat kredensial atau akses lama tidak aman
	adminAccessRemoved := previousUser.PlatformAdmin && !user.PlatformAdmin
	if !user.Active || passwordChanged || adminAccessRemoved {
		_, err = tx.Exec(ctx, revokeSessionsQuery, user.ID)
	} else if !previousUser.PlatformAdmin && user.PlatformAdmin {
		// Peningkatan menjadi admin meminta token baru pada request berikutnya
		_, err = tx.Exec(ctx, rotateSessionsQuery, user.ID)
	}

	if err != nil {
		return core.User{}, err
	}

	return user, tx.Commit(ctx)
}

// Mengubah nama group yang sudah tersedia
func (repository *GroupRepository) Rename(ctx context.Context, groupID int64, name string) error {
	const query = `
		UPDATE groups
		SET name = $2
		WHERE id = $1
	`

	result, err := repository.database.Exec(ctx, query, groupID, name)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Menghapus group dan mencabut session seluruh anggotanya
func (repository *GroupRepository) Delete(ctx context.Context, groupID int64) error {
	// Transaksi mencegah group terhapus tanpa pencabutan session
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	if err = lockGroup(ctx, tx, groupID); err != nil {
		return err
	}

	if err = changeGroupSessions(ctx, tx, groupID, true); err != nil {
		return err
	}

	const deleteGroupQuery = `
		DELETE FROM groups
		WHERE id = $1
	`

	if _, err = tx.Exec(ctx, deleteGroupQuery, groupID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}