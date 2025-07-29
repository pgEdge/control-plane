package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type UpdateDatabaseInput struct {
	TaskID      uuid.UUID      `json:"task_id"`
	Spec        *database.Spec `json:"spec"`
	ForceUpdate bool           `json:"force_update"`
}

type UpdateDatabaseOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) UpdateDatabase(ctx workflow.Context, input *UpdateDatabaseInput) (*UpdateDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("updating database")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to update database")

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

		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID:    input.Spec.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}
	if input.Spec.HasZodanTargetNode() && input.Spec.RestoreConfig == nil {
		logger.Info("zodan enabled: routing to zodan add node workflow")
		zodanOutput, err := w.ZodanAddNode(ctx, &ZodanAddNodeInput{
			TaskID: input.TaskID,
			Spec:   input.Spec,
		})
		if err != nil {
			return nil, handleError(err)
		}
		return &UpdateDatabaseOutput{Updated: zodanOutput.Updated}, nil
	}

	refreshCurrentInput := &RefreshCurrentStateInput{
		DatabaseID: input.Spec.DatabaseID,
		TaskID:     input.TaskID,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
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
		TaskID:      input.TaskID,
		Current:     refreshCurrentOutput.State,
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

	updateTaskInput = &activities.UpdateTaskInput{
		DatabaseID:    input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully updated database")

	return &UpdateDatabaseOutput{
		Updated: reconcileOutput.Updated,
	}, nil
}
