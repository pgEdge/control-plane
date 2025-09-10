package workflows

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type PgBackRestRestoreInput struct {
	TaskID        uuid.UUID               `json:"task_id"`
	Spec          *database.Spec          `json:"spec"`
	TargetNodes   []string                `json:"target_nodes"`
	RestoreConfig *database.RestoreConfig `json:"restore_config"`
	NodeTaskIDs   map[string]uuid.UUID    `json:"node_tasks_ids"`
}

type PgBackRestRestoreOutput struct{}

func (w *Workflows) PgBackRestRestore(ctx workflow.Context, input *PgBackRestRestoreInput) (*PgBackRestRestoreOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("restoring database from pgbackrest backup")

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)
			w.cancelTask(cleanupCtx, input.Spec.DatabaseID, input.TaskID, logger)
		}
	}()

	// This is an 'in-place' restore, meaning that the data directory is left
	// intact. We always want to include the 'delta' option for this type of
	// restore.
	if input.RestoreConfig.RestoreOptions == nil {
		input.RestoreConfig.RestoreOptions = make(map[string]string)
	}
	if _, ok := input.RestoreConfig.RestoreOptions["delta"]; !ok {
		input.RestoreConfig.RestoreOptions["delta"] = ""
	}

	handleError := func(err error) error {
		return w.handlePgBackRestRestoreFailed(ctx, logger, input, err)
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
	refreshCurrentOutput, err := w.
		ExecuteRefreshCurrentState(ctx, refreshCurrentInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	planInput := &PlanRestoreInput{
		Spec:          input.Spec,
		Current:       current,
		RestoreConfig: input.RestoreConfig,
		NodeTaskIDs:   input.NodeTaskIDs,
	}
	planOutput, err := w.ExecutePlanRestore(ctx, planInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to execute plan restore: %w", err))
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

	logger.Info("successfully restored database")

	return &PgBackRestRestoreOutput{}, nil
}

func (w *Workflows) handlePgBackRestRestoreFailed(
	ctx workflow.Context,
	logger *slog.Logger,
	input *PgBackRestRestoreInput,
	cause error,
) error {
	logger.With("error", cause).Error("failed to restore database")
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
	for _, taskID := range input.NodeTaskIDs {
		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID: input.Spec.DatabaseID,
			TaskID:     taskID,
			UpdateOptions: task.UpdateFail(
				fmt.Errorf("parent task failed: %w", cause),
			),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)
	}

	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateFail(cause),
	}
	_ = w.updateTask(ctx, logger, updateTaskInput)

	return cause
}
