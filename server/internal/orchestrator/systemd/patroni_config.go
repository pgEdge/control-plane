package systemd

import (
	"context"
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*PatroniConfig)(nil)

const ResourceTypePatroniConfig resource.Type = "systemd.patroni_config"

func PatroniConfigIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniConfig,
	}
}

type PatroniConfig struct {
	DatabaseID string                `json:"database_id"`
	Base       *common.PatroniConfig `json:"base"`
	AllHostIDs []string              `json:"all_host_ids"`
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
	return c.Base.Dependencies()
}

func (c *PatroniConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *PatroniConfig) Create(ctx context.Context, rc *resource.Context) error {
	hostSvc, err := do.Invoke[*host.Service](rc.Injector)
	if err != nil {
		return err
	}
	hosts, err := hostSvc.GetHosts(ctx, c.AllHostIDs)
	if err != nil {
		return fmt.Errorf("failed to get hosts: %w", err)
	}
	if len(hosts) != len(c.AllHostIDs) {
		return fmt.Errorf("wrong number of hosts: expected %d, got %d", len(c.AllHostIDs), len(hosts))
	}

	addresses := ds.NewSet[string]()
	for _, h := range hosts {
		addresses.Add(h.PeerAddresses...)
	}

	return c.Base.Create(ctx, rc, addresses.ToSortedSlice(strings.Compare), nil)
}

func (c *PatroniConfig) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}
