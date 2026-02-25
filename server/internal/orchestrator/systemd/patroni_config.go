package systemd

import (
	"context"

	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
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
	Base          *common.PatroniConfig `json:"base"`
	HostAddresses []string              `json:"host_addresses"`
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

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *PatroniConfig) Create(ctx context.Context, rc *resource.Context) error {
	return c.Base.Create(ctx, rc, c.HostAddresses, nil)
}

func (c *PatroniConfig) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}
