package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
)

type ValidateInstanceSpecInput struct {
	DatabaseID string                 `json:"database_id"`
	Spec       *database.InstanceSpec `json:"spec"`
}

type ValidateInstanceSpecOutput struct {
	HostID   string `json:"host_id"`
	NodeName string `json:"node_name"`
	Valid    bool   `json:"valid"`
	Error    string `json:"errors,omitempty"`
}

func (a *Activities) ExecuteValidateInstanceSpec(
	ctx workflow.Context,
	hostID string,
	input *ValidateInstanceSpecInput,
) workflow.Future[*ValidateInstanceSpecOutput] {
	options := workflow.ActivityOptions{
		Queue: workflow.Queue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ValidateInstanceSpecOutput](ctx, options, a.ValidateInstanceSpec, input)
}

func (a *Activities) ValidateInstanceSpec(ctx context.Context, input *ValidateInstanceSpecInput) (*ValidateInstanceSpecOutput, error) {
	logger := activity.Logger(ctx)

	if input == nil {
		return nil, errors.New("input is nil")
	}

	logger = logger.With(
		"database_id", input.DatabaseID,
		"node_name", input.Spec.NodeName,
		"host_id", input.Spec.HostID,
	)
	logger.Info("starting instance spec validation")

	if input.Spec == nil {
		return nil, errors.New("spec is nil")
	}

	result, err := a.Orchestrator.ValidateInstanceSpec(ctx, input.Spec)
	if err != nil {
		return nil, fmt.Errorf("instance spec validation failed: %w", err)
	}

	logger.Info("instance spec validation completed", "success", result.Valid)
	return &ValidateInstanceSpecOutput{
		HostID:   input.Spec.HostID,
		NodeName: input.Spec.NodeName,
		Valid:    result.Valid,
		Error:    result.Error,
	}, nil
}
