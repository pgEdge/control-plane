package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GenerateServiceInstanceResourcesInput struct {
	Spec *database.ServiceInstanceSpec `json:"spec"`
}

type GenerateServiceInstanceResourcesOutput struct {
	Resources *database.ServiceInstanceResources `json:"resources"`
}

func (a *Activities) ExecuteGenerateServiceInstanceResources(
	ctx workflow.Context,
	input *GenerateServiceInstanceResourcesInput,
) workflow.Future[*GenerateServiceInstanceResourcesOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.ManagerQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GenerateServiceInstanceResourcesOutput](ctx, options, a.GenerateServiceInstanceResources, input)
}

func (a *Activities) GenerateServiceInstanceResources(
	ctx context.Context,
	input *GenerateServiceInstanceResourcesInput,
) (*GenerateServiceInstanceResourcesOutput, error) {
	logger := activity.Logger(ctx).With(
		"service_instance_id", input.Spec.ServiceInstanceID,
		"database_id", input.Spec.DatabaseID,
	)
	logger.Debug("generating service instance resources")

	resources, err := a.Orchestrator.GenerateServiceInstanceResources(input.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate service instance resources: %w", err)
	}

	return &GenerateServiceInstanceResourcesOutput{
		Resources: resources,
	}, nil
}
