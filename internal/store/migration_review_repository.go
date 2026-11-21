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
	ErrReviewNotFound  = errors.New("migration review tidak ditemukan")
	ErrReviewFinal     = errors.New("migration review sudah memiliki keputusan final")
	ErrNotGroupLeader  = errors.New("user bukan leader yang dapat memutuskan review")
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

// ListReviews mengambil semua request beserta approval setiap group.
func (repository *MigrationRepository) ListReviews(ctx context.Context, targetID, currentUserID int64) ([]core.MigrationRequest, error) {
	const reviewsQuery = `
		SELECT
			mr.id, mr.target_id, mr.draft_id, mr.draft_revision, d.description,
			mr.action_type, mr.target_execution_id, mr.sql_snapshot_up,
			mr.sql_snapshot_down, mr.is_irreversible, mr.irreversible_reason,
			mr.execution_mode, mr.batch_column, mr.batch_size, mr.base_version,
			mr.fingerprint, mr.requested_by, u.username, mr.status,
			mr.created_at, mr.updated_at
		FROM migration_requests AS mr
		JOIN migration_drafts AS d ON d.id = mr.draft_id
		JOIN users AS u ON u.id = mr.requested_by
		WHERE mr.target_id = $1 AND mr.status IN ('PENDING_REVIEW', 'APPROVED', 'REJECTED')
		ORDER BY mr.created_at DESC, mr.id DESC
	`
	rows, err := repository.database.Query(ctx, reviewsQuery, targetID)
	if err != nil {
		return nil, fmt.Errorf("Gagal membaca review: %w", err)
	}

	reviews := []core.MigrationRequest{}
	requestIndexes := map[int64]int{}
	for rows.Next() {
		var request core.MigrationRequest
		if err = rows.Scan(
			&request.ID, &request.TargetID, &request.DraftID, &request.DraftRevision,
			&request.Description, &request.Action, &request.TargetExecutionID,
			&request.SQLSnapshotUp, &request.SQLSnapshotDown, &request.IsIrreversible,
			&request.IrreversibleReason, &request.ExecutionMode, &request.BatchColumn,
			&request.BatchSize, &request.BaseVersion, &request.Fingerprint,
			&request.RequestedBy, &request.RequestedByName, &request.Status,
			&request.CreatedAt, &request.UpdatedAt,
		); 
		
		err != nil {
			rows.Close()
			return nil, fmt.Errorf("Gagal membaca data review: %w", err)
		}

		request.Approvals = []core.MigrationApproval{}
		requestIndexes[request.ID] = len(reviews)
		reviews = append(reviews, request)
	}

	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("Gagal membaca daftar review: %w", err)
	}

	rows.Close()
	if len(reviews) == 0 {
		return reviews, nil
	}

	const approvalsQuery = `
		SELECT
			ma.id, ma.request_id, ma.group_id, g.name, ma.leader_id,
			COALESCE(decider.username, ''), ma.decision, ma.note, ma.decided_at,
			g.leader_id, COALESCE(current_leader.is_active, FALSE), mr.status
		FROM migration_approvals AS ma
		JOIN migration_requests AS mr ON mr.id = ma.request_id
		JOIN groups AS g ON g.id = ma.group_id
		LEFT JOIN users AS decider ON decider.id = ma.leader_id
		LEFT JOIN users AS current_leader ON current_leader.id = g.leader_id
		WHERE mr.target_id = $1 AND mr.status IN ('PENDING_REVIEW', 'APPROVED', 'REJECTED')
		ORDER BY ma.request_id DESC, ma.group_id
	`
	approvalRows, err := repository.database.Query(ctx, approvalsQuery, targetID)
	if err != nil {
		return nil, fmt.Errorf("Gagal membaca approval: %w", err)
	}

	defer approvalRows.Close()
	for approvalRows.Next() {
		var approval core.MigrationApproval
		var currentLeaderID *int64
		var currentLeaderActive bool
		var requestStatus core.RequestStatus
		if err = approvalRows.Scan(
			&approval.ID, &approval.RequestID, &approval.GroupID, &approval.GroupName,
			&approval.LeaderID, &approval.LeaderName, &approval.Decision, &approval.Note,
			&approval.DecidedAt, &currentLeaderID, &currentLeaderActive, &requestStatus,
		); 
		
		err != nil {
			return nil, fmt.Errorf("Gagal membaca data approval: %w", err)
		}

		index, found := requestIndexes[approval.RequestID]
		if !found {
			continue
		}

		reviews[index].Approvals = append(reviews[index].Approvals, approval)
		if requestStatus == core.RequestPendingReview && approval.Decision == core.ApprovalPending &&
			currentLeaderID != nil && *currentLeaderID == currentUserID && currentLeaderActive {
			reviews[index].CanCurrentUserApprove = true
		}
	}

	return reviews, approvalRows.Err()
}

