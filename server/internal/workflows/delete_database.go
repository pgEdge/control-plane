package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type DeleteDatabaseInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
}

type DeleteDatabaseOutput struct{}

func (w *Workflows) DeleteDatabase(ctx workflow.Context, input *DeleteDatabaseInput) (*DeleteDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("deleting database")

	handleError := func(err error) error {
		logger.With("error", err).Error("failed to delete database")
		updateStateInput := &activities.UpdateDbStateInput{
			DatabaseID: input.DatabaseID,
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
		DatabaseID: input.DatabaseID,
	}
	getCurrentOutput, err := w.Activities.
		ExecuteGetCurrentState(ctx, getCurrentInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}

	reconcileInput := &ReconcileStateInput{
		DatabaseID: input.DatabaseID,
		Current:    getCurrentOutput.State,
		Desired:    resource.NewState(),
	}
	_, err = w.ExecuteReconcileState(ctx, reconcileInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to reconcile state: %w", err))
	}

	deleteInput := &activities.DeleteDbEntitiesInput{
		DatabaseID: input.DatabaseID,
	}
	_, err = w.Activities.ExecuteDeleteDbEntities(ctx, deleteInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to delete database entities: %w", err))
	}

	logger.Info("successfully deleted database")

	return &DeleteDatabaseOutput{}, nil
}
