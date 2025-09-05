package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
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
	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			updateStateInput := &activities.UpdateDbStateInput{
				DatabaseID: input.Spec.DatabaseID,
				State:      database.DatabaseStateFailed,
			}

			_, err := w.Activities.ExecuteUpdateDbState(cleanupCtx, updateStateInput).Get(cleanupCtx)
			if err != nil {
				logger.With("error", err).Error("failed to update database state ")
			}

			w.cancelTask(cleanupCtx, input.Spec.DatabaseID, input.TaskID, logger)

		}
	}()

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

	refreshCurrentInput := &RefreshCurrentStateInput{
		DatabaseID: input.Spec.DatabaseID,
		TaskID:     input.TaskID,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	planInput := &PlanUpdateInput{
		Spec:    input.Spec,
		Current: current,
		Options: operations.UpdateDatabaseOptions{
			PlanOptions: resource.PlanOptions{
				ForceUpdate: input.ForceUpdate,
			},
		},
	}
	planOutput, err := w.ExecutePlanUpdate(ctx, planInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to execute plan update: %w", err))
	}

	err = w.applyPlans(ctx, input.Spec.DatabaseID, input.TaskID, current, planOutput.Plans)
	if err != nil {
		return nil, handleError(err)
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
		Updated: current,
	}, nil
}
