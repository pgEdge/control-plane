package swarm

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PatroniConfig)(nil)

const ResourceTypePatroniConfig resource.Type = "swarm.patroni_config"

func PatroniConfigIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniConfig,
	}
}

type PatroniConfig struct {
	Base                *common.PatroniConfig `json:"base"`
	BridgeNetworkInfo   *docker.NetworkInfo   `json:"host_network_info"`
	DatabaseNetworkName string                `json:"database_network_name"`
}

func (c *PatroniConfig) ResourceVersion() string {
	return "1"
}

func (c *PatroniConfig) DiffIgnore() []string {
	return nil
}

func (c *PatroniConfig) Executor() resource.Executor {
	return resource.HostExecutor(c.Base.HostID)
}

func (c *PatroniConfig) Identifier() resource.Identifier {
	return PatroniConfigIdentifier(c.Base.InstanceID)
}

func (c *PatroniConfig) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{NetworkResourceIdentifier(c.DatabaseNetworkName)}
	deps = append(deps, c.Base.Dependencies()...)

	return deps
}

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *PatroniConfig) Create(ctx context.Context, rc *resource.Context) error {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(c.DatabaseNetworkName))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}

	return c.Base.Create(ctx, rc, []string{
		c.BridgeNetworkInfo.Gateway.String(),
		network.Subnet.String(),
	},
		[]hba.Entry{
			// Use MD5 for non-system users from the gateway. External
			// connections will originate from this address when we publish
			// a host port.
			{
				Type:       hba.EntryTypeHost,
				Database:   "all",
				User:       "all",
				Address:    c.BridgeNetworkInfo.Gateway.String(),
				AuthMethod: hba.AuthMethodMD5,
			},
			// Reject all other connections on the bridge network to prevent
			// connections from other databases.
			{
				Type:       hba.EntryTypeHost,
				Database:   "all",
				User:       "all",
				Address:    c.BridgeNetworkInfo.Subnet.String(),
				AuthMethod: hba.AuthMethodReject,
			},
		})
}

func (c *PatroniConfig) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}
