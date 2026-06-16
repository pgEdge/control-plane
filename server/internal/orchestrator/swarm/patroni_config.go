package swarm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
	DatabaseID          string                `json:"database_id"`
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
	deps := c.Base.Dependencies()
	deps = append(deps, NetworkResourceIdentifier(c.DatabaseNetworkName))

	return deps
}

func (r *PatroniConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *PatroniConfig) Create(ctx context.Context, rc *resource.Context) error {
	addresses, extraHBA, err := c.getAddressesAndHBA(rc)
	if err != nil {
		return err
	}

	return c.Base.Create(ctx, rc, addresses, extraHBA)
}

func (c *PatroniConfig) Update(ctx context.Context, rc *resource.Context) error {
	addresses, extraHBA, err := c.getAddressesAndHBA(rc)
	if err != nil {
		return err
	}

	return c.Base.Update(ctx, rc, addresses, extraHBA, c.signalReload)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}

func (c *PatroniConfig) getAddressesAndHBA(rc *resource.Context) ([]string, []hba.Entry, error) {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(c.DatabaseNetworkName))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get database network from state: %w", err)
	}
	addresses := []string{
		c.BridgeNetworkInfo.Gateway.String(),
		network.Subnet.String(),
	}
	extraHBA := []hba.Entry{
		{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "all",
			Address:    c.BridgeNetworkInfo.Gateway.String(),
			AuthMethod: c.Base.Generator.AuthMethod(),
		},
		{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "all",
			Address:    c.BridgeNetworkInfo.Subnet.String(),
			AuthMethod: hba.AuthMethodReject,
		},
	}
	return addresses, extraHBA, nil
}

func (c *PatroniConfig) signalReload(ctx context.Context, rc *resource.Context, wait time.Duration) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	// Signal the container if it exists
	container, err := GetPostgresContainer(ctx, client, c.Base.InstanceID)
	if errors.Is(err, ErrNoPostgresContainer) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check if postgres container exists: %w", err)
	}
	if err := client.ContainerSignal(ctx, container.ID, "SIGHUP"); err != nil {
		return fmt.Errorf("failed to signal patroni to reload: %w", err)
	}

	return utils.SleepContext(ctx, wait)
}
