package store

import (
	"context"
	_ "embed"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed platform_schema.sql
var PlatformSchema string

// Menyiapkan seluruh tabel platform dan migration dalam satu transaksi
func SetupPlatform(ctx context.Context, pool *pgxpool.Pool) error {
	// Transaksi mencegah schema terpasang sebagian ketika terjadi kegagalan
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}

	// Rollback dijalankan otomatis jika proses belum berhasil di-commit
	defer tx.Rollback(ctx)

	// Buat tabel user, session, group, grant, dan registry database
	if _, err = tx.Exec(ctx, PlatformSchema); err != nil {
		return err
	}

	// Buat tabel draft dan riwayat percobaan migration
	if _, err = tx.Exec(ctx, MigrationSchema); err != nil {
		return err
	}

	// Simpan seluruh perubahan schema setelah semua query berhasil
	return tx.Commit(ctx)
}
