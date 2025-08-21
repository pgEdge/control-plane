package activities

import (
	"context"
	"errors"
	"fmt"
	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/samber/do"
)

type StopInstanceInput struct {
	DatabaseID string       `json:"database_id"`
	InstanceID string       `json:"instance_id"`
	HostID     string       `json:"host_id"`
	Cohort     *host.Cohort `json:"cohort,omitempty"`
	TaskID     uuid.UUID    `json:"task_id"`
}

type StopInstanceOutput struct{}

func (a *Activities) ExecuteStopInstance(
	ctx workflow.Context,
	input *StopInstanceInput,
) workflow.Future[*StopInstanceOutput] {
	options := workflow.ActivityOptions{
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}

	if input.Cohort != nil {
		options.Queue = core.Queue(input.Cohort.CohortID)
	} else {
		options.Queue = core.Queue(input.HostID)
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

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	err = orch.StopInstance(ctx, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to stop instance : %w", err)
	}

	logger.Info("stop instance completed")
	return &StopInstanceOutput{}, nil
}
