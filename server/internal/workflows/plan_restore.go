package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type PlanRestoreInput struct {
	Spec          *database.Spec          `json:"spec"`
	Current       *resource.State         `json:"current"`
	RestoreConfig *database.RestoreConfig `json:"restore_config"`
	NodeTaskIDs   map[string]uuid.UUID    `json:"node_tasks_ids"`
}

type PlanRestoreOutput struct {
	Plans []resource.Plan `json:"plans"`
}

func (w *Workflows) ExecutePlanRestore(
	ctx workflow.Context,
	input *PlanRestoreInput,
) workflow.Future[*PlanRestoreOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: utils.HostQueue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*PlanRestoreOutput](ctx, options, w.PlanRestore, input)
}

func (w *Workflows) PlanRestore(ctx workflow.Context, input *PlanRestoreInput) (*PlanRestoreOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("getting desired state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	var nodeResources []*operations.NodeResources
	var restoreResources []*operations.NodeRestoreResources

	for _, node := range nodeInstances {
		taskID, beingRestored := input.NodeTaskIDs[node.NodeName]
		if !beingRestored {
			resources, err := w.getNodeResources(ctx, node)
			if err != nil {
				return nil, err
			}
			nodeResources = append(nodeResources, resources)
		} else {
			resources, err := w.getRestoreResources(ctx, input.Current, node, taskID, input.RestoreConfig)
			if err != nil {
				return nil, err
			}
			restoreResources = append(restoreResources, resources)
		}
	}

	plans, err := operations.RestoreDatabase(input.Current, nodeResources, restoreResources)
	if err != nil {
		return nil, fmt.Errorf("failed to plan database restore: %w", err)
	}

	logger.Info("successfully planned database restore")

	return &PlanRestoreOutput{Plans: plans}, nil
}

func (w *Workflows) getRestoreResources(
	ctx workflow.Context,
	state *resource.State,
	node *database.NodeInstances,
	taskID uuid.UUID,
	restoreConfig *database.RestoreConfig,
) (*operations.NodeRestoreResources, error) {
	primaryInstanceID, err := w.getOrPickPrimary(state, node)
	if err != nil {
		return nil, err
	}

	nodeRestore := &operations.NodeRestoreResources{
		NodeName: node.NodeName,
	}
	for _, instance := range node.Instances {
		if instance.InstanceID == primaryInstanceID {
			in := &activities.GetRestoreResourcesInput{
				Spec:          instance,
				TaskID:        taskID,
				RestoreConfig: restoreConfig,
			}
			out, err := w.Activities.
				ExecuteGetRestoreResources(ctx, in).
				Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get restore resources: %w", err)
			}
			nodeRestore.PrimaryInstance = out.Resources
			nodeRestore.RestoreInstance = out.RestoreResources
		} else {
			in := &activities.GetInstanceResourcesInput{
				Spec: instance,
			}
			out, err := w.Activities.
				ExecuteGetInstanceResources(ctx, in).
				Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get instance resources: %w", err)
			}
			nodeRestore.ReplicaInstances = append(nodeRestore.ReplicaInstances, out.Resources)
		}
	}

	return nodeRestore, nil
}

func (w *Workflows) getOrPickPrimary(
	state *resource.State,
	node *database.NodeInstances,
) (string, error) {
	instanceIDs := node.InstanceIDs()
	if len(instanceIDs) == 0 {
		return "", fmt.Errorf("node %s has no instances in spec", node.NodeName)
	}

	var primaryInstanceID string

	current, err := resource.FromState[*database.NodeResource](state, database.NodeResourceIdentifier(node.NodeName))
	switch {
	case errors.Is(err, resource.ErrNotFound):
		// This can happen if the restore process failed before recreating the
		// node. In this case, we'll first try to find the ID of the first
		// existing instance since that's most likely our primary.
		for _, id := range instanceIDs {
			_, ok := state.Get(database.InstanceResourceIdentifier(id))
			if ok {
				primaryInstanceID = id
				break
			}
		}
		if primaryInstanceID == "" {
			// If there are no existing instances, then it's impossible to know what
			// the last primary was. In this case we'll just have to pick one.
			primaryInstanceID = instanceIDs[0]
		}
	case err != nil:
		return "", fmt.Errorf("failed to get node from current state: %w", err)
	case current.PrimaryInstanceID == "":
		// The node exists, but we were unable to elect a primary. We'll just
		// pick the first existing instance ID.
		if len(current.InstanceIDs) == 0 {
			return "", fmt.Errorf("invalid state: node %s has no instances state", node.NodeName)
		}
		primaryInstanceID = current.InstanceIDs[0]
	default:
		primaryInstanceID = current.PrimaryInstanceID
	}

	return primaryInstanceID, nil
}
