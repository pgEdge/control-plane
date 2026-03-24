package zfs

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

func (c *Clone) chown() error {
	if c.OwnerUID == 0 && c.OwnerGID == 0 {
		return nil
	}
	if _, err := hostChown(c.MountPoint, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to chown clone mountpoint %q: %w", c.MountPoint, err)
	}
	return nil
}

var _ resource.Resource = (*Clone)(nil)

// Clone is a ZFS resource that creates a writable clone of a snapshot,
// serving as the data directory for a cloned Postgres instance.
type Clone struct {
	SourceInstanceID string `json:"source_instance_id"`
	CloneInstanceID  string `json:"clone_instance_id"`
	HostID           string `json:"host_id"`
	Pool             string `json:"pool"`
	MountPoint       string `json:"mount_point"`
	OwnerUID         int    `json:"owner_uid"`
	OwnerGID         int    `json:"owner_gid"`

	// Run is an injectable CommandRunner for testability. If nil,
	// DefaultCommandRunner is used.
	Run CommandRunner `json:"-"`
}

func (c *Clone) runner() CommandRunner {
	if c.Run != nil {
		return c.Run
	}
	return DefaultCommandRunner
}

func (c *Clone) ResourceVersion() string {
	return "1"
}

func (c *Clone) DiffIgnore() []string {
	return nil
}

func (c *Clone) Executor() resource.Executor {
	return resource.HostExecutor(c.HostID)
}

func (c *Clone) Identifier() resource.Identifier {
	return resource.Identifier{Type: ResourceTypeClone, ID: c.CloneInstanceID}
}

func (c *Clone) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		SnapshotIdentifier(c.CloneInstanceID),
	}
}

func (c *Clone) TypeDependencies() []resource.Type {
	return nil
}

func (c *Clone) Refresh(_ context.Context, _ *resource.Context) error {
	name := DatasetName(c.Pool, c.CloneInstanceID)
	exists, err := datasetExists(c.runner(), name)
	if err != nil {
		return fmt.Errorf("failed to check clone dataset %q: %w", name, err)
	}
	if !exists {
		return resource.ErrNotFound
	}
	return nil
}

func (c *Clone) Create(_ context.Context, _ *resource.Context) error {
	snapshot := SnapshotName(c.Pool, c.SourceInstanceID, c.CloneInstanceID)
	cloneDataset := DatasetName(c.Pool, c.CloneInstanceID)
	_, err := c.runner()("clone", "-o", fmt.Sprintf("mountpoint=%s", c.MountPoint), snapshot, cloneDataset)
	if err != nil {
		return fmt.Errorf("failed to clone %q to %q: %w", snapshot, cloneDataset, err)
	}
	if err := c.chown(); err != nil {
		return err
	}
	return nil
}

func (c *Clone) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *Clone) Delete(_ context.Context, _ *resource.Context) error {
	name := DatasetName(c.Pool, c.CloneInstanceID)
	_, err := c.runner()("destroy", name)
	if err != nil {
		return fmt.Errorf("failed to destroy clone dataset %q: %w", name, err)
	}
	return nil
}
