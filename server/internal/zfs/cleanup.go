package zfs

import (
	"context"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SpockCleanup)(nil)

// SpockCleanup is a resource that runs post-start SQL to remove Spock
// replication metadata copied from the source instance into the clone. It
// depends on the cloned Postgres service being up and accepting connections.
type SpockCleanup struct {
	CloneInstanceID string   `json:"clone_instance_id"`
	HostID          string   `json:"host_id"`
	ServiceID       string   `json:"service_id"`
	Port            int      `json:"port"`
	DatabaseNames   []string `json:"database_names"`
}

func (c *SpockCleanup) ResourceVersion() string {
	return "1"
}

func (c *SpockCleanup) DiffIgnore() []string {
	return nil
}

func (c *SpockCleanup) Executor() resource.Executor {
	return resource.HostExecutor(c.HostID)
}

func (c *SpockCleanup) Identifier() resource.Identifier {
	return resource.Identifier{Type: ResourceTypeCleanup, ID: c.CloneInstanceID}
}

func (c *SpockCleanup) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		{Type: "swarm.postgres_service", ID: c.ServiceID},
	}
}

func (c *SpockCleanup) TypeDependencies() []resource.Type {
	return nil
}

// Refresh checks whether the Spock cleanup SQL has already been applied to the
// clone instance.
//
// TODO: Connect to Postgres on c.Port and check whether spock subscription and
// node metadata tables are empty for each database in c.DatabaseNames. Return
// nil if all databases are clean, resource.ErrNotFound otherwise.
func (c *SpockCleanup) Refresh(_ context.Context, _ *resource.Context) error {
	return nil
}

// Create runs SQL against the cloned Postgres instance to remove Spock
// replication metadata (subscriptions, nodes, etc.) so the clone operates as
// a standalone database without attempting to replicate.
//
// TODO: For each database in c.DatabaseNames, connect to Postgres on c.Port
// and execute:
//   - DELETE FROM spock.subscription (or equivalent DDL to drop subscriptions)
//   - DELETE FROM spock.node (drop node entries)
//   - Any other Spock catalog cleanup required for a clean standalone instance
func (c *SpockCleanup) Create(_ context.Context, _ *resource.Context) error {
	return nil
}

func (c *SpockCleanup) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

// Delete is a no-op; Spock cleanup is a one-time idempotent operation and
// there is nothing to undo when a clone is removed.
func (c *SpockCleanup) Delete(_ context.Context, _ *resource.Context) error {
	return nil
}
