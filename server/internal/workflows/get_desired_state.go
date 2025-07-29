package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type GetDesiredStateInput struct {
	Spec *database.Spec
}

type GetDesiredStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteGetDesiredState(
	ctx workflow.Context,
	input *GetDesiredStateInput,
) workflow.Future[*GetDesiredStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*GetDesiredStateOutput](ctx, options, w.GetDesiredState, input)
}

func (w *Workflows) GetDesiredState(ctx workflow.Context, input *GetDesiredStateInput) (*GetDesiredStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("getting desired state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	state := resource.NewState()

	var instanceFutures []workflow.Future[*activities.GetInstanceResourcesOutput]
	for i, nodeInstance := range nodeInstances {
		var instanceIDs []string
		for _, instance := range nodeInstance.Instances {
			instanceIDs = append(instanceIDs, instance.InstanceID)
			instanceFuture := w.Activities.ExecuteGetInstanceResources(ctx, &activities.GetInstanceResourcesInput{
				Spec: instance,
			})
			instanceFutures = append(instanceFutures, instanceFuture)
			err = state.AddResource(&monitor.InstanceMonitorResource{
				DatabaseID:   instance.DatabaseID,
				InstanceID:   instance.InstanceID,
				HostID:       instance.HostID,
				DatabaseName: instance.DatabaseName,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to add instance monitor resource to state: %w", err)
			}
		}
		err = state.AddResource(&database.NodeResource{
			ClusterID:   w.Config.ClusterID,
			Name:        nodeInstance.NodeName,
			InstanceIDs: instanceIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node resource to state: %w", err)
		}

		isZodan := false
		for _, inst := range nodeInstance.Instances {
			if inst.ZodanEnabled {
				isZodan = true
				break
			}
		}

		for j, peer := range nodeInstances {
			if i == j {
				continue
			}

			if isZodan {
				var providerInstanceIDs []string
				for _, pInst := range peer.Instances {
					providerInstanceIDs = append(providerInstanceIDs, pInst.InstanceID)
				}
				sub := &database.SubscriptionResource{
					Spec:              input.Spec,
					SubscriberNode:    nodeInstance.NodeName,
					ProviderNode:      peer.NodeName,
					ProviderInstances: providerInstanceIDs,
					Zodan:             true,
				}
				err = state.AddResource(sub)
				if err != nil {
					return nil, fmt.Errorf("failed to add zodan subscription resource: %w", err)
				}
			} else {
				err = state.AddResource(database.NewSubscriptionResource(nodeInstance, peer))
				if err != nil {
					return nil, fmt.Errorf("failed to add subscription resource to state: %w", err)
				}
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

	logger.Info("successfully got desired state")

	return &GetDesiredStateOutput{
		State: state,
	}, nil
}
