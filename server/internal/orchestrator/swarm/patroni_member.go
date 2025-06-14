package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var _ resource.Resource = (*PatroniMember)(nil)

const ResourceTypePatroniMember resource.Type = "swarm.patroni_member"

func PatroniMemberResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniMember,
	}
}

type PatroniMember struct {
	ClusterID  string `json:"cluster_id"`
	NodeName   string `json:"node_name"`
	InstanceID string `json:"instance_id"`
}

func (p *PatroniMember) ResourceVersion() string {
	return "1"
}

func (p *PatroniMember) DiffIgnore() []string {
	return nil
}

func (p *PatroniMember) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCluster,
		ID:   p.ClusterID,
	}
}

func (p *PatroniMember) Identifier() resource.Identifier {
	return PatroniMemberResourceIdentifier(p.InstanceID)
}

func (p *PatroniMember) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		PatroniClusterResourceIdentifier(p.NodeName),
	}
}

func (p *PatroniMember) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniMember) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniMember) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniMember) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*clientv3.Client](rc.Injector)
	if err != nil {
		return err
	}

	cluster, err := resource.FromContext[*PatroniCluster](rc, PatroniClusterResourceIdentifier(p.NodeName))
	if errors.Is(err, resource.ErrNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get patroni cluster from state: %w", err)
	}

	key := fmt.Sprintf("%s/members/%s", cluster.PatroniNamespace, p.InstanceID)
	_, err = client.Delete(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to delete patroni cluster member from DCS: %w", err)
	}

	return nil
}
