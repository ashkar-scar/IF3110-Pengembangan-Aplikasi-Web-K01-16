package core

import "time"

type AttemptStatus string

const (
	AttemptRunning AttemptStatus = "RUNNING"
	AttemptSuccess AttemptStatus = "SUCCESS"
	AttemptFailed  AttemptStatus = "FAILED"
	AttemptUnknown AttemptStatus = "UNKNOWN"
)

type Draft struct {
	ID          int64     `json:"id"`
	TargetID    int64     `json:"target_id"`
	Description string    `json:"description"`
	SQL         string    `json:"sql"`
	CreatedBy   int64     `json:"created_by"`
	Revision    int       `json:"revision"`
	BaseVersion int64     `json:"base_version"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

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
