package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

// Open and check connection pool PostgreSQL
func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	// Pastikan alamat database tersedia
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL tidak boleh kosong")
	}

	// Ubah URL menjadi konfigurasi pgxpool
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("Konfigurasi PostgreSQL tidak valid: %w", err)
	}

	// Buat pool koneksi berdasarkan konfigurasi
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("Gagal membuat PostgreSQL pool: %w", err)
	}

	// Periksa apakah PostgreSQL dapat connect
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("PostgreSQL tidak dapat dihubungi: %w", err)
	}

	// Return pool nya
	return pool, nil
}
