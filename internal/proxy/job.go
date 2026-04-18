package proxy

import (
	"time"

	"github.com/hibiken/asynq"
)

const TaskTypeName = "proxy:request"

const DefaultQueue = "default"

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

func StatusFromTaskState(s asynq.TaskState) Status {
	switch s {
	case asynq.TaskStatePending,
		asynq.TaskStateScheduled,
		asynq.TaskStateAggregating:
		return StatusQueued
	case asynq.TaskStateActive,
		asynq.TaskStateRetry:
		return StatusRunning
	case asynq.TaskStateCompleted:
		return StatusCompleted
	case asynq.TaskStateArchived:
		return StatusFailed
	}

	return StatusFailed
}

type jobInfo struct {
	ID          string    `json:"id"`
	Status      Status    `json:"status"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	CompletedAt time.Time `json:"completedAt,omitzero"`
}

type statusResponse struct {
	Status string `json:"status"`
}

//nolint:gochecknoglobals
var cancelledResponse = statusResponse{
	Status: "cancelled",
}
