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
		options.Queue = utils.CohortQueue(input.Cohort.CohortID)
	} else {
		options.Queue = utils.HostQueue(input.HostID)
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

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	prevState, err := dbSvc.GetStoredInstanceState(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current instance state: %w", err)
	}

	if prevState == database.InstanceStateStopped {
		logger.Info("instance already in stopped state")
		return &StopInstanceOutput{}, nil
	}

	if prevState != database.InstanceStateAvailable {
		return nil, fmt.Errorf("instance is not available or running, current state: %s", prevState)
	}

	err = orch.StopInstance(ctx, input.InstanceID)
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

		return nil, fmt.Errorf("failed to stop instance : %w", err)
	}

	// Update the instance state to stopped
	err = dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
		InstanceID: input.InstanceID,
		DatabaseID: input.DatabaseID,
		State:      database.InstanceStateStopped,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update instance: %w", err)
	}

	instances, err := dbSvc.GetInstances(ctx, input.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}
	isDatabaseFullyStopped := true
	for _, inst := range instances {
		if inst.State != database.InstanceStateStopped {
			isDatabaseFullyStopped = false
			break
		}
	}
	// In case all instances are stopped, update the database state to stopped
	// This is to handle the case where the database was running with multiple instances
	// and now all instances are stopped.
	if isDatabaseFullyStopped {
		err = dbSvc.UpdateDatabaseState(ctx, input.DatabaseID, "", database.DatabaseStateStopped)
		if err != nil {
			return nil, fmt.Errorf("failed to update database state: %w", err)
		}
	}
	logger.Info("stop instance completed")
	return &StopInstanceOutput{}, nil
}
