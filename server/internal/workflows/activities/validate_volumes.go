package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
)

type ValidateVolumesInput struct {
	DatabaseID string                 `json:"database_id"`
	Spec       *database.InstanceSpec `json:"spec"`
}

type ValidateVolumesOutput struct {
	HostID   string `json:"host_id"`
	NodeName string `json:"node_name"`
	Valid    bool   `json:"valid"`
	Error    string `json:"errors,omitempty"`
}

func (a *Activities) ExecuteValidateVolumes(
	ctx workflow.Context,
	hostID string,
	input *ValidateVolumesInput,
) workflow.Future[*ValidateVolumesOutput] {
	options := workflow.ActivityOptions{
		Queue: workflow.Queue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ValidateVolumesOutput](ctx, options, a.ValidateVolumes, input)
}

func (a *Activities) ValidateVolumes(ctx context.Context, input *ValidateVolumesInput) (*ValidateVolumesOutput, error) {
	logger := activity.Logger(ctx)

	if input == nil {
		return nil, errors.New("input is nil")
	}

	logger = logger.With("database_id", input.DatabaseID)
	logger.Info("starting volume validation")

	if input.Spec == nil {
		return nil, errors.New("spec is nil")
	}

	result, err := a.Orchestrator.ValidateVolumes(ctx, input.Spec)
	if err != nil {
		return nil, fmt.Errorf("volume validation failed: %w", err)
	}

	logger.Info("volume validation completed", "success", result.Valid)
	return &ValidateVolumesOutput{
		HostID:   input.Spec.HostID,
		NodeName: input.Spec.NodeName,
		Valid:    result.Valid,
		Error:    result.Error,
	}, nil
}
