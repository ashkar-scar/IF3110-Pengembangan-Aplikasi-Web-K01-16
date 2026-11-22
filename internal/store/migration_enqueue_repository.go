package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
)

var ErrRequestNotApproved = errors.New("migration request belum disetujui")

// GetApprovedRequest mengambil request APPROVED yang siap dikirim ke queue.
func (repository *MigrationRepository) GetApprovedRequest(ctx context.Context, targetID, requestID int64) (core.MigrationRequest, error) {
	const query = `
		SELECT id, target_id, action_type, status
		FROM migration_requests
		WHERE id = $1 AND target_id = $2
	`
	var request core.MigrationRequest
	err := repository.database.QueryRow(ctx, query, requestID, targetID).Scan(&request.ID, &request.TargetID, &request.Action, &request.Status)

	if errors.Is(err, pgx.ErrNoRows) {
		return request, ErrReviewNotFound
	}

	if err != nil {
		return request, fmt.Errorf("Gagal membaca request untuk enqueue: %w", err)
	}

	if request.Status != core.RequestApproved && request.Status != core.RequestEnqueued {
		return request, ErrRequestNotApproved
	}

	return request, nil
}

// MarkRequestEnqueued mengubah request APPROVED menjadi ENQUEUED setelah job berhasil dibuat dan masuk queue
func (repository *MigrationRepository) MarkRequestEnqueued(ctx context.Context, targetID, requestID int64) error {
	const query = `
		UPDATE migration_requests
		SET status = 'ENQUEUED', updated_at = now()
		WHERE id = $1 AND target_id = $2 AND status = 'APPROVED'
	`
	result, err := repository.database.Exec(ctx, query, requestID, targetID)
	if err != nil {
		return fmt.Errorf("Gagal memperbarui status enqueue: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrRequestNotApproved
	}

	return nil
}
