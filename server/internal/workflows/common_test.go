package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/cschleiden/go-workflows/tester"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func TestApplyEventsWaitsForQueuedEventsOnQueueError(t *testing.T) {
	newEvent := func(eventType resource.EventType, id string, executor resource.Executor) *resource.Event {
		return &resource.Event{
			Type: eventType,
			Resource: &resource.ResourceData{
				Identifier: resource.Identifier{Type: "test.resource", ID: id},
				Executor:   executor,
			},
		}
	}

	for _, tc := range []struct {
		name          string
		unqueueable   *resource.Event
		removeHosts   []string
		activityErr   error
		expectedErrs  []string
		expectApplied bool
	}{
		{
			name:        "unknown executor",
			unqueueable: newEvent(resource.EventTypeUpdate, "unqueueable", resource.Executor{Type: "unknown"}),
			expectedErrs: []string{
				"failed to queue apply event",
				"unknown executor type",
			},
			expectApplied: true,
		},
		{
			name:        "host removed",
			unqueueable: newEvent(resource.EventTypeUpdate, "unqueueable", resource.HostExecutor("host-2")),
			removeHosts: []string{"host-2"},
			expectedErrs: []string{
				"cannot queue event type update for test.resource::unqueueable because its host is being removed",
			},
			expectApplied: true,
		},
		{
			name:        "no primary",
			unqueueable: newEvent(resource.EventTypeUpdate, "unqueueable", resource.PrimaryExecutor("n1")),
			expectedErrs: []string{
				"cannot queue event type update for test.resource::unqueueable because its node has no primary instance",
			},
			expectApplied: true,
		},
		{
			name:        "queued activity fails",
			unqueueable: newEvent(resource.EventTypeUpdate, "unqueueable", resource.Executor{Type: "unknown"}),
			activityErr: errors.New("activity failed"),
			expectedErrs: []string{
				"unknown executor type",
				"failed to apply refresh event to test.resource::queued: activity failed",
			},
			expectApplied: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &Workflows{Activities: &activities.Activities{}}

			queued := newEvent(resource.EventTypeRefresh, "queued", resource.HostExecutor("host-1"))
			// This event comes after the unqueueable event, so it should never
			// be queued.
			skipped := newEvent(resource.EventTypeRefresh, "skipped", resource.HostExecutor("host-1"))
			plan := resource.Plan{{queued, tc.unqueueable, skipped}}

			state := resource.NewState()
			// A node without a primary instance for the PrimaryExecutor case.
			node, err := resource.ToResourceData(&database.NodeResource{
				Name:        "n1",
				InstanceIDs: []string{"n1-instance"},
			})
			require.NoError(t, err)
			state.Add(node)

			wf := func(ctx workflow.Context) error {
				return w.applyEvents(ctx, "database-1", uuid.Nil, state, nil, plan, tc.removeHosts...)
			}

			var output *activities.ApplyEventOutput
			if tc.activityErr == nil {
				output = &activities.ApplyEventOutput{Event: queued}
			}
			wt := tester.NewWorkflowTester[any](wf)
			wt.OnActivity(w.Activities.ApplyEvent, mock.Anything, mock.Anything).
				Return(output, tc.activityErr).
				Once()

			// Before the fix, returning while the first event's activity was
			// still pending caused the tester to panic with a "pending
			// futures" error.
			wt.Execute(context.Background())

			require.True(t, wt.WorkflowFinished())
			_, err = wt.WorkflowResult()
			require.Error(t, err)
			for _, expected := range tc.expectedErrs {
				assert.ErrorContains(t, err, expected)
			}
			wt.AssertExpectations(t)

			_, ok := state.Get(queued.Resource.Identifier)
			assert.Equal(t, tc.expectApplied, ok, "queued event's output applied to state")
			_, ok = state.Get(skipped.Resource.Identifier)
			assert.False(t, ok, "event after the unqueueable event should not be queued")
		})
	}
}
