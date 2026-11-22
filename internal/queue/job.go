package queue

import (
	"errors"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"time"
)

var (
	ErrNotFound      = errors.New("job atau request tidak ditemukan")
	ErrForbidden     = errors.New("izin EDIT dan user aktif diperlukan")
	ErrApproval      = errors.New("approval seluruh leader aktif diperlukan")
	ErrConflict      = errors.New("status job atau request tidak mengizinkan operasi ini")
	ErrNoGroupLeader = errors.New("setiap group berizin wajib memiliki leader aktif")
)

type Job struct {
	ID               string             `json:"id"`
	RequestID        int64              `json:"request_id"`
	TargetID         int64              `json:"target_id"`
	Action           ActionType         `json:"action_type"`
	Mode             core.ExecutionMode `json:"execution_mode"`
	Status           string             `json:"status"`
	TotalBatches     int                `json:"total_batches"`
	CompletedBatches int                `json:"completed_batches"`
	RowsProcessed    int64              `json:"rows_processed"`
	LastCursor       string             `json:"last_cursor"`
	RetryCount       int                `json:"retry_count"`
	MaxRetries       int                `json:"max_retries"`
	NextRetryAt      *time.Time         `json:"next_retry_at"`
	LockedBy         string             `json:"-"`
	LockExpiresAt    *time.Time         `json:"-"`
	TraceContext     string             `json:"-"`
	SafeError        string             `json:"safe_error"`
	CreatedBy        int64              `json:"created_by"`
	CreatedAt        time.Time          `json:"created_at"`
	StartedAt        *time.Time         `json:"started_at"`
	FinishedAt       *time.Time         `json:"finished_at"`
	CanRetry         bool               `json:"can_retry"`
	CanCancelBatch   bool               `json:"can_cancel_batch"`
	Checkpoints      []Checkpoint       `json:"checkpoints"`
}

type Checkpoint struct {
	BatchNo      int        `json:"batch_no"`
	CursorStart  string     `json:"cursor_start"`
	CursorEnd    string     `json:"cursor_end"`
	RowsAffected int        `json:"rows_affected"`
	Status       string     `json:"status"`
	CommittedAt  time.Time  `json:"committed_at"`
	RevertedAt   *time.Time `json:"reverted_at"`
}
