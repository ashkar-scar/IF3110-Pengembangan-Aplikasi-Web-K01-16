package store

import (
	"context"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Penyimpanan data group di database platform
type GroupRepository struct {
	database *pgxpool.Pool
}

// Membuat repository group dengan koneksi yang tersedia
func NewGroupRepository(database *pgxpool.Pool) *GroupRepository {
	return &GroupRepository{database: database}
}

// Menyimpan group baru dan mengembalikan data hasil penyimpanan
func (repository *GroupRepository) Create(ctx context.Context, group core.Group) (core.Group, error) {
	const query = `
		INSERT INTO groups (name)
		VALUES ($1)
		RETURNING id, created_at
	`

	err := repository.database.QueryRow(ctx, query, group.Name).Scan(&group.ID, &group.CreatedAt)

	if err != nil {
		return core.Group{}, fmt.Errorf("Gagal menyimpan group: %w", err)
	}

	return group, nil
}

// Mengambil semua group dari database platform
func (repository *GroupRepository) FindAll(ctx context.Context) ([]core.Group, error) {
	const query = `
		SELECT id, name, created_at
		FROM groups
		ORDER BY id
	`

	rows, err := repository.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Gagal mengambil daftar group: %w", err)
	}
	
	defer rows.Close()

	groups := []core.Group{}
	for rows.Next() {
		var group core.Group

		if err := rows.Scan(&group.ID, &group.Name, &group.CreatedAt); err != nil {
			return nil, fmt.Errorf("Gagal membaca data group: %w", err)
		}

		groups = append(groups, group)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Gagal membaca daftar group: %w", err)
	}

	return groups, nil
}

// Menambahkan user sebagai anggota group
func (repository *GroupRepository) AddMember(ctx context.Context, groupID, userID int64) error {
	const query = `
		INSERT INTO group_members (group_id, user_id)
		VALUES ($1, $2)
	`

	if _, err := repository.database.Exec(ctx, query, groupID, userID); err != nil {
		return fmt.Errorf("Gagal menambahkan anggota group: %w", err)
	}

	return nil
}