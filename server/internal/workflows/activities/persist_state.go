package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type PersistStateInput struct {
	DatabaseID string          `json:"database_id"`
	State      *resource.State `json:"state"`
}

type PersistStateOutput struct{}

func (a *Activities) ExecutePersistState(
	ctx workflow.Context,
	input *PersistStateInput,
) workflow.Future[*PersistStateOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PersistStateOutput](ctx, options, a.PersistState, input)
}

func (a *Activities) PersistState(ctx context.Context, input *PersistStateInput) (*PersistStateOutput, error) {
	store, err := do.Invoke[*resource.Store](a.Injector)
	if err != nil {
		return nil, err
	}

	err = store.Put(&resource.StoredState{
		DatabaseID: input.DatabaseID,
		State:      input.State,
	}).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}

	return &PersistStateOutput{}, nil
}
