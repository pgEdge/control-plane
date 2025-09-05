package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type ValidateInstanceSpecsInput struct {
	DatabaseID string                   `json:"database_id"`
	Specs      []*database.InstanceSpec `json:"spec"`
}

type ValidateInstanceSpecsOutput struct {
	Results []*database.ValidationResult `json:"results"`
}

func (a *Activities) ExecuteValidateInstanceSpecs(
	ctx workflow.Context,
	hostID string,
	input *ValidateInstanceSpecsInput,
) workflow.Future[*ValidateInstanceSpecsOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ValidateInstanceSpecsOutput](ctx, options, a.ValidateInstanceSpecs, input)
}

func (a *Activities) ValidateInstanceSpecs(ctx context.Context, input *ValidateInstanceSpecsInput) (*ValidateInstanceSpecsOutput, error) {
	logger := activity.Logger(ctx)

	if input == nil {
		return nil, errors.New("input is nil")
	}

	logger = logger.With(
		"database_id", input.DatabaseID,
		"host_id", a.Config.HostID,
	)
	logger.Info("starting instance spec validation")

	results, err := a.Orchestrator.ValidateInstanceSpecs(ctx, input.Specs)
	if err != nil {
		return nil, fmt.Errorf("instance spec validation failed: %w", err)
	}

	logger.Info("instance spec validation completed")
	return &ValidateInstanceSpecsOutput{
		Results: results,
	}, nil
}
