package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
)

// ListHistory migration_attempts, mengurutkannya dari terbaru, dan menangani pagination dengan opsi buat menyembunyikan pasangan rollback sukses.
func (repository *MigrationRepository) ListHistory(ctx context.Context, targetID int64, hideRollback bool, offset int) ([]core.Attempt, error) {
	query := `
		SELECT ` + attemptColumns + `
		FROM migration_attempts AS a
		WHERE a.target_id = $1
			AND ($2 = FALSE OR (
				NOT (a.status = 'SUCCESS' AND a.action_type = 'ROLLBACK')
				AND NOT EXISTS (
					SELECT 1
					FROM migration_attempts AS rollback_attempt
					WHERE rollback_attempt.target_id = a.target_id
						AND rollback_attempt.status = 'SUCCESS'
						AND rollback_attempt.action_type = 'ROLLBACK'
						AND rollback_attempt.reverts_execution_id = a.id
				)
			)
		)
			
		ORDER BY a.started_at DESC, a.id DESC
		LIMIT $3 OFFSET $4
	`
	rows, err := repository.database.Query(ctx, query, targetID, hideRollback, AttemptsPerPage+1, max(offset, 0))
	if err != nil {
		return nil, fmt.Errorf("Gagal membaca migration history: %w", err)
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

// HistoryActions mencari execution sukses terakhir yang dapat di-rollback atau di-forward.
func (repository *MigrationRepository) HistoryActions(ctx context.Context, targetID int64) (canRollback, canForward *string, err error) {
	const activeQuery = `
		SELECT a.id, COALESCE(mr.is_irreversible, d.is_irreversible)
		FROM migration_attempts AS a
		JOIN migration_drafts AS d ON d.id = a.draft_id
		LEFT JOIN migration_requests AS mr ON mr.id = a.request_id
		WHERE a.target_id = $1
			AND a.status = 'SUCCESS'
			AND a.action_type IN ('UP', 'FORWARD')
			AND NOT EXISTS (
				SELECT 1 FROM migration_attempts AS rollback_attempt
				WHERE rollback_attempt.target_id = a.target_id
				AND rollback_attempt.status = 'SUCCESS'
				AND rollback_attempt.action_type = 'ROLLBACK'
				AND rollback_attempt.reverts_execution_id = a.id
			)

		ORDER BY COALESCE(a.version, 0) DESC, a.started_at DESC, a.id DESC
		LIMIT 1
	`
	var activeID string
	var irreversible bool
	
	err = repository.database.QueryRow(ctx, activeQuery, targetID).Scan(&activeID, &irreversible)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("Gagal menentukan rollback: %w", err)
	}

	if err == nil && !irreversible {
		canRollback = &activeID
	}

	const latestSuccessQuery = `
		SELECT action_type, reverts_execution_id
		FROM migration_attempts
		WHERE target_id = $1
			AND status = 'SUCCESS'
			AND action_type IN ('UP', 'ROLLBACK', 'FORWARD')
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	`
	var action core.MigrationAction
	var revertsID *string
	
	err = repository.database.QueryRow(ctx, latestSuccessQuery, targetID).Scan(&action, &revertsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return canRollback, nil, nil
	}
	
	if err != nil {
		return nil, nil, fmt.Errorf("Gagal menentukan forward: %w", err)
	}
	
	if action == core.ActionRollback && revertsID != nil {
		canForward = revertsID
	}
	
	return canRollback, canForward, nil
}