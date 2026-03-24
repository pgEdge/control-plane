package zfs

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*Dataset)(nil)

// Dataset is a ZFS resource that provisions a ZFS dataset for a Postgres
// instance. It replaces DirResource for data directories on ZFS-backed hosts.
type Dataset struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	HostID     string `json:"host_id"`
	Pool       string `json:"pool"`
	MountPoint string `json:"mount_point"`
	OwnerUID   int    `json:"owner_uid"`
	OwnerGID   int    `json:"owner_gid"`

	// Run is an injectable CommandRunner for testability. If nil,
	// DefaultCommandRunner is used.
	Run CommandRunner `json:"-"`
}

func (d *Dataset) runner() CommandRunner {
	if d.Run != nil {
		return d.Run
	}
	return DefaultCommandRunner
}

func (d *Dataset) ResourceVersion() string {
	return "1"
}

func (d *Dataset) DiffIgnore() []string {
	return nil
}

func (d *Dataset) Executor() resource.Executor {
	return resource.HostExecutor(d.HostID)
}

func (d *Dataset) Identifier() resource.Identifier {
	return resource.Identifier{Type: ResourceTypeDataset, ID: d.ID}
}

func (d *Dataset) Dependencies() []resource.Identifier {
	if d.ParentID == "" {
		return nil
	}
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(d.ParentID),
	}
}

func (d *Dataset) TypeDependencies() []resource.Type {
	return nil
}

func (d *Dataset) Refresh(_ context.Context, _ *resource.Context) error {
	name := DatasetName(d.Pool, d.ID)
	exists, err := datasetExists(d.runner(), name)
	if err != nil {
		return fmt.Errorf("failed to check dataset %q: %w", name, err)
	}
	if !exists {
		return resource.ErrNotFound
	}
	return nil
}

func (d *Dataset) Create(_ context.Context, _ *resource.Context) error {
	name := DatasetName(d.Pool, d.ID)
	_, err := d.runner()("create", "-o", fmt.Sprintf("mountpoint=%s", d.MountPoint), name)
	if err != nil {
		return fmt.Errorf("failed to create dataset %q: %w", name, err)
	}
	if err := d.chown(); err != nil {
		return err
	}
	return nil
}

func (d *Dataset) chown() error {
	if d.OwnerUID == 0 && d.OwnerGID == 0 {
		return nil
	}
	if _, err := hostChown(d.MountPoint, d.OwnerUID, d.OwnerGID); err != nil {
		return fmt.Errorf("failed to chown dataset mountpoint %q: %w", d.MountPoint, err)
	}
	return nil
}

func (d *Dataset) Update(ctx context.Context, rc *resource.Context) error {
	return d.Create(ctx, rc)
}

func (d *Dataset) Delete(_ context.Context, _ *resource.Context) error {
	name := DatasetName(d.Pool, d.ID)
	_, err := d.runner()("destroy", name)
	if err != nil {
		return fmt.Errorf("failed to destroy dataset %q: %w", name, err)
	}
	return nil
}
