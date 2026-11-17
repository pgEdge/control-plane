package docker

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestCountServiceTasks(t *testing.T) {
	updatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	before := updatedAt.Add(-time.Hour)
	after := updatedAt.Add(time.Second)

	service := swarm.Service{
		Meta: swarm.Meta{UpdatedAt: updatedAt},
	}
	readyNodes := ds.NewSet("node-1")

	task := func(createdAt time.Time, nodeID string, state, desired swarm.TaskState, errMsg string) swarm.Task {
		return swarm.Task{
			Meta:         swarm.Meta{CreatedAt: createdAt},
			NodeID:       nodeID,
			DesiredState: desired,
			Status: swarm.TaskStatus{
				State: state,
				Err:   errMsg,
			},
		}
	}

	for _, tc := range []struct {
		name     string
		tasks    []swarm.Task
		expected serviceTaskCounts
	}{
		{
			name: "historic failed task is ignored",
			tasks: []swarm.Task{
				task(before, "node-1", swarm.TaskStateFailed, swarm.TaskStateShutdown, "old failure"),
				task(after, "node-1", swarm.TaskStateStarting, swarm.TaskStateRunning, ""),
			},
			expected: serviceTaskCounts{
				taskStates: []string{"failed", "starting"},
			},
		},
		{
			name: "failed task created after update is counted",
			tasks: []swarm.Task{
				task(after, "node-1", swarm.TaskStateFailed, swarm.TaskStateShutdown, "new failure"),
			},
			expected: serviceTaskCounts{
				failed:         1,
				lastFailureMsg: "new failure",
				taskStates:     []string{"failed"},
			},
		},
		{
			name: "rejected task created after update is counted",
			tasks: []swarm.Task{
				task(after, "node-1", swarm.TaskStateRejected, swarm.TaskStateShutdown, "rejected"),
			},
			expected: serviceTaskCounts{
				failed:         1,
				lastFailureMsg: "rejected",
				taskStates:     []string{"rejected"},
			},
		},
		{
			name: "running tasks created before update are counted",
			tasks: []swarm.Task{
				task(before, "node-1", swarm.TaskStateRunning, swarm.TaskStateRunning, ""),
				task(before, "node-1", swarm.TaskStateRunning, swarm.TaskStateShutdown, ""),
			},
			expected: serviceTaskCounts{
				running:    1,
				stopping:   1,
				taskStates: []string{"running", "running"},
			},
		},
		{
			name: "transitional states are counted",
			tasks: []swarm.Task{
				task(after, "node-1", swarm.TaskStatePending, swarm.TaskStateRunning, ""),
				task(after, "node-1", swarm.TaskStateAssigned, swarm.TaskStateRunning, ""),
				task(after, "node-1", swarm.TaskStateAccepted, swarm.TaskStateRunning, ""),
				task(after, "node-1", swarm.TaskStatePreparing, swarm.TaskStateRunning, ""),
			},
			expected: serviceTaskCounts{
				pending:    3,
				preparing:  1,
				taskStates: []string{"pending", "assigned", "accepted", "preparing"},
			},
		},
		{
			name: "unassigned transitional tasks are counted",
			tasks: []swarm.Task{
				task(after, "", swarm.TaskStatePending, swarm.TaskStateRunning, ""),
				task(after, "", swarm.TaskStatePending, swarm.TaskStateReady, ""),
			},
			expected: serviceTaskCounts{
				pending:    2,
				taskStates: []string{"pending", "pending"},
			},
		},
		{
			name: "transitional tasks that are being shut down are ignored",
			tasks: []swarm.Task{
				task(after, "node-1", swarm.TaskStatePreparing, swarm.TaskStateShutdown, ""),
				task(after, "node-1", swarm.TaskStateAssigned, swarm.TaskStateRemove, ""),
			},
			expected: serviceTaskCounts{
				taskStates: []string{"preparing", "assigned"},
			},
		},
		{
			name: "tasks on nodes that are not ready are ignored",
			tasks: []swarm.Task{
				task(after, "node-2", swarm.TaskStateFailed, swarm.TaskStateShutdown, "down"),
				task(after, "node-2", swarm.TaskStateRunning, swarm.TaskStateRunning, ""),
			},
			expected: serviceTaskCounts{
				taskStates: []string{"failed", "running"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, countServiceTasks(service, tc.tasks, readyNodes))
		})
	}
}

func TestCountServiceTasksRollingUpdate(t *testing.T) {
	startedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	createdAt := startedAt.Add(time.Second)
	completedAt := startedAt.Add(time.Minute)
	readyNodes := ds.NewSet("node-1")

	failedTask := func(createdAt time.Time) swarm.Task {
		return swarm.Task{
			Meta:         swarm.Meta{CreatedAt: createdAt},
			NodeID:       "node-1",
			DesiredState: swarm.TaskStateShutdown,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateFailed,
				Err:   "failure",
			},
		}
	}

	t.Run("task created before completion and failed after is counted", func(t *testing.T) {
		// Completing the update is a status-only change that advances
		// UpdatedAt past the creation time of the update's tasks.
		service := swarm.Service{
			Meta: swarm.Meta{UpdatedAt: completedAt},
			UpdateStatus: &swarm.UpdateStatus{
				State:       swarm.UpdateStateCompleted,
				StartedAt:   &startedAt,
				CompletedAt: &completedAt,
			},
		}
		expected := serviceTaskCounts{
			failed:         1,
			lastFailureMsg: "failure",
			taskStates:     []string{"failed"},
		}
		assert.Equal(t, expected, countServiceTasks(service, []swarm.Task{failedTask(createdAt)}, readyNodes))
	})

	t.Run("task created before update started is ignored", func(t *testing.T) {
		service := swarm.Service{
			Meta: swarm.Meta{UpdatedAt: completedAt},
			UpdateStatus: &swarm.UpdateStatus{
				State:       swarm.UpdateStateCompleted,
				StartedAt:   &startedAt,
				CompletedAt: &completedAt,
			},
		}
		expected := serviceTaskCounts{
			taskStates: []string{"failed"},
		}
		assert.Equal(t, expected, countServiceTasks(service, []swarm.Task{failedTask(startedAt.Add(-time.Hour))}, readyNodes))
	})
}
