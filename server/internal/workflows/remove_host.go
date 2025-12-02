package workflows

import (
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
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
	)
	logger.Info("removing host")

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
				return nil, err
			}
		}

		logger.Info("all database update workflows completed successfully")
	}

	req := activities.RemoveHostInput{
		HostID: input.HostID,
	}
	_, err := w.Activities.ExecuteRemoveHost(ctx, &req).Get(ctx)
	if err != nil {
		return nil, err
	}

	logger.Info("successfully removed host")
	return &RemoveHostOutput{}, nil
}
