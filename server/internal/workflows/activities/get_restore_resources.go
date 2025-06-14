package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type GetRestoreResourcesInput struct {
	Spec   *database.InstanceSpec `json:"spec"`
	TaskID uuid.UUID              `json:"task_id"`
}

type GetRestoreResourcesOutput struct {
	Resources *database.InstanceResources
}

func (a *Activities) ExecuteGetRestoreResources(
	ctx workflow.Context,
	input *GetRestoreResourcesInput,
) workflow.Future[*GetRestoreResourcesOutput] {
	executor := input.Spec.HostID
	options := workflow.ActivityOptions{
		Queue: core.Queue(executor),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetRestoreResourcesOutput](ctx, options, a.GetRestoreResources, input)
}

func (a *Activities) GetRestoreResources(ctx context.Context, input *GetRestoreResourcesInput) (*GetRestoreResourcesOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.Spec.DatabaseID,
		"instance_id", input.Spec.InstanceID,
	)
	logger.Info("getting restore resources")

	var resources *database.InstanceResources
	var err error
	if input.TaskID == uuid.Nil {
		resources, err = a.Orchestrator.GenerateInstanceResources(input.Spec)
	} else {
		resources, err = a.Orchestrator.GenerateInstanceRestoreResources(input.Spec, input.TaskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	return &GetRestoreResourcesOutput{
		Resources: resources,
	}, nil
}
