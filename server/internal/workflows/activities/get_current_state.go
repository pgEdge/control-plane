package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type GetCurrentStateInput struct {
	DatabaseID string `json:"database_id"`
}

type GetCurrentStateOutput struct {
	State *resource.State `json:"state"`
}

func (a *Activities) ExecuteGetCurrentState(
	ctx workflow.Context,
	input *GetCurrentStateInput,
) workflow.Future[*GetCurrentStateOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetCurrentStateOutput](ctx, options, a.GetCurrentState, input)
}

func (a *Activities) GetCurrentState(ctx context.Context, input *GetCurrentStateInput) (*GetCurrentStateOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Info("getting current state")

	service, err := do.Invoke[*resource.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	state, err := service.GetState(ctx, input.DatabaseID)
	if errors.Is(err, resource.ErrStateNotFound) {
		// So that we can run plan on new databases before they're created.
		return &GetCurrentStateOutput{
			State: resource.NewState(),
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve current state: %w", err)
	}

	return &GetCurrentStateOutput{
		State: state,
	}, nil
}
