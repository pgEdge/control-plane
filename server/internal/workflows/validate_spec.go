package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ValidateVolumesInput struct {
	DatabaseID uuid.UUID
	Spec       *database.Spec
}

func (w *Workflows) ValidateSpec(ctx workflow.Context, input *ValidateVolumesInput) (*activities.ValidateVolumesOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("Starting volume validation")

	activityInput := &activities.ValidateVolumesInput{
		DatabaseID: input.DatabaseID,
		Spec:       input.Spec,
	}

	output, err := w.Activities.ExecuteValidateVolumes(ctx, activityInput).Get(ctx)
	if err != nil {
		logger.Error("Volume validation activity failed", "error", err)
		return output, fmt.Errorf("volume validation activity error: %w", err)
	}
	if !output.Valid {
		logger.Error("Volume validation failed", "errors", output.Errors)
		return output, fmt.Errorf("volume validation errors: %v", output.Errors)
	}

	logger.Info("Volume validation succeeded")
	return output, nil
}
