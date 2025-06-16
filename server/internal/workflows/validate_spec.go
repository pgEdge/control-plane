package workflows

import (
	"errors"
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
	logger.Info("starting volume validation")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		logger.Error("failed to get node instances", "error", err)
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}
	var instanceFutures []workflow.Future[*activities.ValidateVolumesOutput]
	for _, nodeInstance := range nodeInstances {
		for _, instance := range nodeInstance.Instances {
			if len(instance.ExtraVolumes) < 1 {
				continue
			}

			instanceFuture := w.Activities.ExecuteValidateVolumes(ctx, instance.HostID, &activities.ValidateVolumesInput{
				DatabaseID: databaseID,
				Spec:       instance,
			})
			instanceFutures = append(instanceFutures, instanceFuture)
		}
	}

	overallResult := &ValidateSpecOutput{
		Valid: true,
	}

	var allErrors []error
	for _, instanceFuture := range instanceFutures {
		output, err := instanceFuture.Get(ctx)
		if err != nil {
			allErrors = append(allErrors, err)
			continue
		}

		if !output.Valid {
			overallResult.Valid = false
			overallResult.Errors = append(
				overallResult.Errors,
				fmt.Sprintf("invalid volumes for node %s, host %s: %s", output.NodeName, output.HostID, output.Error),
			)
		}
	}

	if err := errors.Join(allErrors...); err != nil {
		logger.Error("failed to validate volumes", "error", err)
		return nil, fmt.Errorf("failed to validate volumes: %w", err)
	}

	logger.Info("volume validation succeeded")
	return overallResult, nil
}
