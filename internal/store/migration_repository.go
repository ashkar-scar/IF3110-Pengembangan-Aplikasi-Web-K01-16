package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migration_schema.sql
var MigrationSchema string

var ErrDraftNotFound = errors.New("draft tidak ditemukan")
var ErrAttemptNotFound = errors.New("attempt tidak ditemukan")
var ErrDraftConflict = errors.New("draft berubah, sudah diterapkan, atau sedang dieksekusi")

const draftColumns = "id, target_id, description, sql_text, created_by, revision, base_version, fingerprint, created_at, updated_at"
const attemptColumns = "id, target_id, draft_id, draft_revision, description, sql_text, actor_id, actor_name, base_version, version, status, safe_error, started_at, finished_at"

type MigrationRepository struct {
	database *pgxpool.Pool
}

func NewMigrationRepository(database *pgxpool.Pool) *MigrationRepository {
	return &MigrationRepository{database: database}
}

func scanDraft(row pgx.Row) (core.Draft, error) {
	var draft core.Draft
	err := row.Scan(&draft.ID, &draft.TargetID, &draft.Description, &draft.SQL, &draft.CreatedBy, &draft.Revision, &draft.BaseVersion, &draft.Fingerprint, &draft.CreatedAt, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return draft, ErrDraftNotFound
	}

	return draft, err
}

func scanAttempt(row pgx.Row) (core.Attempt, error) {
	var attempt core.Attempt
	err := row.Scan(&attempt.ID, &attempt.TargetID, &attempt.DraftID, &attempt.Revision, &attempt.Description, &attempt.SQL, &attempt.ActorID, &attempt.ActorName, &attempt.BaseVersion, &attempt.Version, &attempt.Status, &attempt.Error, &attempt.StartedAt, &attempt.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return attempt, ErrAttemptNotFound
	}

	return attempt, err
}

func (repository *MigrationRepository) ListDrafts(ctx context.Context, targetID int64) ([]core.Draft, error) {
	rows, err := repository.database.Query(ctx, "SELECT "+draftColumns+" FROM migration_drafts WHERE target_id = $1 ORDER BY updated_at DESC, id DESC", targetID)
	if err != nil {
		return nil, fmt.Errorf("Gagal membaca draft: %w", err)
	}
	defer rows.Close()

	drafts := []core.Draft{}
	for rows.Next() {
		draft, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	return drafts, rows.Err()
}

func (repository *MigrationRepository) GetDraft(ctx context.Context, targetID, draftID int64) (core.Draft, error) {
	return scanDraft(repository.database.QueryRow(ctx, "SELECT "+draftColumns+" FROM migration_drafts WHERE target_id = $1 AND id = $2", targetID, draftID))
}

func (repository *MigrationRepository) CreateDraft(ctx context.Context, draft core.Draft) (core.Draft, error) {
	const query = `
		INSERT INTO migration_drafts (target_id, description, sql_text, created_by, base_version, fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + draftColumns

	return scanDraft(repository.database.QueryRow(ctx, query, draft.TargetID, draft.Description, draft.SQL, draft.CreatedBy, draft.BaseVersion, draft.Fingerprint))
}

// UpdateDraft hanya berhasil jika revisi masih sama dan draft belum pernah dijalankan dengan hasil final atau sedang berjalan
func (repository *MigrationRepository) UpdateDraft(ctx context.Context, draft core.Draft) (core.Draft, error) {
	const query = `
		UPDATE migration_drafts AS d
		SET description = $1, sql_text = $2, base_version = $3, fingerprint = $4, revision = revision + 1, updated_at = now()
		WHERE d.target_id = $5 AND d.id = $6 AND d.revision = $7
		  AND NOT EXISTS (
			SELECT FROM migration_attempts AS a
			WHERE a.draft_id = d.id AND a.status IN ('SUCCESS', 'RUNNING', 'UNKNOWN')
		  )
		RETURNING ` + draftColumns

	updated, err := scanDraft(repository.database.QueryRow(ctx, query, draft.Description, draft.SQL, draft.BaseVersion, draft.Fingerprint, draft.TargetID, draft.ID, draft.Revision))
	if errors.Is(err, ErrDraftNotFound) {
		return updated, ErrDraftConflict
	}
	
	return updated, err
}

func (repository *MigrationRepository) BeginAttempt(ctx context.Context, targetID, draftID int64, revision int, attemptID string, actor core.User) (attempt core.Attempt, fingerprint string, created bool, err error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return attempt, "", false, fmt.Errorf("Gagal memulai transaksi attempt: %w", err)
	}
	defer transaction.Rollback(ctx)

	draft, err := scanDraft(transaction.QueryRow(ctx, "SELECT "+draftColumns+" FROM migration_drafts WHERE target_id = $1 AND id = $2 FOR UPDATE", targetID, draftID))
	if err != nil {
		return attempt, "", false, err
	}
	if draft.Revision != revision {
		return attempt, "", false, ErrDraftConflict
	}

	const insert = `
		INSERT INTO migration_attempts (id, target_id, draft_id, draft_revision, description, sql_text, actor_id, actor_name, base_version, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'RUNNING')
		ON CONFLICT (draft_id, draft_revision) DO NOTHING`
	tag, err := transaction.Exec(ctx, insert, attemptID, draft.TargetID, draft.ID, draft.Revision, draft.Description, draft.SQL, actor.ID, actor.Username, draft.BaseVersion)
	if err != nil {
		return attempt, "", false, fmt.Errorf("Gagal mencatat attempt: %w", err)
	}

	attempt, err = scanAttempt(transaction.QueryRow(ctx, "SELECT "+attemptColumns+" FROM migration_attempts WHERE draft_id = $1 AND draft_revision = $2", draft.ID, draft.Revision))
	if err != nil {
		return attempt, "", false, err
	}
	if err = transaction.Commit(ctx); err != nil {
		return attempt, "", false, fmt.Errorf("Gagal menyimpan attempt: %w", err)
	}

	return attempt, draft.Fingerprint, tag.RowsAffected() == 1, nil
}

func (repository *MigrationRepository) FinishAttempt(ctx context.Context, attemptID string, status core.AttemptStatus, version *int64, message string) (core.Attempt, error) {
	const query = `
		UPDATE migration_attempts
		SET status = $1, version = $2, safe_error = $3, finished_at = now()
		WHERE id = $4
		RETURNING ` + attemptColumns

	return scanAttempt(repository.database.QueryRow(ctx, query, status, version, message, attemptID))
}

func (repository *MigrationRepository) ListAttempts(ctx context.Context, targetID int64) ([]core.Attempt, error) {
	rows, err := repository.database.Query(ctx, "SELECT "+attemptColumns+" FROM migration_attempts WHERE target_id = $1 ORDER BY started_at DESC LIMIT 200", targetID)
	if err != nil {
		return nil, fmt.Errorf("Gagal membaca riwayat migration: %w", err)
	}
	defer rows.Close()

	attempts := []core.Attempt{}
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		
		attempts = append(attempts, attempt)
	}

	return attempts, rows.Err()
}

func (repository *MigrationRepository) GetAttempt(ctx context.Context, targetID int64, attemptID string) (core.Attempt, error) {
	return scanAttempt(repository.database.QueryRow(ctx, "SELECT "+attemptColumns+" FROM migration_attempts WHERE target_id = $1 AND id = $2", targetID, attemptID))
}
