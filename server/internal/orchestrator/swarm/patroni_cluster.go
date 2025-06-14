package swarm

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var _ resource.Resource = (*PatroniCluster)(nil)

const ResourceTypePatroniCluster resource.Type = "swarm.patroni_cluster"

func PatroniClusterResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePatroniCluster,
	}
}

type PatroniCluster struct {
	ClusterID        string `json:"cluster_id"`
	DatabaseID       string `json:"database_id"`
	NodeName         string `json:"node_name"`
	PatroniNamespace string `json:"patroni_namespace"`
}

func (p *PatroniCluster) ResourceVersion() string {
	return "1"
}

func (p *PatroniCluster) DiffIgnore() []string {
	return nil
}

func (p *PatroniCluster) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCluster,
		ID:   p.ClusterID,
	}
}

func (p *PatroniCluster) Identifier() resource.Identifier {
	return PatroniClusterResourceIdentifier(p.NodeName)
}

func (p *PatroniCluster) Dependencies() []resource.Identifier {
	return nil
}

func (p *PatroniCluster) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniCluster) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniCluster) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PatroniCluster) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*clientv3.Client](rc.Injector)
	if err != nil {
		return err
	}

	_, err = client.Delete(ctx, p.PatroniNamespace, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to delete patroni namespace from DCS: %w", err)
	}

	return nil
}
