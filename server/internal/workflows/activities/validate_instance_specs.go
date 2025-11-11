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
	DatabaseID    string                   `json:"database_id"`
	Specs         []*database.InstanceSpec `json:"spec"`
	PreviousSpecs []*database.InstanceSpec `json:"previous_spec"`
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

	var prevIndex map[string]*database.InstanceSpec
	if len(input.PreviousSpecs) > 0 {
		prevIndex = make(map[string]*database.InstanceSpec, len(input.PreviousSpecs))
		for _, p := range input.PreviousSpecs {
			if p == nil {
				continue
			}
			prevIndex[p.NodeName] = p
		}
	}

	changes := make([]*database.InstanceSpecChange, 0, len(input.Specs))
	for _, cur := range input.Specs {
		var prev *database.InstanceSpec
		if prevIndex != nil {
			prev = prevIndex[cur.NodeName]
		}
		changes = append(changes, &database.InstanceSpecChange{
			Previous: prev,
			Current:  cur,
		})
	}

	results, err := a.Orchestrator.ValidateInstanceSpecs(ctx, changes)
	if err != nil {
		return nil, fmt.Errorf("instance spec validation failed: %w", err)
	}

	logger.Info("instance spec validation completed")
	return &ValidateInstanceSpecsOutput{
		Results: results,
	}, nil
}
