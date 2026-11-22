package store

import (
	"context"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// ListHistory migration_attempts, mengurutkannya dari terbaru, dan menangani pagination dengan opsi buat menyembunyikan pasangan rollback sukses.
func (repository *MigrationRepository) ListHistory(ctx context.Context, targetID int64, hideRollback bool, offset int) ([]core.Attempt, error) {
	query := `
        WITH rollback_pairs AS (
            SELECT r.id AS rollback_id, e.id AS execution_id
            FROM migration_attempts r
            JOIN migration_attempts e ON e.id=r.reverts_execution_id AND e.target_id=r.target_id
            WHERE r.target_id=$1 AND r.status='SUCCESS' AND r.action_type='ROLLBACK'
                AND e.status='SUCCESS' AND r.batch_no IS NOT DISTINCT FROM e.batch_no
        )
        SELECT ` + attemptColumns + ` FROM migration_attempts a
        WHERE a.target_id=$1 AND ($2=FALSE OR NOT EXISTS(
            SELECT 1 FROM rollback_pairs p WHERE p.execution_id=a.id OR p.rollback_id=a.id
        ))
        ORDER BY a.started_at DESC,a.id DESC LIMIT $3 OFFSET $4
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

func (repository *MigrationRepository) HistoryActions(ctx context.Context, targetID int64) (canRollback, canForward *string, err error) {
	rows, err := repository.database.Query(ctx, `SELECT a.id,a.action_type,COALESCE(a.reverts_execution_id,''),
    COALESCE(r.is_irreversible,d.is_irreversible) FROM migration_attempts a
    JOIN migration_drafts d ON d.id=a.draft_id LEFT JOIN migration_requests r ON r.id=a.request_id
    WHERE a.target_id=$1 AND a.status='SUCCESS' AND a.batch_no IS NULL
    ORDER BY a.started_at,a.id`, targetID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type execution struct {
		id           string
		irreversible bool
	}
	active := []execution{}
	undone := []execution{}
	for rows.Next() {
		var e execution
		var action core.MigrationAction
		var reverts string
		if err = rows.Scan(&e.id, &action, &reverts, &e.irreversible); err != nil {
			return nil, nil, err
		}
		switch action {
		case core.ActionUp:
			active = append(active, e)
			undone = nil
		case core.ActionRollback:
			if len(active) > 0 && active[len(active)-1].id == reverts {
				undone = append(undone, active[len(active)-1])
				active = active[:len(active)-1]
			}
		case core.ActionForward:
			if len(undone) > 0 && undone[len(undone)-1].id == reverts {
				undone = undone[:len(undone)-1]
				active = append(active, e)
			}
		case core.ActionCancelBatch:
			if len(active) > 0 && active[len(active)-1].id == reverts {
				active = active[:len(active)-1]
				undone = nil
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(active) > 0 && !active[len(active)-1].irreversible {
		id := active[len(active)-1].id
		canRollback = &id
	}
	if len(undone) > 0 {
		id := undone[len(undone)-1].id
		canForward = &id
	}
	return canRollback, canForward, nil
}
