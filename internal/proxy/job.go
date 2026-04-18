package proxy

import (
	"encoding/json"
	"math"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	proxqtypes "github.com/psyb0t/proxq/pkg/types"
)

const TaskTypeName = "proxy:request"

const DefaultQueue = "default"

const (
	HeaderNameXProxqSource = proxqtypes.HeaderNameXProxqSource
	HeaderValueProxq       = proxqtypes.HeaderValueProxq
)

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

type taskEnvelope struct {
	Request                prawxxey.RequestPayload `json:"request"`
	RetryDelay             time.Duration           `json:"retryDelay,omitempty"`
	CacheKeyExcludeHeaders []string                `json:"cacheKeyExcludeHeaders,omitempty"` //nolint:lll
}

func RetryDelayFunc(
	n int, _ error, t *asynq.Task,
) time.Duration {
	var envelope taskEnvelope

	if err := json.Unmarshal(
		t.Payload(), &envelope,
	); err == nil && envelope.RetryDelay > 0 {
		return envelope.RetryDelay
	}

	//nolint:mnd
	return time.Duration(
		math.Pow(float64(n), 4),
	) * time.Second
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
