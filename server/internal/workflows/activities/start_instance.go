package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/samber/do"
)

type StartInstanceInput struct {
	DatabaseID string       `json:"database_id"`
	InstanceID string       `json:"instance_id"`
	HostID     string       `json:"host_id"`
	Cohort     *host.Cohort `json:"cohort"`
	TaskID     uuid.UUID    `json:"task_id"`
}

type StartInstanceOutput struct{}

func (a *Activities) ExecuteStartInstance(
	ctx workflow.Context,
	input *StartInstanceInput,
) workflow.Future[*StartInstanceOutput] {
	options := workflow.ActivityOptions{
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}

	if input.Cohort != nil {
		options.Queue = utils.ManagerQueue()
	} else {
		options.Queue = utils.HostQueue(input.HostID)
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

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	err = orch.StartInstance(ctx, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to start instance : %w", err)
	}

	logger.Info("start instance completed")
	return &StartInstanceOutput{}, nil
}
