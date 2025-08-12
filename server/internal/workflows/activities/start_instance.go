package activities

import (
	"context"
	"errors"
	"fmt"
	"github.com/pgEdge/control-plane/server/internal/resource"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type StartInstanceInput struct {
	DatabaseID string          `json:"database_id"`
	InstanceID string          `json:"instance_id"`
	TaskID     uuid.UUID       `json:"task_id"`
	State      *resource.State `json:"state"`
}

type StartInstanceOutput struct{}

func (a *Activities) ExecuteStartInstance(
	ctx workflow.Context,
	input *StartInstanceInput,
) workflow.Future[*StartInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*StartInstanceOutput](ctx, options, a.StartInstance, input)
}

func (a *Activities) StartInstance(ctx context.Context, input *StartInstanceInput) (*StartInstanceOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
	)
	logger.Info("starting start instance activity")

	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return nil, err
	}

	rc := &resource.Context{
		State:    input.State,
		Injector: a.Injector,
		Registry: registry,
	}

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	err = orch.StartInstance(ctx, rc, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to start instance : %w", err)
	}

	logger.Info("start instance completed")
	return &StartInstanceOutput{}, nil
}
