package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Error message
var ErrLeaderNotMember = errors.New("Leader harus merupakan anggota aktif dari group")

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

// Mengambil semua group beserta informasi leader
func (repository *GroupRepository) FindAll(ctx context.Context) ([]core.Group, error) {
	const query = `
		SELECT
			g.id,
			g.name,
			g.leader_id,
			COALESCE(u.username, ''),
			g.created_at
		FROM groups AS g
		LEFT JOIN users AS u
			ON u.id = g.leader_id
		ORDER BY g.id
	`

	rows, err := repository.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Gagal mengambil daftar group: %w", err)
	}

	defer rows.Close()

	groups := []core.Group{}

	for rows.Next() {
		var group core.Group
		err := rows.Scan(&group.ID, &group.Name, &group.LeaderID, &group.LeaderUsername, &group.CreatedAt)

		if err != nil {
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
	// Transaksi menjaga perubahan anggota dan session tetap konsisten
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	// Kunci group agar perubahan anggota tidak berjalan bersamaan
	if err = lockGroup(ctx, tx, groupID); err != nil {
		return err
	}

	const query = `
		INSERT INTO group_members (group_id, user_id)
		VALUES ($1, $2)
	`

	if _, err = tx.Exec(ctx, query, groupID, userID); err != nil {
		return fmt.Errorf("Gagal menambahkan anggota group: %w", err)
	}

	// Minta token dirotasi karena hak akses user mungkin bertambah
	const rotateSessionQuery = `
		UPDATE sessions
		SET rotation_required = TRUE
		WHERE user_id = $1
		  AND revoked_at IS NULL
	`

	if _, err = tx.Exec(ctx, rotateSessionQuery, userID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Mengambil seluruh user yang menjadi anggota sebuah group
func (repository *GroupRepository) FindMembers(ctx context.Context, groupID int64) ([]core.User, error) {
	const groupExistsQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM groups
			WHERE id = $1
		)
	`

	var exists bool
	if err := repository.database.QueryRow(ctx, groupExistsQuery, groupID).Scan(&exists); err != nil {
		return nil, err
	}

	if !exists {
		return nil, ErrNotFound
	}

	const findMembersQuery = `
		SELECT
			u.id,
			u.username,
			u.is_platform_admin,
			u.is_active,
			u.created_at
		FROM users AS u
		JOIN group_members AS m
			ON m.user_id = u.id
		WHERE m.group_id = $1
		ORDER BY u.id
	`

	rows, err := repository.database.Query(ctx, findMembersQuery, groupID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	members := []core.User{}
	for rows.Next() {
		var user core.User
		if err := rows.Scan(&user.ID, &user.Username, &user.PlatformAdmin, &user.Active, &user.CreatedAt); err != nil {
			return nil, err
		}
		
		members = append(members, user)
	}

	return members, rows.Err()
}

// Menghapus user dari group dan mencabut session aktifnya
func (repository *GroupRepository) RemoveMember(ctx context.Context, groupID, userID int64) error {
	// Transaksi mencegah membership terhapus tanpa pencabutan session
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)
	if err = lockGroup(ctx, tx, groupID); err != nil {
		return err
	}

	const removeMemberQuery = `
		DELETE FROM group_members
		WHERE group_id = $1
		  AND user_id = $2
	`

	result, err := tx.Exec(ctx, removeMemberQuery, groupID, userID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	// Jika anggota yang dihapus adalah leader, kosongkan posisi leader group
	const clearLeaderQuery = `
		UPDATE groups
		SET leader_id = NULL
		WHERE id = $1
		AND leader_id = $2
	`

	if _, err = tx.Exec(ctx, clearLeaderQuery, groupID, userID); err != nil {
		return fmt.Errorf("Gagal mengosongkan leader group: %w", err)
	}

	const revokeSessionQuery = `
		UPDATE sessions
		SET revoked_at = NOW()
		WHERE user_id = $1
		  AND revoked_at IS NULL
	`

	if _, err = tx.Exec(ctx, revokeSessionQuery, userID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// SetLeader menetapkan anggota aktif sebagai leader group.
func (repository *GroupRepository) SetLeader(ctx context.Context, groupID int64, leaderID int64) (core.Group, error) {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return core.Group{}, fmt.Errorf("Gagal memulai transaksi leader: %w", err)
	}

	defer tx.Rollback(ctx)

	// Memastikan group tersedia dan menguncinya selama perubahan
	if err := lockGroup(ctx, tx, groupID); err != nil {
		return core.Group{}, err
	}

	// Leader harus menjadi anggota group dan masih aktif
	const memberQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM group_members AS gm
			JOIN users AS u
				ON u.id = gm.user_id
			WHERE gm.group_id = $1
				AND gm.user_id = $2
			  	AND u.is_active = TRUE
		)
	`

	var validMember bool
	if err := tx.QueryRow(ctx, memberQuery, groupID, leaderID).Scan(&validMember); err != nil {
		return core.Group{}, fmt.Errorf(
			"Gagal memeriksa calon leader: %w",
			err,
		)
	}

	if !validMember {
		return core.Group{}, ErrLeaderNotMember
	}

	const updateQuery = `
		UPDATE groups
		SET leader_id = $2
		WHERE id = $1
	`

	if _, err := tx.Exec(ctx, updateQuery, groupID, leaderID); err != nil {
		return core.Group{}, fmt.Errorf("Gagal menetapkan leader: %w", err)
	}

	const findQuery = `
		SELECT
			g.id,
			g.name,
			g.leader_id,
			COALESCE(u.username, ''),
			g.created_at
		FROM groups AS g
		LEFT JOIN users AS u
			ON u.id = g.leader_id
		WHERE g.id = $1
	`

	var group core.Group
	err = tx.QueryRow(ctx, findQuery, groupID).Scan(&group.ID, &group.Name, &group.LeaderID, &group.LeaderUsername, &group.CreatedAt)
	if err != nil {
		return core.Group{}, fmt.Errorf("Gagal membaca group setelah menetapkan leader: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return core.Group{}, fmt.Errorf("Gagal menyimpan leader: %w", err)
	}

	return group, nil
}