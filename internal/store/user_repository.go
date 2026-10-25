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

// Menyimpan user baru dan mengembalikan data hasil penyimpanan
func (repository *UserRepository) Create(ctx context.Context, user core.User) (core.User, error) {
	const query = `
		INSERT INTO users (
			username,
			password_hash,
			is_platform_admin,
			is_active
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`

	err := repository.database.QueryRow(ctx, query, user.Username, user.PasswordHash, user.PlatformAdmin, user.Active).Scan(&user.ID, &user.CreatedAt)
	
	if err != nil {
		return core.User{}, fmt.Errorf("Gagal menyimpan user: %w", err)
	}

	return user, nil
}

// Mengambil semua user tanpa menyertakan password hash
func (repository *UserRepository) FindAll(ctx context.Context) ([]core.User, error) {
	const query = `
		SELECT id, username, is_platform_admin, is_active, created_at
		FROM users
		ORDER BY id
	`

	rows, err := repository.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Gagal mengambil daftar user: %w", err)
	}
	
	defer rows.Close()

	users := []core.User{}
	for rows.Next() {
		var user core.User
		if err := rows.Scan(&user.ID, &user.Username, &user.PlatformAdmin, &user.Active, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("Gagal membaca data user: %w", err)
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Gagal membaca daftar user: %w", err)
	}

	return users, nil
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

	err := repository.database.QueryRow(ctx, query, argument).Scan( &user.ID, &user.Username, &user.PasswordHash, &user.PlatformAdmin, &user.Active, &user.CreatedAt)

	if err != nil {
		return core.User{}, fmt.Errorf("Gagal mencari user: %w", err)
	}

	return user, nil
}
