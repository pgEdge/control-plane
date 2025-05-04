package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type UpdateDatabaseInput struct {
	Spec        *database.Spec `json:"spec"`
	ForceUpdate bool           `json:"force_update"`
}

type UpdateDatabaseOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) UpdateDatabase(ctx workflow.Context, input *UpdateDatabaseInput) (*UpdateDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID.String())
	logger.Info("updating database")

	handleError := func(err error) error {
		logger.With("error", err).Error("failed to update database")
		updateStateInput := &activities.UpdateDbStateInput{
			DatabaseID: input.Spec.DatabaseID,
			State:      database.DatabaseStateFailed,
		}
		_, stateErr := w.Activities.
			ExecuteUpdateDbState(ctx, updateStateInput).
			Get(ctx)
		if stateErr != nil {
			logger.With("error", stateErr).Error("failed to update database state")
		}
		return err
	}

	getCurrentInput := &activities.GetCurrentStateInput{
		DatabaseID: input.Spec.DatabaseID,
	}
	getCurrentOutput, err := w.Activities.
		ExecuteGetCurrentState(ctx, getCurrentInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}

	getDesiredInput := &GetDesiredStateInput{
		Spec: input.Spec,
	}
	getDesiredOutput, err := w.ExecuteGetDesiredState(ctx, getDesiredInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get desired state: %w", err))
	}

	reconcileInput := &ReconcileStateInput{
		DatabaseID:  input.Spec.DatabaseID,
		Current:     getCurrentOutput.State,
		Desired:     getDesiredOutput.State,
		ForceUpdate: input.ForceUpdate,
	}
	reconcileOutput, err := w.ExecuteReconcileState(ctx, reconcileInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to reconcile state: %w", err))
	}

	updateStateInput := &activities.UpdateDbStateInput{
		DatabaseID: input.Spec.DatabaseID,
		State:      database.DatabaseStateAvailable,
	}
	_, err = w.Activities.
		ExecuteUpdateDbState(ctx, updateStateInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to update database state to available: %w", err))
	}

	logger.Info("successfully updated database")

	return &UpdateDatabaseOutput{
		Updated: reconcileOutput.Updated,
	}, nil
}
