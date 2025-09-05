package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetRestoreResourcesInput struct {
	Spec          *database.InstanceSpec  `json:"spec"`
	TaskID        uuid.UUID               `json:"task_id"`
	RestoreConfig *database.RestoreConfig `json:"restore_config"`
}

type GetRestoreResourcesOutput struct {
	Resources        *database.InstanceResources `json:"resources"`
	RestoreResources *database.InstanceResources `json:"restore_resources"`
}

func (a *Activities) ExecuteGetRestoreResources(
	ctx workflow.Context,
	input *GetRestoreResourcesInput,
) workflow.Future[*GetRestoreResourcesOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(input.Spec.HostID),
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

	resources, err := a.Orchestrator.GenerateInstanceResources(input.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	restoreSpec := input.Spec.Clone()
	restoreSpec.RestoreConfig = input.RestoreConfig
	restoreResources, err := a.Orchestrator.GenerateInstanceRestoreResources(restoreSpec, input.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate restore resources: %w", err)
	}

	return &GetRestoreResourcesOutput{
		Resources:        resources,
		RestoreResources: restoreResources,
	}, nil
}
