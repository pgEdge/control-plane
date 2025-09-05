package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type PlanUpdateInput struct {
	Options operations.UpdateDatabaseOptions `json:"options"`
	Spec    *database.Spec                   `json:"spec"`
	Current *resource.State                  `json:"current"`
}

type PlanUpdateOutput struct {
	Plans []resource.Plan `json:"plans"`
}

func (w *Workflows) ExecutePlanUpdate(
	ctx workflow.Context,
	input *PlanUpdateInput,
) workflow.Future[*PlanUpdateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: utils.HostQueue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*PlanUpdateOutput](ctx, options, w.PlanUpdate, input)
}

func (w *Workflows) PlanUpdate(ctx workflow.Context, input *PlanUpdateInput) (*PlanUpdateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("getting desired state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	nodeResources := make([]*operations.NodeResources, len(nodeInstances))
	for i, node := range nodeInstances {
		resources, err := w.getNodeResources(ctx, node)
		if err != nil {
			return nil, err
		}

		nodeResources[i] = resources
	}

	plans, err := operations.UpdateDatabase(input.Options, input.Current, nodeResources)
	if err != nil {
		return nil, fmt.Errorf("failed to plan database update: %w", err)
	}

	logger.Info("successfully planned database update")

	return &PlanUpdateOutput{Plans: plans}, nil
}
