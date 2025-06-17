package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type GetInstanceResourcesInput struct {
	Spec *database.InstanceSpec
}

type GetInstanceResourcesOutput struct {
	Resources *database.InstanceResources
}

func (a *Activities) ExecuteGetInstanceResources(
	ctx workflow.Context,
	input *GetInstanceResourcesInput,
) workflow.Future[*GetInstanceResourcesOutput] {
	executor := input.Spec.HostID
	options := workflow.ActivityOptions{
		Queue: core.Queue(executor),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetInstanceResourcesOutput](ctx, options, a.GetInstanceResources, input)
}

func (a *Activities) GetInstanceResources(ctx context.Context, input *GetInstanceResourcesInput) (*GetInstanceResourcesOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.Spec.DatabaseID,
		"instance_id", input.Spec.InstanceID,
	)
	logger.Info("getting instance resources")

	resources, err := a.Orchestrator.GenerateInstanceResources(input.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	return &GetInstanceResourcesOutput{
		Resources: resources,
	}, nil
}
