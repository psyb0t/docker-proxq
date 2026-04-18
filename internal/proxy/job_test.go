package proxy

import (
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
)

func TestStatusFromTaskState(t *testing.T) {
	tests := []struct {
		name     string
		state    asynq.TaskState
		expected Status
	}{
		{
			name:     "pending maps to queued",
			state:    asynq.TaskStatePending,
			expected: StatusQueued,
		},
		{
			name:     "scheduled maps to queued",
			state:    asynq.TaskStateScheduled,
			expected: StatusQueued,
		},
		{
			name:     "aggregating maps to queued",
			state:    asynq.TaskStateAggregating,
			expected: StatusQueued,
		},
		{
			name:     "active maps to running",
			state:    asynq.TaskStateActive,
			expected: StatusRunning,
		},
		{
			name:     "retry maps to running",
			state:    asynq.TaskStateRetry,
			expected: StatusRunning,
		},
		{
			name:     "completed maps to completed",
			state:    asynq.TaskStateCompleted,
			expected: StatusCompleted,
		},
		{
			name:     "archived maps to failed",
			state:    asynq.TaskStateArchived,
			expected: StatusFailed,
		},
		{
			name:     "unknown state maps to failed",
			state:    asynq.TaskState(99),
			expected: StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(
				t, tt.expected,
				StatusFromTaskState(tt.state),
			)
		})
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "task type name",
			got:      TaskTypeName,
			expected: "proxy:request",
		},
		{
			name:     "default queue",
			got:      DefaultQueue,
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.got)
		})
	}
}
