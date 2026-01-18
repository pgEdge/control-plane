package workflows

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RemoveHostInput struct {
	HostID               string                 `json:"host_id"`
	UpdateDatabaseInputs []*UpdateDatabaseInput `json:"update_database_inputs,omitempty"`
	TaskID               uuid.UUID              `json:"task_id"`
}

type RemoveHostOutput struct{}

func (w *Workflows) RemoveHost(ctx workflow.Context, input *RemoveHostInput) (*RemoveHostOutput, error) {
	logger := workflow.Logger(ctx).With(
		"host_id", input.HostID,
	)
	logger.Info("removing host")

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			w.cancelTask(cleanupCtx, task.ScopeHost, input.HostID, input.TaskID, logger)
		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to remove host")

		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeHost,
			EntityID:      input.HostID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)

		return cause
	}

	// Curry w.logTaskEvent and treat logging errors as non-fatal
	logTaskEvent := func(entry task.LogEntry) {
		err := w.logTaskEvent(ctx,
			task.ScopeHost,
			input.HostID,
			input.TaskID,
			entry,
		)
		if err != nil {
			// These log messages are not critical to this process, so it's safe
			// to treat this error as non-fatal.
			logger.With("error", err).Error("failed to log task event")
		}
	}

	updateTaskInput := &activities.UpdateTaskInput{
		Scope:         task.ScopeHost,
		EntityID:      input.HostID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	if len(input.UpdateDatabaseInputs) > 0 {
		err := w.removeHostFromDatabases(ctx, logger, logTaskEvent, input.UpdateDatabaseInputs)
		if err != nil {
			return nil, handleError(err)
		}
	}

	logTaskEvent(task.LogEntry{
		Message: fmt.Sprintf("removing host '%s'", input.HostID),
	})

	req := activities.RemoveHostInput{
		HostID: input.HostID,
	}
	_, err := w.Activities.ExecuteRemoveHost(ctx, &req).Get(ctx)
	if err != nil {
		return nil, handleError(err)
	}

	// Needs to come before the UpdateTaskInput or else clients will see the
	// task complete before the log entry is added.
	logTaskEvent(task.LogEntry{
		Message: fmt.Sprintf("successfully removed host '%s'", input.HostID),
	})

	updateTaskInput = &activities.UpdateTaskInput{
		Scope:         task.ScopeHost,
		EntityID:      input.HostID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully removed host")

	return &RemoveHostOutput{}, nil
}

func (w *Workflows) removeHostFromDatabases(ctx workflow.Context, logger *slog.Logger, logTaskEvent func(task.LogEntry), inputs []*UpdateDatabaseInput) error {
	logger.Info("starting database update workflows", "count", len(inputs))

	futures := make([]workflow.Future[*UpdateDatabaseOutput], len(inputs))
	for i, dbInput := range inputs {
		if dbInput.Spec == nil {
			return fmt.Errorf("update database input at index %d has nil spec", i)
		}
		logger.Info("creating update database sub-workflow", "database_id", dbInput.Spec.DatabaseID)

		logTaskEvent(task.LogEntry{
			Message: fmt.Sprintf("removing host from database '%s'", dbInput.Spec.DatabaseID),
		})

		futures[i] = workflow.CreateSubWorkflowInstance[*UpdateDatabaseOutput](
			ctx,
			workflow.SubWorkflowOptions{},
			w.UpdateDatabase,
			dbInput,
		)
	}

	for i, future := range futures {
		_, err := future.Get(ctx)
		if err != nil {
			dbID := inputs[i].Spec.DatabaseID

			logTaskEvent(task.LogEntry{
				Message: fmt.Sprintf("failed to update database '%s'", inputs[i].Spec.DatabaseID),
				Fields: map[string]any{
					"error": err.Error(),
				},
			})

			logger.With("error", err, "database_id", dbID).Error("database update sub-workflow failed")
			return err
		}

		logTaskEvent(task.LogEntry{
			Message: fmt.Sprintf("successfully removing host from database '%s'", inputs[i].Spec.DatabaseID),
		})
	}

	logger.Info("all database update workflows completed successfully")

	return nil
}
