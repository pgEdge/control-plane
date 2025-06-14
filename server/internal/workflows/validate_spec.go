package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ValidateSpecInput struct {
	DatabaseID string
	Spec       *database.Spec
}

type ValidateSpecOutput struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func (w *Workflows) ValidateSpec(ctx workflow.Context, input *ValidateSpecInput) (*ValidateSpecOutput, error) {
	databaseID := input.DatabaseID
	logger := workflow.Logger(ctx).With("database_id", databaseID)
	logger.Info("Starting volume validation")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		logger.Error("Failed to get node instances", "error", err)
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}
	var instanceFutures []workflow.Future[*activities.ValidateVolumesOutput]
	for _, nodeInstance := range nodeInstances {
		for _, instance := range nodeInstance.Instances {
			instanceFuture := w.Activities.ExecuteValidateVolumes(ctx, instance.HostID, &activities.ValidateVolumesInput{
				DatabaseID: databaseID,
				Spec:       instance,
			})
			instanceFutures = append(instanceFutures, instanceFuture)
		}
	}

	var allErrors []string
	for _, instanceFuture := range instanceFutures {
		output, err := instanceFuture.Get(ctx)
		if err != nil {
			logger.Error("Volume validation activity failed", "error", err)
			allErrors = append(allErrors, fmt.Sprintf("activity error: %v", err))
			continue
		}

		if !output.Valid {
			logger.Error("Volume validation failed", "errors", output.Errors)
			allErrors = append(allErrors, output.Errors...)
		}
	}

	if len(allErrors) > 0 {
		return &ValidateSpecOutput{
			Valid:  false,
			Errors: allErrors,
		}, fmt.Errorf("volume validation encountered %d issues", len(allErrors))
	}

	logger.Info("Volume validation succeeded")
	return &ValidateSpecOutput{Valid: true}, nil
}
