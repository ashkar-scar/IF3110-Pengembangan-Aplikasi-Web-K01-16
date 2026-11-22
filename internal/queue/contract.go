package queue

import "context"

type ActionType string

const (
	ActionUp          ActionType = "UP"
	ActionRollback    ActionType = "ROLLBACK"
	ActionForward     ActionType = "FORWARD"
	ActionCancelBatch ActionType = "CANCEL_BATCH"
)

type EnqueueRequest struct {
	RequestID int64
	TargetID  int64
	ActorID   int64
	ActorName string
	Action    ActionType
}

type JobEnqueuer interface {
	Enqueue(ctx context.Context, req EnqueueRequest) (jobID string, err error)
	Retry(ctx context.Context, jobID string, actorID int64, actorName string) (string, error)
	CancelBatch(ctx context.Context, jobID string, actorID int64, actorName string) (string, error)
}