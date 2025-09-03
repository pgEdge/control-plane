package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type GetRestoreStateInput struct {
	Spec        *database.Spec       `json:"spec"`
	NodeTaskIDs map[string]uuid.UUID `json:"node_task_ids"`
}

type GetRestoreStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteGetRestoreState(
	ctx workflow.Context,
	input *GetRestoreStateInput,
) workflow.Future[*GetRestoreStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*GetRestoreStateOutput](ctx, options, w.GetRestoreState, input)
}

func (w *Workflows) GetRestoreState(ctx workflow.Context, input *GetRestoreStateInput) (*GetRestoreStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("getting restore state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	state := resource.NewState()

	var instanceFutures []workflow.Future[*activities.GetRestoreResourcesOutput]
	for i, nodeInstance := range nodeInstances {
		var instanceIDs []string
		// Nil task ID is handled in the activity
		taskID := input.NodeTaskIDs[nodeInstance.NodeName]
		for _, instance := range nodeInstance.Instances {
			instanceIDs = append(instanceIDs, instance.InstanceID)
			instanceFuture := w.Activities.ExecuteGetRestoreResources(ctx, &activities.GetRestoreResourcesInput{
				Spec:   instance,
				TaskID: taskID,
			})
			instanceFutures = append(instanceFutures, instanceFuture)
		}
		err = state.AddResource(&database.NodeResource{
			ClusterID:   w.Config.ClusterID,
			Name:        nodeInstance.NodeName,
			InstanceIDs: instanceIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node resource to state: %w", err)
		}
		for j, peer := range nodeInstances {
			if i == j {
				continue
			}
			err = state.AddResource(database.NewSubscriptionResource(nodeInstance, peer, true, false, false))
			if err != nil {
				return nil, fmt.Errorf("failed to add subscription resource to state: %w", err)
			}
		}
	}

	for _, instanceFuture := range instanceFutures {
		instanceOutput, err := instanceFuture.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get instance resources: %w", err)
		}
		err = state.AddResource(instanceOutput.Resources.Instance)
		if err != nil {
			return nil, fmt.Errorf("failed to add instance resource to state: %w", err)
		}
		for _, resource := range instanceOutput.Resources.Resources {
			state.Add(resource)
		}
	}

	logger.Info("successfully got restore state")

	return &GetRestoreStateOutput{
		State: state,
	}, nil
}
