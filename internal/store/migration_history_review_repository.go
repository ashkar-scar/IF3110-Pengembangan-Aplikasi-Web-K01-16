package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/jackc/pgx/v5"
)

var (
	ErrHistoryActionNotAllowed = errors.New("execution tidak dapat diajukan untuk aksi ini")
	ErrHistoryReviewExists     = errors.New("pengajuan history masih aktif")
)

// SubmitHistoryReview membuat request dan approval untuk rollback atau forward.
func (repository *MigrationRepository) SubmitHistoryReview(ctx context.Context, targetID int64, executionID string, action core.MigrationAction, actorID int64, baseVersion int64) (core.MigrationRequest, error) {
	if action != core.ActionRollback && action != core.ActionForward {
		return core.MigrationRequest{}, ErrHistoryActionNotAllowed
	}

	if _, err := repository.GetAttempt(ctx, targetID, executionID); err != nil {
		return core.MigrationRequest{}, err
	}

	canRollback, canForward, err := repository.HistoryActions(ctx, targetID)
	if err != nil {
		return core.MigrationRequest{}, err
	}

	allowed := canRollback
	if action == core.ActionForward {
		allowed = canForward
	}

	if allowed == nil || *allowed != executionID {
		return core.MigrationRequest{}, ErrHistoryActionNotAllowed
	}

	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal memulai pengajuan history: %w", err)
	}

	defer transaction.Rollback(ctx)
	request, err := historyRequestSnapshot(ctx, transaction, targetID, executionID)
	if err != nil {
		return core.MigrationRequest{}, err
	}

	request.Action = action
	request.TargetExecutionID = &executionID
	request.RequestedBy = actorID
	request.BaseVersion = baseVersion

	var activeRequestID int64
	err = transaction.QueryRow(ctx, `
		SELECT id FROM migration_requests
		WHERE target_id = $1 AND action_type = $2 AND target_execution_id = $3
		  	AND status IN ('PENDING_REVIEW', 'APPROVED', 'ENQUEUED')
		LIMIT 1
	`, targetID, action, executionID).Scan(&activeRequestID)

	if err == nil {
		return core.MigrationRequest{}, ErrHistoryReviewExists
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return core.MigrationRequest{}, fmt.Errorf("Gagal memeriksa pengajuan aktif: %w", err)
	}

	candidates, err := historyApprovalCandidates(ctx, transaction, targetID)
	if err != nil {
		return core.MigrationRequest{}, err
	}

	const insertRequest = `
		INSERT INTO migration_requests (
			target_id, draft_id, draft_revision, action_type, target_execution_id,
			sql_snapshot_up, sql_snapshot_down, is_irreversible,
			irreversible_reason, execution_mode, batch_column, batch_size,
			base_version, fingerprint, requested_by, status
		)

		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'PENDING_REVIEW')
		RETURNING ` + requestColumns

	created, err := scanMigrationRequest(transaction.QueryRow(ctx, insertRequest,
		request.TargetID, request.DraftID, request.DraftRevision, request.Action,
		request.TargetExecutionID, request.SQLSnapshotUp, request.SQLSnapshotDown,
		request.IsIrreversible, request.IrreversibleReason, request.ExecutionMode,
		request.BatchColumn, request.BatchSize, request.BaseVersion,
		request.Fingerprint, request.RequestedBy,
	))

	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal membuat migration request: %w", err)
	}

	created.Description = request.Description
	created.Approvals = make([]core.MigrationApproval, 0, len(candidates))
	for _, candidate := range candidates {
		var approvalID int64
		err = transaction.QueryRow(ctx, `
			INSERT INTO migration_approvals (request_id, group_id, leader_id)
			VALUES ($1, $2, $3)
			RETURNING id
		`, created.ID, candidate.GroupID, *candidate.LeaderID).Scan(&approvalID)

		if err != nil {
			return core.MigrationRequest{}, fmt.Errorf("Gagal membuat approval: %w", err)
		}

		created.Approvals = append(created.Approvals, core.MigrationApproval{
			ID:         approvalID,
			RequestID:  created.ID,
			GroupID:    candidate.GroupID,
			GroupName:  candidate.GroupName,
			LeaderID:   *candidate.LeaderID,
			LeaderName: candidate.LeaderName,
			Decision:   core.ApprovalPending,
		})
	}

	if err = transaction.Commit(ctx); err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan pengajuan history: %w", err)
	}

	return created, nil
}

// historyRequestSnapshot membaca snapshot immutable milik execution terpilih / mengambil data hasil execution
func historyRequestSnapshot(ctx context.Context, transaction pgx.Tx, targetID int64, executionID string) (core.MigrationRequest, error) {
	const query = `
		SELECT a.target_id, a.draft_id, a.draft_revision, d.description,
		    COALESCE(mr.sql_snapshot_up, d.sql_up),
		    COALESCE(mr.sql_snapshot_down, d.sql_down),
		    COALESCE(mr.is_irreversible, d.is_irreversible),
		    COALESCE(mr.irreversible_reason, d.irreversible_reason),
		    COALESCE(mr.execution_mode, d.execution_mode),
		    COALESCE(mr.batch_column, d.batch_column),
		    COALESCE(mr.batch_size, d.batch_size),
		    COALESCE(mr.fingerprint, d.fingerprint)
		FROM migration_attempts AS a
		JOIN migration_drafts AS d ON d.id = a.draft_id
		LEFT JOIN migration_requests AS mr ON mr.id = a.request_id
		WHERE a.target_id = $1 AND a.id = $2
		FOR UPDATE OF a
	`
	var request core.MigrationRequest
	err := transaction.QueryRow(ctx, query, targetID, executionID).Scan(
		&request.TargetID, &request.DraftID, &request.DraftRevision, &request.Description,
		&request.SQLSnapshotUp, &request.SQLSnapshotDown, &request.IsIrreversible,
		&request.IrreversibleReason, &request.ExecutionMode, &request.BatchColumn,
		&request.BatchSize, &request.Fingerprint,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return request, ErrAttemptNotFound
	}

	return request, err
}

// historyApprovalCandidates mengambil leader aktif dari seluruh group yang harus memberikan keputusan
func historyApprovalCandidates(ctx context.Context, transaction pgx.Tx, targetID int64) ([]approvalCandidate, error) {
	rows, err := transaction.Query(ctx, `
		SELECT g.id, g.name, g.leader_id, COALESCE(u.username, ''), COALESCE(u.is_active, FALSE)
		FROM database_grants AS dg
		JOIN groups AS g ON g.id = dg.group_id
		LEFT JOIN users AS u ON u.id = g.leader_id
		WHERE dg.database_id = $1 AND dg.permission IN ('VIEW', 'EDIT')
		ORDER BY g.id
	`, targetID)

	if err != nil {
		return nil, fmt.Errorf("Gagal membaca group approval: %w", err)
	}

	defer rows.Close()
	candidates := []approvalCandidate{}
	for rows.Next() {
		var candidate approvalCandidate
		if err = rows.Scan(&candidate.GroupID, &candidate.GroupName, &candidate.LeaderID, &candidate.LeaderName, &candidate.LeaderLive); 
		
		err != nil {
			return nil, fmt.Errorf("Gagal membaca calon approver: %w", err)
		}

		candidates = append(candidates, candidate)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("Gagal membaca daftar approver: %w", err)
	}

	if len(candidates) == 0 {
		return nil, ErrNoApprovalGroup
	}

	for _, candidate := range candidates {
		if candidate.LeaderID == nil || !candidate.LeaderLive {
			return nil, ErrNoGroupLeader
		}
	}

	return candidates, nil
}