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

type GetPreRestoreStateInput struct {
	Spec        *database.Spec       `json:"spec"`
	NodeTaskIDs map[string]uuid.UUID `json:"node_task_ids"`
}

type GetPreRestoreStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteGetPreRestoreState(
	ctx workflow.Context,
	input *GetPreRestoreStateInput,
) workflow.Future[*GetPreRestoreStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*GetPreRestoreStateOutput](ctx, options, w.GetPreRestoreState, input)
}

func (w *Workflows) GetPreRestoreState(ctx workflow.Context, input *GetPreRestoreStateInput) (*GetPreRestoreStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID.String())
	logger.Info("getting pre-restore state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	state := resource.NewState()

	for i, nodeInstance := range nodeInstances {
		var instanceIDs []uuid.UUID
		_, toBeRestored := input.NodeTaskIDs[nodeInstance.NodeName]
		for _, instance := range nodeInstance.Instances {
			instanceIDs = append(instanceIDs, instance.InstanceID)
			out, err := w.Activities.ExecuteGetInstanceResources(ctx, &activities.GetInstanceResourcesInput{
				Spec: instance,
			}).Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get restore resources: %w", err)
			}
			for _, resource := range out.Resources.Resources {
				state.Add(resource)
			}
			// Skip adding the instance resource if this instance is going to
			// be restored.
			if toBeRestored {
				continue
			}
			err = state.AddResource(out.Resources.Instance)
			if err != nil {
				return nil, fmt.Errorf("failed to add instance resource to state: %w", err)
			}
		}
		// Skip adding the node resource and subscriptions if the node is going
		// to be restored.
		if toBeRestored {
			continue
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
			// Skip adding the subscription if the peer node is going to be
			// restored.
			_, peerToBeRestored := input.NodeTaskIDs[peer.NodeName]
			if i == j || peerToBeRestored {
				continue
			}
			err = state.AddResource(&database.SubscriptionResource{
				SubscriberNode: nodeInstance.NodeName,
				ProviderNode:   peer.NodeName,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to add subscription resource to state: %w", err)
			}
		}
	}

	logger.Info("successfully got pre-restore state")

	return &GetPreRestoreStateOutput{
		State: state,
	}, nil
}
