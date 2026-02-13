package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type UpdateServiceInstanceStateInput struct {
	ServiceInstanceID string                          `json:"service_instance_id"`
	DatabaseID        string                          `json:"database_id,omitempty"`
	State             database.ServiceInstanceState   `json:"state"`
	Status            *database.ServiceInstanceStatus `json:"status,omitempty"`
	Error             string                          `json:"error,omitempty"`
}

type UpdateServiceInstanceStateOutput struct{}

func (a *Activities) ExecuteUpdateServiceInstanceState(
	ctx workflow.Context,
	input *UpdateServiceInstanceStateInput,
) workflow.Future[*UpdateServiceInstanceStateOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.ManagerQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateServiceInstanceStateOutput](ctx, options, a.UpdateServiceInstanceState, input)
}

func (a *Activities) UpdateServiceInstanceState(
	ctx context.Context,
	input *UpdateServiceInstanceStateInput,
) (*UpdateServiceInstanceStateOutput, error) {
	logger := activity.Logger(ctx).With(
		"service_instance_id", input.ServiceInstanceID,
		"state", input.State,
	)
	logger.Debug("updating service instance state")

	err := a.DatabaseService.UpdateServiceInstanceState(ctx, input.ServiceInstanceID, &database.ServiceInstanceStateUpdate{
		DatabaseID: input.DatabaseID,
		State:      input.State,
		Status:     input.Status,
		Error:      input.Error,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update service instance state: %w", err)
	}

	logger.Debug("successfully updated service instance state")

	return &UpdateServiceInstanceStateOutput{}, nil
}
