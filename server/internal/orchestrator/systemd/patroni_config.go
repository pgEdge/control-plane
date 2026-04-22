package systemd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
	if err := c.Create(ctx, rc); err != nil {
		return err
	}

	return c.signalReload(ctx, rc)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}

func (c *PatroniConfig) signalReload(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*Client](rc.Injector)
	if err != nil {
		return err
	}
	// Reload patroni unit if it exists
	name := patroniServiceName(c.Base.InstanceID)
	err = client.UnitExists(ctx, name)
	if errors.Is(err, ErrUnitNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check if patroni unit exists: %w", err)
	}
	if err := client.ReloadUnit(ctx, name); err != nil {
		return fmt.Errorf("failed to reload patroni: %w", err)
	}
	// It can take up to loop_wait seconds for Patroni to reload the config.
	// We'll want to update this code to read loop_wait from c.Base if we make
	// loop_wait configurable.
	return utils.SleepContext(ctx, patroni.DefaultLoopWaitSeconds*time.Second)
}
