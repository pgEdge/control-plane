package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*NodeResource)(nil)

const ResourceTypeNode resource.Type = "database.node"

func NodeResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypeNode,
	}
}

type NodeResource struct {
	ClusterID         uuid.UUID   `json:"cluster_id"`
	Name              string      `json:"name"`
	InstanceIDs       []uuid.UUID `json:"instance_ids"`
	PrimaryInstanceID uuid.UUID   `json:"primary_instance_id"`
}

func (n *NodeResource) ResourceVersion() string {
	return "1"
}

func (n *NodeResource) DiffIgnore() []string {
	return []string{
		"/primary_instance_id",
	}
}

func (n *NodeResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCluster,
		ID:   n.ClusterID.String(),
	}
}

func (n *NodeResource) Identifier() resource.Identifier {
	return NodeResourceIdentifier(n.Name)
}

func (n *NodeResource) Dependencies() []resource.Identifier {
	var dependencies []resource.Identifier
	for _, id := range n.InstanceIDs {
		dependencies = append(dependencies, InstanceResourceIdentifier(id))
	}
	return dependencies
}

func (n *NodeResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := n.Create(ctx, rc); err != nil {
		return err
	}
	return nil
}

func (n *NodeResource) Create(ctx context.Context, rc *resource.Context) error {
	if len(n.InstanceIDs) == 0 {
		return fmt.Errorf("node %q does not have any instances", n.Name)
	}

	// The primary instance ID should be the same on every instance
	instance, err := resource.FromContext[*InstanceResource](rc, InstanceResourceIdentifier(n.InstanceIDs[0]))
	if err != nil {
		return fmt.Errorf("failed to get instance %q: %w", n.InstanceIDs[0], err)
	}
	n.PrimaryInstanceID = instance.PrimaryInstanceID

	return nil
}

func (n *NodeResource) Update(ctx context.Context, rc *resource.Context) error {
	return n.Create(ctx, rc)
}

func (n *NodeResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
