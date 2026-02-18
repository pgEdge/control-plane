package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type CleanupInstanceInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
}

type CleanupInstanceOutput struct{}

// ExecuteCleanupInstance queues the CleanupInstance activity on the manager.
func (a *Activities) ExecuteCleanupInstance(
	ctx workflow.Context,
	input *CleanupInstanceInput,
) workflow.Future[*CleanupInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CleanupInstanceOutput](ctx, options, a.CleanupInstance, input)
}

// CleanupInstance deletes an orphaned instance record from etcd.
// This is called when a host is removed and instance Delete() cannot run.
func (a *Activities) CleanupInstance(ctx context.Context, input *CleanupInstanceInput) (*CleanupInstanceOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
	)
	logger.Info("cleaning up orphaned instance record")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = dbSvc.DeleteInstance(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to cleanup instance: %w", err)
	}

	logger.Info("successfully cleaned up orphaned instance record")
	return &CleanupInstanceOutput{}, nil
}
