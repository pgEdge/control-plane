package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*Switchover)(nil)

const ResourceTypeSwitchover resource.Type = "swarm.switchover"

func SwitchoverResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeSwitchover,
	}
}

type Switchover struct {
	HostID     string `json:"host_id"`
	InstanceID string `json:"instance_id"`
}

func (s *Switchover) ResourceVersion() string {
	return "1"
}

func (s *Switchover) DiffIgnore() []string {
	return nil
}

func (s *Switchover) Executor() resource.Executor {
	return resource.HostExecutor(s.HostID)
}

func (s *Switchover) Identifier() resource.Identifier {
	return SwitchoverResourceIdentifier(s.InstanceID)
}

func (s *Switchover) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		CheckWillRestartIdentifier(s.InstanceID),
	}
}

func (s *Switchover) Refresh(ctx context.Context, rc *resource.Context) error {
	if !rc.State.HasResources(s.Dependencies()...) {
		return resource.ErrNotFound
	}
	return nil
}

func (s *Switchover) Create(ctx context.Context, rc *resource.Context) error {
	checkWillRestart, err := resource.FromContext[*CheckWillRestart](rc, CheckWillRestartIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get 'will restart' check from state: %w", err)
	}
	if !checkWillRestart.WillRestart {
		// No work needed if this instance is not going to restart
		return nil
	}

	existing, err := resource.FromContext[*database.InstanceResource](rc, database.InstanceResourceIdentifier(s.InstanceID))
	if errors.Is(err, resource.ErrNotFound) {
		// This resource doesn't apply to brand new instances
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get existing instance from state: %w", err)
	}

	node, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(existing.Spec.NodeName))
	if errors.Is(err, resource.ErrNotFound) {
		// This node hasn't successfully deployed yet, so we won't worry about
		// switching over.
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get node from state: %w", err)
	}

	if len(node.InstanceIDs) < 2 {
		// This node has no replicas, or it's not in a functional state.
		return nil
	}

	// We know three things at this point:
	// - The instance exists
	// - The instance will restart
	// - There are replica instances
	// Which means this instance should become a replica before it restarts. The
	// switchover resource will check if the instance is already a replica
	// before it does any work.
	switchover := &database.SwitchoverResource{
		HostID:     s.HostID,
		InstanceID: s.InstanceID,
		TargetRole: patroni.InstanceRoleReplica,
	}

	return switchover.Create(ctx, rc)
}

func (s *Switchover) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}

func (s *Switchover) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
