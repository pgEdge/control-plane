package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type StoreServiceInstanceInput struct {
	ServiceInstance *database.ServiceInstance `json:"service_instance"`
}

type StoreServiceInstanceOutput struct{}

func (a *Activities) ExecuteStoreServiceInstance(
	ctx workflow.Context,
	input *StoreServiceInstanceInput,
) workflow.Future[*StoreServiceInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.ManagerQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*StoreServiceInstanceOutput](ctx, options, a.StoreServiceInstance, input)
}

func (a *Activities) StoreServiceInstance(
	ctx context.Context,
	input *StoreServiceInstanceInput,
) (*StoreServiceInstanceOutput, error) {
	logger := activity.Logger(ctx).With(
		"service_instance_id", input.ServiceInstance.ServiceInstanceID,
		"database_id", input.ServiceInstance.DatabaseID,
	)
	logger.Debug("storing service instance")

	err := a.DatabaseService.UpdateServiceInstance(ctx, &database.ServiceInstanceUpdateOptions{
		ServiceInstanceID: input.ServiceInstance.ServiceInstanceID,
		ServiceID:         input.ServiceInstance.ServiceID,
		DatabaseID:        input.ServiceInstance.DatabaseID,
		HostID:            input.ServiceInstance.HostID,
		State:             input.ServiceInstance.State,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store service instance: %w", err)
	}

	logger.Debug("successfully stored service instance")

	return &StoreServiceInstanceOutput{}, nil
}
