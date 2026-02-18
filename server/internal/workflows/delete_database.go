package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type DeleteDatabaseInput struct {
	DatabaseID string    `json:"database_id"`
	TaskID     uuid.UUID `json:"task_id"`
}

type DeleteDatabaseOutput struct{}

func (w *Workflows) DeleteDatabase(ctx workflow.Context, input *DeleteDatabaseInput) (*DeleteDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Info("deleting database")

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			updateStateInput := &activities.UpdateDbStateInput{
				DatabaseID: input.DatabaseID,
				State:      database.DatabaseStateFailed,
			}

			_, stateErr := w.Activities.ExecuteUpdateDbState(cleanupCtx, updateStateInput).Get(cleanupCtx)
			if stateErr != nil {
				logger.With("error", stateErr).Error("failed to update database state")
			}
			w.cancelTask(cleanupCtx, task.ScopeDatabase, input.DatabaseID, input.TaskID, logger)

		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to delete database")

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

		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	refreshCurrentInput := &RefreshCurrentStateInput{
		DatabaseID: input.DatabaseID,
		TaskID:     input.TaskID,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	plans, err := operations.UpdateDatabase(operations.UpdateDatabaseOptions{}, current, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to plan database delete: %w", err)
	}

	err = w.applyPlans(ctx, input.DatabaseID, input.TaskID, current, plans)
	if err != nil {
		return nil, handleError(err)
	}

	deleteInput := &activities.DeleteDbEntitiesInput{
		DatabaseID: input.DatabaseID,
	}
	_, err = w.Activities.ExecuteDeleteDbEntities(ctx, deleteInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to delete database entities: %w", err))
	}

	updateTaskInput = &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully deleted database")

	return &DeleteDatabaseOutput{}, nil
}
