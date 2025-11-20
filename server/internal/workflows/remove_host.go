package workflows

import (
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RemoveHostInput struct {
	HostID               string                 `json:"host_id"`
	UpdateDatabaseInputs []*UpdateDatabaseInput `json:"update_database_inputs,omitempty"`
	DatabaseTaskIDs      map[string]uuid.UUID   `json:"database_task_ids,omitempty"`
	TaskID               uuid.UUID              `json:"task_id"`
}

type RemoveHostOutput struct{}

func (w *Workflows) RemoveHost(ctx workflow.Context, input *RemoveHostInput) (*RemoveHostOutput, error) {
	logger := workflow.Logger(ctx).With(
		"host_id", input.HostID,
		"task_id", input.TaskID.String(),
	)
	logger.Info("removing host")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to remove host")

		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID:    input.HostID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
			logger.With("error", err).Error("failed to update task after remove host failure")
		}

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.HostID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
		return nil, handleError(err)
	}

	if len(input.UpdateDatabaseInputs) > 0 {
		logger.Info("starting database update workflows", "count", len(input.UpdateDatabaseInputs))

		futures := make([]workflow.Future[*UpdateDatabaseOutput], len(input.UpdateDatabaseInputs))
		for i, dbInput := range input.UpdateDatabaseInputs {
			logger.Info("creating update database sub-workflow", "database_id", dbInput.Spec.DatabaseID)
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
				dbID := input.UpdateDatabaseInputs[i].Spec.DatabaseID
				logger.With("error", err, "database_id", dbID).Error("database update sub-workflow failed")
				return nil, handleError(err)
			}
		}

		logger.Info("all database update workflows completed successfully")
	}

	req := activities.RemoveHostInput{
		HostID: input.HostID,
		TaskID: input.TaskID,
	}
	_, err := w.Activities.ExecuteRemoveHost(ctx, &req).Get(ctx)
	if err != nil {
		return nil, handleError(err)
	}

	updateTaskInput = &activities.UpdateTaskInput{
		DatabaseID:    input.HostID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully removed host")
	return &RemoveHostOutput{}, nil
}
