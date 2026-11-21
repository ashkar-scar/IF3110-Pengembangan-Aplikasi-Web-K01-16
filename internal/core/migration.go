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
