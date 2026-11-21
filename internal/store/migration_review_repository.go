package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
)

var (
	ErrDraftImmutable  = errors.New("draft sudah diajukan atau dieksekusi")
	ErrDraftInvalid    = errors.New("draft belum memenuhi syarat untuk diajukan")
	ErrNoGroupLeader   = errors.New("semua group berizin wajib memiliki leader aktif")
	ErrNoApprovalGroup = errors.New("database belum memiliki group berizin")
)

const requestColumns = `
	id, target_id, draft_id, draft_revision, action_type, target_execution_id,
	sql_snapshot_up, sql_snapshot_down, is_irreversible, irreversible_reason,
	execution_mode, batch_column, batch_size, base_version, fingerprint,
	requested_by, status, created_at, updated_at
`

type approvalCandidate struct {
	GroupID    int64
	GroupName  string
	LeaderID   *int64
	LeaderName string
	LeaderLive bool
}

// scanMigrationRequest memetakan satu row database menjadi migration request.
func scanMigrationRequest(row pgx.Row) (core.MigrationRequest, error) {
	var request core.MigrationRequest
	err := row.Scan(
		&request.ID,
		&request.TargetID,
		&request.DraftID,
		&request.DraftRevision,
		&request.Action,
		&request.TargetExecutionID,
		&request.SQLSnapshotUp,
		&request.SQLSnapshotDown,
		&request.IsIrreversible,
		&request.IrreversibleReason,
		&request.ExecutionMode,
		&request.BatchColumn,
		&request.BatchSize,
		&request.BaseVersion,
		&request.Fingerprint,
		&request.RequestedBy,
		&request.Status,
		&request.CreatedAt,
		&request.UpdatedAt,
	)

	return request, err
}

// validateDraftForReview memastikan snapshot draft aman dan lengkap sebelum dikunci / di review
func validateDraftForReview(draft core.Draft) error {
	if strings.TrimSpace(draft.SQLUp) == "" {
		return ErrDraftInvalid
	}

	if draft.IsIrreversible {
		if strings.TrimSpace(draft.IrreversibleReason) == "" {
			return ErrDraftInvalid
		}

	} else if strings.TrimSpace(draft.SQLDown) == "" {
		return ErrDraftInvalid
	}

	if draft.ExecutionMode != core.ExecutionNormal && draft.ExecutionMode != core.ExecutionBatch {
		return ErrDraftInvalid
	}

	if draft.ExecutionMode == core.ExecutionBatch && strings.TrimSpace(draft.BatchColumn) == "" {
		return ErrDraftInvalid
	}

	if draft.BatchSize < 1 || draft.BatchSize > 100000 {
		return ErrDraftInvalid
	}

	return nil
}

// SubmitReview membuat request, approval tiap group, dan mengunci draft dalam satu transaksi.
func (repository *MigrationRepository) SubmitReview(ctx context.Context, targetID, draftID, actorID int64) (core.MigrationRequest, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal memulai submit review: %w", err)
	}

	defer transaction.Rollback(ctx)
	lockDraftQuery := `
		SELECT ` + draftColumns + `
		FROM migration_drafts
		WHERE target_id = $1 AND id = $2
		FOR UPDATE
	`
	draft, err := scanDraft(transaction.QueryRow(ctx, lockDraftQuery, targetID, draftID))
	if err != nil {
		return core.MigrationRequest{}, err
	}

	if draft.IsLocked {
		return core.MigrationRequest{}, ErrDraftImmutable
	}

	if err = validateDraftForReview(draft); err != nil {
		return core.MigrationRequest{}, err
	}

	const groupsQuery = `
		SELECT
			g.id,
			g.name,
			g.leader_id,
			COALESCE(u.username, ''),
			COALESCE(u.is_active, FALSE)
		FROM database_grants AS dg
		JOIN groups AS g ON g.id = dg.group_id
		LEFT JOIN users AS u ON u.id = g.leader_id
		WHERE dg.database_id = $1
		  	AND dg.permission IN ('VIEW', 'EDIT')
		ORDER BY g.id
	`
	rows, err := transaction.Query(ctx, groupsQuery, targetID)
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal membaca group approval: %w", err)
	}

	candidates := []approvalCandidate{}
	for rows.Next() {
		var candidate approvalCandidate
		if err = rows.Scan(&candidate.GroupID, &candidate.GroupName, &candidate.LeaderID, &candidate.LeaderName, &candidate.LeaderLive);

		err != nil {
			rows.Close()
			return core.MigrationRequest{}, fmt.Errorf("Gagal membaca calon approver: %w", err)
		}
		candidates = append(candidates, candidate)
	}

	if err = rows.Err(); err != nil {
		rows.Close()
		return core.MigrationRequest{}, fmt.Errorf("Gagal membaca daftar approver: %w", err)
	}
	
	rows.Close()
	if len(candidates) == 0 {
		return core.MigrationRequest{}, ErrNoApprovalGroup
	}

	for _, candidate := range candidates {
		if candidate.LeaderID == nil || !candidate.LeaderLive {
			return core.MigrationRequest{}, ErrNoGroupLeader
		}
	}

	insertRequestQuery := `
		INSERT INTO migration_requests (
			target_id, draft_id, draft_revision, action_type,
			sql_snapshot_up, sql_snapshot_down, is_irreversible,
			irreversible_reason, execution_mode, batch_column, batch_size,
			base_version, fingerprint, requested_by, status
		)

		VALUES ($1, $2, $3, 'UP', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'PENDING_REVIEW')
		RETURNING ` + requestColumns

	request, err := scanMigrationRequest(transaction.QueryRow(
		ctx,
		insertRequestQuery,
		draft.TargetID,
		draft.ID,
		draft.Revision,
		draft.SQLUp,
		draft.SQLDown,
		draft.IsIrreversible,
		draft.IrreversibleReason,
		draft.ExecutionMode,
		draft.BatchColumn,
		draft.BatchSize,
		draft.BaseVersion,
		draft.Fingerprint,
		actorID,
	))

	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal membuat migration request: %w", err)
	}

	const insertApprovalQuery = `
		INSERT INTO migration_approvals (request_id, group_id, leader_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	request.Approvals = make([]core.MigrationApproval, 0, len(candidates))
	for _, candidate := range candidates {
		var approvalID int64
		if err = transaction.QueryRow(ctx, insertApprovalQuery, request.ID, candidate.GroupID, *candidate.LeaderID).Scan(&approvalID); 
		err != nil {
			return core.MigrationRequest{}, fmt.Errorf("Gagal membuat approval: %w", err)
		}
		
		request.Approvals = append(request.Approvals, core.MigrationApproval{
			ID:         approvalID,
			RequestID:  request.ID,
			GroupID:    candidate.GroupID,
			GroupName:  candidate.GroupName,
			LeaderID:   *candidate.LeaderID,
			LeaderName: candidate.LeaderName,
			Decision:   core.ApprovalPending,
		})
	}

	if _, err = transaction.Exec(ctx, `
		UPDATE migration_drafts
		SET is_locked = TRUE, updated_at = now()
		WHERE id = $1
	`, draft.ID); 
	
	err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal mengunci draft: %w", err)
	}
	
	if err = transaction.Commit(ctx); err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan submit review: %w", err)
	}
	
	return request, nil
}