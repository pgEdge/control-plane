package workflows

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
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
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID.String())
	logger.Info("restoring database from pgbackrest backup")

	// This is an 'in-place' restore, meaning that the data directory is left
	// intact. We always want to include the 'delta' option for this type of
	// restore.
	if !slices.Contains(input.RestoreConfig.RestoreOptions, "--delta") {
		input.RestoreConfig.RestoreOptions = append(
			input.RestoreConfig.RestoreOptions,
			"--delta",
		)
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

	restoreSpecInput := &activities.RestoreSpecInput{
		State:         current,
		Spec:          input.Spec,
		TargetNodes:   input.TargetNodes,
		RestoreConfig: input.RestoreConfig,
	}
	restoreSpecOutput, err := w.Activities.
		ExecuteRestoreSpec(ctx, restoreSpecInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to compute restore spec: %w", err))
	}

	// Now we'll compute each state that we'll transition to
	// 1. Pre-restore state. Here we'll remove the read replicas as well as the
	//    Node and Instance resources for the target nodes.
	preRestoreDesiredInput := &GetPreRestoreStateInput{
		Spec:        restoreSpecOutput.Spec,
		NodeTaskIDs: input.NodeTaskIDs,
	}
	preRestoreDesiredOutput, err := w.ExecuteGetPreRestoreState(ctx, preRestoreDesiredInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get pre-restore state: %w", err))
	}
	preRestoreDesired := preRestoreDesiredOutput.State

	// 2. The restore state. This creates restore-specific resources for the
	//    target nodes and recreates the Node, Instance, and Subscription
	//    resources.
	restoreDesiredInput := &GetRestoreStateInput{
		Spec:        restoreSpecOutput.Spec,
		NodeTaskIDs: input.NodeTaskIDs,
	}
	restoreDesiredOutput, err := w.ExecuteGetRestoreState(ctx, restoreDesiredInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get restore state: %w", err))
	}
	restoreDesired := restoreDesiredOutput.State

	// 3. Post-restore state. After the restore is complete, we'll remove the
	//    restore-specific resources and recreate the read replicas.
	postRestoreDesiredInput := &GetDesiredStateInput{
		Spec: input.Spec,
	}
	postRestoreDesiredOutput, err := w.ExecuteGetDesiredState(ctx, postRestoreDesiredInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get post-restore state: %w", err))
	}
	postRestoreDesired := postRestoreDesiredOutput.State

	// Now we'll loop through the states and reconcile them
	desiredStates := []*resource.State{
		preRestoreDesired,
		restoreDesired,
		postRestoreDesired,
	}
	for _, desired := range desiredStates {
		reconcileInput := &ReconcileStateInput{
			DatabaseID: input.Spec.DatabaseID,
			TaskID:     input.TaskID,
			Current:    current,
			Desired:    desired,
		}
		reconcileOutput, err := w.ExecuteReconcileState(ctx, reconcileInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to reconcile state: %w", err))
		}
		current = reconcileOutput.Updated
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
