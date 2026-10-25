package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TargetRepository struct {
	database *pgxpool.Pool
	secrets  cipher.AEAD
}

func NewTargetRepository(database *pgxpool.Pool, key []byte) (*TargetRepository, error) {
	if len(key) != 32 {
		return nil, errors.New("TARGET_CREDENTIAL_KEY harus 32 byte setelah decode base64")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	secrets, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &TargetRepository{database: database, secrets: secrets}, nil
}

func (r *TargetRepository) encrypt(password string) ([]byte, error) {
	nonce := make([]byte, r.secrets.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return r.secrets.Seal(nonce, nonce, []byte(password), nil), nil
}

func (r *TargetRepository) decrypt(value []byte) (string, error) {
	n := r.secrets.NonceSize()
	if len(value) < n {
		return "", errors.New("kredensial target tidak valid")
	}
	plain, err := r.secrets.Open(nil, value[:n], value[n:], nil)
	if err != nil {
		return "", errors.New("kredensial target tidak dapat dibuka")
	}
	return string(plain), nil
}

const targetColumns = "id,name,driver,host,port,database_name,username,sslmode,is_active"

func scanTarget(row pgx.Row) (core.DatabaseTarget, error) {
	var target core.DatabaseTarget
	err := row.Scan(&target.ID, &target.Name, &target.Driver, &target.Host, &target.Port, &target.Database, &target.Username, &target.SSLMode, &target.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, ErrNotFound
	}
	return target, err
}

func (r *TargetRepository) FindAll(ctx context.Context) ([]core.DatabaseTarget, error) {
	rows, err := r.database.Query(ctx, "SELECT "+targetColumns+" FROM database_targets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []core.DatabaseTarget{}
	for rows.Next() {
		target, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (r *TargetRepository) FindByID(ctx context.Context, id int64) (core.DatabaseTarget, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return core.DatabaseTarget{}, err
	}
	defer tx.Rollback(ctx)
	target, err := scanTarget(tx.QueryRow(ctx, "SELECT "+targetColumns+" FROM database_targets WHERE id=$1 FOR SHARE", id))
	if err != nil {
		return target, err
	}
	if !target.Active {
		return target, nil
	}
	var encrypted []byte
	if err = tx.QueryRow(ctx, "SELECT password_ciphertext FROM database_targets WHERE id=$1", id).Scan(&encrypted); err != nil {
		return target, err
	}
	target.Password, err = r.decrypt(encrypted)
	return target, err
}

func (r *TargetRepository) Create(ctx context.Context, target core.DatabaseTarget) (core.DatabaseTarget, error) {
	if target.Driver == "sqlite" {
		file := make([]byte, 16)
		if _, err := rand.Read(file); err != nil {
			return core.DatabaseTarget{}, err
		}
		target.Database = hex.EncodeToString(file) + ".db"
		target.Host = ""
		target.Port = 1
		target.Username = ""
		target.SSLMode = "disable"
		target.Password = ""
	}
	encrypted, err := r.encrypt(target.Password)
	if err != nil {
		return core.DatabaseTarget{}, err
	}
	return scanTarget(r.database.QueryRow(ctx, `INSERT INTO database_targets
(name,driver,host,port,database_name,username,sslmode,is_active,password_ciphertext)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+targetColumns,
		target.Name, target.Driver, target.Host, target.Port, target.Database, target.Username, target.SSLMode, target.Active, encrypted))
}

func (r *TargetRepository) Update(ctx context.Context, target core.DatabaseTarget) (core.DatabaseTarget, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return core.DatabaseTarget{}, err
	}
	defer tx.Rollback(ctx)
	var encrypted []byte
	var driver, database string
	if err = tx.QueryRow(ctx, "SELECT password_ciphertext,driver,database_name FROM database_targets WHERE id=$1 FOR UPDATE", target.ID).Scan(&encrypted, &driver, &database); errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseTarget{}, ErrNotFound
	} else if err != nil {
		return core.DatabaseTarget{}, err
	}
	if driver != target.Driver {
		return core.DatabaseTarget{}, errors.New("driver koneksi tidak dapat diubah")
	}
	if driver == "sqlite" {
		target.Database = database
		target.Host = ""
		target.Port = 1
		target.Username = ""
		target.SSLMode = "disable"
		target.Password = ""
	}
	if target.Password != "" {
		if encrypted, err = r.encrypt(target.Password); err != nil {
			return core.DatabaseTarget{}, err
		}
	}
	target, err = scanTarget(tx.QueryRow(ctx, `UPDATE database_targets SET name=$2,driver=$3,host=$4,port=$5,database_name=$6,username=$7,sslmode=$8,is_active=$9,password_ciphertext=$10 WHERE id=$1 RETURNING `+targetColumns,
		target.ID, target.Name, target.Driver, target.Host, target.Port, target.Database, target.Username, target.SSLMode, target.Active, encrypted))
	if err != nil {
		return core.DatabaseTarget{}, err
	}
	if !target.Active {
		if err = revokeTargetSessions(ctx, tx, target.ID); err != nil {
			return core.DatabaseTarget{}, err
		}
	}
	return target, tx.Commit(ctx)
}

func revokeTargetSessions(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=NOW() WHERE revoked_at IS NULL AND user_id IN
(SELECT m.user_id FROM group_members m JOIN database_grants g ON g.group_id=m.group_id WHERE g.database_id=$1)`, id)
	return err
}

func (r *TargetRepository) Disable(ctx context.Context, id int64) error {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, "UPDATE database_targets SET is_active=FALSE WHERE id=$1", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err = revokeTargetSessions(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ValidateTarget(target core.DatabaseTarget) error {
	if target.Driver == "sqlite" && target.Name != "" && len(target.Name) <= 100 {
		return nil
	}
	if target.Name == "" || len(target.Name) > 100 || target.Driver != "postgres" || target.Host == "" || target.Database == "" || target.Username == "" || target.Port < 1 || target.Port > 65535 {
		return fmt.Errorf("isi nama, driver postgres, host, port 1-65535, database, dan username")
	}
	switch target.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
		return nil
	}
	return fmt.Errorf("sslmode harus disable, require, verify-ca, atau verify-full")
}
