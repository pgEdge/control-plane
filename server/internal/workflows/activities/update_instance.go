package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type UpdateInstanceInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
	State      string `json:"state"`
}

func (a *Activities) ExecuteUpdateInstance(
	ctx workflow.Context,
	input *UpdateInstanceInput,
) workflow.Future[struct{}] {
	options := workflow.ActivityOptions{
		Queue:        core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{MaxAttempts: 1},
	}
	return workflow.ExecuteActivity[struct{}](ctx, options, a.UpdateInstance, input)
}

func (a *Activities) UpdateInstance(ctx context.Context, input *UpdateInstanceInput) (struct{}, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)
	logger.Info("updating instance state")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return struct{}{}, fmt.Errorf("failed to get database service: %w", err)
	}

	err = dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		State:      database.InstanceState(input.State),
	})
	if err != nil {
		return struct{}{}, fmt.Errorf("failed to update instance: %w", err)
	}

	logger.Info("instance state updated successfully")
	return struct{}{}, nil
}
