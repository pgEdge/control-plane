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
		options.Queue = core.Queue(input.Cohort.CohortID)
	} else {
		options.Queue = core.Queue(input.HostID)
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

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	prevState, err := dbSvc.GetStoredInstanceState(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current instance state: %w", err)
	}

	if prevState != database.InstanceStateStopped {
		return nil, fmt.Errorf("instance is not in stopped state, current state: %s", prevState)
	}
	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	err = orch.StartInstance(ctx, input.InstanceID)
	if err != nil {
		// Revert the instance state to original state in case of failure
		err = dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
			InstanceID: input.InstanceID,
			DatabaseID: input.DatabaseID,
			State:      prevState,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update instance: %w", err)
		}
		return nil, fmt.Errorf("failed to start instance : %w", err)
	}

	// Update the instance state to available
	err = dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
		InstanceID: input.InstanceID,
		DatabaseID: input.DatabaseID,
		State:      database.InstanceStateAvailable,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update instance: %w", err)
	}
	// Update the database state to available, since at least one instance is now available
	err = dbSvc.UpdateDatabaseState(ctx, input.DatabaseID, "", database.DatabaseStateAvailable)
	if err != nil {
		return nil, fmt.Errorf("failed to update database state: %w", err)
	}
	logger.Info("start instance completed")
	return &StartInstanceOutput{}, nil
}
