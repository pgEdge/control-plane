package workflows

import (
	"context"
	"testing"

	"github.com/cschleiden/go-workflows/tester"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func TestApplyEventsWaitsForQueuedEventsOnQueueError(t *testing.T) {
	w := &Workflows{Activities: &activities.Activities{}}

	queued := &resource.Event{
		Type: resource.EventTypeRefresh,
		Resource: &resource.ResourceData{
			Identifier: resource.Identifier{Type: "test.resource", ID: "queued"},
			Executor:   resource.HostExecutor("host-1"),
		},
	}
	unqueueable := &resource.Event{
		Type: resource.EventTypeUpdate,
		Resource: &resource.ResourceData{
			Identifier: resource.Identifier{Type: "test.resource", ID: "unqueueable"},
			Executor:   resource.Executor{Type: "unknown"},
		},
	}
	plan := resource.Plan{{queued, unqueueable}}
	state := resource.NewState()

	wf := func(ctx workflow.Context) error {
		return w.applyEvents(ctx, "database-1", uuid.Nil, state, nil, plan)
	}

	wt := tester.NewWorkflowTester[any](wf)
	wt.OnActivity(w.Activities.ApplyEvent, mock.Anything, mock.Anything).
		Return(&activities.ApplyEventOutput{Event: queued}, nil)

	// Before the fix, returning while the first event's activity was still
	// pending caused the tester to panic with a "pending futures" error.
	wt.Execute(context.Background())

	require.True(t, wt.WorkflowFinished())
	_, err := wt.WorkflowResult()
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to queue apply event")
	assert.ErrorContains(t, err, "unknown executor type")
	wt.AssertExpectations(t)

	// The queued event's output is still applied to the state.
	_, ok := state.Get(queued.Resource.Identifier)
	assert.True(t, ok)
}
