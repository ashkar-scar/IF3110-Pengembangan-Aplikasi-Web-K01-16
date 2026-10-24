package store

import (
	"context"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Menangani pembacaan data user dari database
type UserRepository struct {
	database *pgxpool.Pool
}

// Membuat repository dengan koneksi database yang tersedia
func NewUserRepository(database *pgxpool.Pool) *UserRepository {
	return &UserRepository{database: database}
}

// Mencari user berdasarkan username untuk proses login
func (repository *UserRepository) FindByUsername(ctx context.Context, username string) (core.User, error) {
	const query = `
		SELECT id, username, password_hash, is_platform_admin, is_active, created_at
		FROM users
		WHERE username = $1
	`
	return repository.findOne(ctx, query, username)
}

// Mencari user berdasarkan ID untuk pemeriksaan session
func (repository *UserRepository) FindByID(ctx context.Context, userID int64) (core.User, error) {
	const query = `
		SELECT id, username, password_hash, is_platform_admin, is_active, created_at
		FROM users
		WHERE id = $1
	`

	return repository.findOne(ctx, query, userID)
}

// Menjalankan query dan memetakan satu baris menjadi User
func (repository *UserRepository) findOne(ctx context.Context, query string, argument any) (core.User, error) {
	var user core.User

	err := repository.database.QueryRow(ctx, query, argument).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.PlatformAdmin,
		&user.Active,
		&user.CreatedAt,
	)

	if err != nil {
		return core.User{}, fmt.Errorf("Gagal mencari user: %w", err)
	}

	return user, nil
}
