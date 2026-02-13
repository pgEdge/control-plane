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

type GetServiceInstanceStatusInput struct {
	ServiceInstanceID string `json:"service_instance_id"`
	HostID            string `json:"host_id"`
}

type GetServiceInstanceStatusOutput struct {
	Status *database.ServiceInstanceStatus `json:"status"`
}

func (a *Activities) ExecuteGetServiceInstanceStatus(
	ctx workflow.Context,
	hostID string,
	input *GetServiceInstanceStatusInput,
) workflow.Future[*GetServiceInstanceStatusOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 3,
		},
	}
	return workflow.ExecuteActivity[*GetServiceInstanceStatusOutput](ctx, options, a.GetServiceInstanceStatus, input)
}

func (a *Activities) GetServiceInstanceStatus(
	ctx context.Context,
	input *GetServiceInstanceStatusInput,
) (*GetServiceInstanceStatusOutput, error) {
	logger := activity.Logger(ctx).With("service_instance_id", input.ServiceInstanceID)
	logger.Debug("getting service instance status")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	status, err := orch.GetServiceInstanceStatus(ctx, input.ServiceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instance status: %w", err)
	}

	return &GetServiceInstanceStatusOutput{Status: status}, nil
}
