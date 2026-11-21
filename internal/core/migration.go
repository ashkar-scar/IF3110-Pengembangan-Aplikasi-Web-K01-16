package core

import "time"

// Status eksekusi migration.
type AttemptStatus string

const (
	AttemptRunning AttemptStatus = "RUNNING"
	AttemptSuccess AttemptStatus = "SUCCESS"
	AttemptFailed  AttemptStatus = "FAILED"
	AttemptUnknown AttemptStatus = "UNKNOWN"
)

// ExecutionMode menentukan apakah worker mengeksekusi SQL sekaligus atau per batch.
type ExecutionMode string

const (
	ExecutionNormal ExecutionMode = "NORMAL"
	ExecutionBatch  ExecutionMode = "BATCH"
)

// MigrationAction menentukan SQL snapshot yang akan dieksekusi oleh worker.
type MigrationAction string

const (
	ActionUp          MigrationAction = "UP"
	ActionRollback    MigrationAction = "ROLLBACK"
	ActionForward     MigrationAction = "FORWARD"
	ActionCancelBatch MigrationAction = "CANCEL_BATCH"
)

// RequestStatus menunjukkan posisi pengajuan dalam workflow review dan eksekusi.
type RequestStatus string

const (
	RequestPendingReview RequestStatus = "PENDING_REVIEW"
	RequestApproved      RequestStatus = "APPROVED"
	RequestRejected      RequestStatus = "REJECTED"
	RequestEnqueued      RequestStatus = "ENQUEUED"
	RequestCompleted     RequestStatus = "COMPLETED"
	RequestFailed        RequestStatus = "FAILED"
)

// ApprovalDecision menyimpan keputusan satu leader untuk group yang diwakilinya.
type ApprovalDecision string

const (
	ApprovalPending  ApprovalDecision = "PENDING"
	ApprovalApproved ApprovalDecision = "APPROVED"
	ApprovalRejected ApprovalDecision = "REJECTED"
)

// Draft SQL migration yang masih bisa diedit sebelum dijalankan.
type Draft struct {
	ID          int64  `json:"id"`
	TargetID    int64  `json:"target_id"`
	Description string `json:"description"`

	SQL                string        `json:"-"`
	SQLUp              string        `json:"sql_up"`
	SQLDown            string        `json:"sql_down"`
	IsIrreversible     bool          `json:"is_irreversible"`
	IrreversibleReason string        `json:"irreversible_reason"`
	ExecutionMode      ExecutionMode `json:"execution_mode"`
	BatchColumn        string        `json:"batch_column"`
	BatchSize          int           `json:"batch_size"`
	IsLocked           bool          `json:"is_locked"`

	CreatedBy   int64     `json:"created_by"`
	Revision    int       `json:"revision"`
	BaseVersion int64     `json:"base_version"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MigrationApproval adalah satu keputusan group terhadap sebuah migration request.
type MigrationApproval struct {
	ID         int64            `json:"id"`
	RequestID  int64            `json:"request_id"`
	GroupID    int64            `json:"group_id"`
	GroupName  string           `json:"group_name"`
	LeaderID   int64            `json:"leader_id"`
	LeaderName string           `json:"leader_name"`
	Decision   ApprovalDecision `json:"decision"`
	Note       string           `json:"note"`
	DecidedAt  *time.Time       `json:"decided_at"`
}

// MigrationRequest adalah snapshot immutable draft yang menunggu approval atau eksekusi.
type MigrationRequest struct {
	ID                 int64               `json:"id"`
	TargetID           int64               `json:"target_id"`
	DraftID            int64               `json:"draft_id"`
	DraftRevision      int                 `json:"draft_revision"`
	Action             MigrationAction     `json:"action_type"`
	TargetExecutionID  *string             `json:"target_execution_id"`
	SQLSnapshotUp      string              `json:"sql_up"`
	SQLSnapshotDown    string              `json:"sql_down"`
	IsIrreversible     bool                `json:"is_irreversible"`
	IrreversibleReason string              `json:"irreversible_reason"`
	ExecutionMode      ExecutionMode       `json:"execution_mode"`
	BatchColumn        string              `json:"batch_column"`
	BatchSize          int                 `json:"batch_size"`
	BaseVersion        int64               `json:"base_version"`
	Fingerprint        string              `json:"fingerprint"`
	RequestedBy        int64               `json:"requested_by"`
	Status             RequestStatus       `json:"status"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	Approvals          []MigrationApproval `json:"approvals"`
}

// Catatan eksekusi satu revisi draft yang sudah dikunci.
type Attempt struct {
	ID          string        `json:"id"`
	TargetID    int64         `json:"target_id"`
	DraftID     int64         `json:"draft_id"`
	Revision    int           `json:"revision"`
	Description string        `json:"description"`
	SQL         string        `json:"sql"`
	ActorID     int64         `json:"actor_id"`
	ActorName   string        `json:"actor_name"`
	BaseVersion int64         `json:"base_version"`
	Version     *int64        `json:"version"`
	Status      AttemptStatus `json:"status"`
	Error       string        `json:"error"`
	StartedAt   time.Time     `json:"started_at"`
	FinishedAt  *time.Time    `json:"finished_at"`
}