// lockPendingReview mengunci request dan memastikan keputusan masih pending review
func lockPendingReview(ctx context.Context, transaction pgx.Tx, targetID, requestID int64) error {
	const query = `
		SELECT status FROM migration_requests
		WHERE id = $1 AND target_id = $2
		FOR UPDATE
	`
	var status core.RequestStatus
	err := transaction.QueryRow(ctx, query, requestID, targetID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReviewNotFound
	}

	if err != nil {
		return err
	}

	if status != core.RequestPendingReview {
		return ErrReviewFinal
	}

	return nil
}

// ApproveReview menyetujui group milik leader dan menyelesaikan request bila semuanya setuju.
func (repository *MigrationRepository) ApproveReview(ctx context.Context, targetID, requestID, actorID int64, note string) (core.MigrationRequest, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal memulai approval: %w", err)
	}

	defer transaction.Rollback(ctx)
	if err = lockPendingReview(ctx, transaction, targetID, requestID); err != nil {
		return core.MigrationRequest{}, err
	}

	const approveQuery = `
		UPDATE migration_approvals AS ma
		SET decision = 'APPROVED', note = $3, decided_at = now(), leader_id = $2
		WHERE ma.request_id = $1 AND ma.decision = 'PENDING'
			AND EXISTS (
				SELECT 1 FROM groups AS g
				JOIN users AS u ON u.id = g.leader_id
				WHERE g.id = ma.group_id AND g.leader_id = $2 AND u.is_active
			)
	`
	result, err := transaction.Exec(ctx, approveQuery, requestID, actorID, strings.TrimSpace(note))
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan approval: %w", err)
	}

	if result.RowsAffected() == 0 {
		return core.MigrationRequest{}, ErrNotGroupLeader
	}

	const finalizeQuery = `
		UPDATE migration_requests AS mr
		SET status = 'APPROVED', updated_at = now()
		WHERE mr.id = $1
			AND NOT EXISTS (
				SELECT 1 FROM migration_approvals AS ma
				WHERE ma.request_id = mr.id AND ma.decision <> 'APPROVED'
			)
		RETURNING status
	`
	status := core.RequestPendingReview
	err = transaction.QueryRow(ctx, finalizeQuery, requestID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}

	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyelesaikan approval: %w", err)
	}

	if err = transaction.Commit(ctx); err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan keputusan approval: %w", err)
	}

	return core.MigrationRequest{ID: requestID, TargetID: targetID, Status: status}, nil
}

// RejectReview menolak group milik leader dan langsung menolak seluruh request.
func (repository *MigrationRepository) RejectReview(ctx context.Context, targetID, requestID, actorID int64, note string) (core.MigrationRequest, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal memulai rejection: %w", err)
	}

	defer transaction.Rollback(ctx)
	if err = lockPendingReview(ctx, transaction, targetID, requestID); err != nil {
		return core.MigrationRequest{}, err
	}

	const rejectQuery = `
		UPDATE migration_approvals AS ma
		SET decision = 'REJECTED', note = $3, decided_at = now(), leader_id = $2
		WHERE ma.request_id = $1 AND ma.decision = 'PENDING'
			AND EXISTS (
				SELECT 1 FROM groups AS g
				JOIN users AS u ON u.id = g.leader_id
				WHERE g.id = ma.group_id AND g.leader_id = $2 AND u.is_active
			)
	`
	result, err := transaction.Exec(ctx, rejectQuery, requestID, actorID, strings.TrimSpace(note))
	if err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan rejection: %w", err)
	}

	if result.RowsAffected() == 0 {
		return core.MigrationRequest{}, ErrNotGroupLeader
	}

	if _, err = transaction.Exec(ctx, `
		UPDATE migration_requests 
		SET status = 'REJECTED', updated_at = now()
		WHERE id = $1
	`, requestID); 
	
	err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menolak request: %w", err)
	}

	if err = transaction.Commit(ctx); err != nil {
		return core.MigrationRequest{}, fmt.Errorf("Gagal menyimpan keputusan rejection: %w", err)
	}

	return core.MigrationRequest{ID: requestID, TargetID: targetID, Status: core.RequestRejected}, nil
}