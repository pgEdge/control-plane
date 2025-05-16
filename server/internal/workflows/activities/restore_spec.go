package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

type InstanceHost struct {
	InstanceID uuid.UUID `json:"instance_id"`
	HostID     uuid.UUID `json:"host_id"`
}

type RestoreSpecInput struct {
	State         *resource.State         `json:"state"`
	Spec          *database.Spec          `json:"spec"`
	TargetNodes   []string                `json:"target_nodes"`
	RestoreConfig *database.RestoreConfig `json:"restore_config"`
}

type RestoreSpecOutput struct {
	Spec      *database.Spec           `json:"spec"`
	Primaries map[string]*InstanceHost `json:"primaries"`
}

func (a *Activities) ExecuteRestoreSpec(
	ctx workflow.Context,
	input *RestoreSpecInput,
) workflow.Future[*RestoreSpecOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*RestoreSpecOutput](ctx, options, a.RestoreSpec, input)
}

func (a *Activities) RestoreSpec(ctx context.Context, input *RestoreSpecInput) (*RestoreSpecOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.Spec.DatabaseID.String())
	logger.Info("computing restore spec")

	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return nil, err
	}

	// The clone is unnecessary in normal use as an activity, but we want to
	// prevent any surprises if this is called as a normal function.
	spec := input.Spec.Clone()
	rc := &resource.Context{
		State:    input.State,
		Registry: registry,
		Injector: a.Injector,
	}

	if spec.BackupConfig != nil {
		// Move the backup config from the database spec onto each node so that
		// we can nil it out on every node that's being restored.
		for i := range spec.Nodes {
			if spec.Nodes[i].BackupConfig == nil {
				spec.Nodes[i].BackupConfig = spec.BackupConfig
			}
		}
		spec.BackupConfig = nil
	}

	primaries := map[string]*InstanceHost{}
	for _, nodeName := range input.TargetNodes {
		node, err := spec.Node(nodeName)
		if err != nil {
			return nil, fmt.Errorf("failed to get node %s from spec: %w", nodeName, err)
		}
		primary, err := database.GetPrimaryInstance(ctx, rc, nodeName)
		if errors.Is(err, resource.ErrNotFound) {
			// ErrNotFound is expected if we previously failed to restore the
			// database and the node is not in the current state. In this case,
			// we'll just pick the first host ID from the to be the primary.
			if len(node.HostIDs) < 1 {
				return nil, fmt.Errorf("node %s has no host IDs", nodeName)
			}
			node.HostIDs = []uuid.UUID{node.HostIDs[0]}
			node.RestoreConfig = input.RestoreConfig
			node.BackupConfig = nil
			primaries[nodeName] = &InstanceHost{
				InstanceID: database.InstanceIDFor(node.HostIDs[0], spec.DatabaseID, nodeName),
				HostID:     node.HostIDs[0],
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get primary instance for node %s: %w", nodeName, err)
		} else {
			// We're only going to restore the primary instance, then we'll recreate
			// the replicas.
			node.HostIDs = []uuid.UUID{primary.Spec.HostID}
			node.RestoreConfig = input.RestoreConfig
			node.BackupConfig = nil
			primaries[nodeName] = &InstanceHost{
				InstanceID: primary.Spec.InstanceID,
				HostID:     primary.Spec.HostID,
			}
		}

	}

	return &RestoreSpecOutput{
		Spec:      spec,
		Primaries: primaries,
	}, nil
}
