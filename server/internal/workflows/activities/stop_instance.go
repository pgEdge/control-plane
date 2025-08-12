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

type StopInstanceInput struct {
	DatabaseID string          `json:"database_id"`
	InstanceID string          `json:"instance_id"`
	TaskID     uuid.UUID       `json:"task_id"`
	State      *resource.State `json:"state"`
}

type StopInstanceOutput struct{}

func (a *Activities) ExecuteStopInstance(
	ctx workflow.Context,
	input *StopInstanceInput,
) workflow.Future[*StopInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*StopInstanceOutput](ctx, options, a.StopInstance, input)
}

func (a *Activities) StopInstance(ctx context.Context, input *StopInstanceInput) (*StopInstanceOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
	)
	logger.Info("starting stop instance activity")

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

	err = orch.StopInstance(ctx, rc, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to stop instance : %w", err)
	}

	logger.Info("stop instance completed")
	return &StopInstanceOutput{}, nil
}
