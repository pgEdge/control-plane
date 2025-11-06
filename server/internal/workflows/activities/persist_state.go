package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PersistStateOutput](ctx, options, a.PersistState, input)
}

func (a *Activities) PersistState(ctx context.Context, input *PersistStateInput) (*PersistStateOutput, error) {
	service, err := do.Invoke[*resource.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = service.PersistState(ctx, input.DatabaseID, input.State)
	if err != nil {
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}

	return &PersistStateOutput{}, nil
}
