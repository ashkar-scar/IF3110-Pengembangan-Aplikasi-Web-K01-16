package store

import (
	"context"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Menangani grant database dan permission user
type PermissionRepository struct {
	database *pgxpool.Pool
}

// Membuat repository dengan koneksi database platform
func NewPermissionRepository(database *pgxpool.Pool) *PermissionRepository {
	return &PermissionRepository{database: database}
}

// Menyimpan atau memperbarui permission group terhadap database
func (repository *PermissionRepository) SetGrant(ctx context.Context, grant core.DatabaseGrant) error {
	const query = `
		INSERT INTO database_grants (group_id, database_id, permission)
		VALUES ($1, $2, $3)
		ON CONFLICT (group_id, database_id)
		DO UPDATE SET permission = EXCLUDED.permission
	`

	if _, err := repository.database.Exec(ctx, query, grant.GroupID, grant.DatabaseID, grant.Permission); err != nil {
		return fmt.Errorf("Gagal menyimpan database grant: %w", err)
	}

	return nil
}

// Mngambil seluruh database grant untuk pengelolaan admin
func (repository *PermissionRepository) FindAll(ctx context.Context) ([]core.DatabaseGrant, error) {
	const query = `
		SELECT group_id, database_id, permission
		FROM database_grants
		ORDER BY group_id, database_id
	`

	rows, err := repository.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Gagal mengambil daftar database grant: %w", err)
	}

	defer rows.Close()

	grants := []core.DatabaseGrant{}
	for rows.Next() {
		var grant core.DatabaseGrant
		if err := rows.Scan(&grant.GroupID, &grant.DatabaseID, &grant.Permission); err != nil {
			return nil, fmt.Errorf("Gagal membaca database grant: %w", err)
		}
		grants = append(grants, grant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Gagal membaca daftar database grant: %w", err)
	}

	return grants, nil
}

// Menghitung izin tertinggi user untuk satu database
func (repository *PermissionRepository) EffectivePermission(ctx context.Context, userID, databaseID int64) (core.Permission, error) {
	const query = `
		SELECT database_grant.permission
		FROM group_members AS member
		JOIN database_grants AS database_grant
			ON database_grant.group_id = member.group_id
		WHERE member.user_id = $1
			AND database_grant.database_id = $2
	`

	rows, err := repository.database.Query(ctx, query, userID, databaseID)
	if err != nil {
		return core.PermissionNone, fmt.Errorf("Gagal mengambil permission user: %w", err)
	}

	defer rows.Close()

	permission := core.PermissionNone
	for rows.Next() {
		var candidate core.Permission
		if err := rows.Scan(&candidate); err != nil {
			return core.PermissionNone, fmt.Errorf("Gagal membaca permission user: %w", err)
		}

		permission = core.HighestPermission(permission, candidate)
	}

	if err := rows.Err(); err != nil {
		return core.PermissionNone, fmt.Errorf("Gagal membaca permission user: %w", err)
	}

	return permission, nil
}