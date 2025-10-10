package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

var _ resource.Resource = (*DirResource)(nil)

const ResourceTypeDir resource.Type = "filesystem.dir"

func DirResourceIdentifier(id string) resource.Identifier {
	return resource.Identifier{
		ID:   id,
		Type: ResourceTypeDir,
	}
}

type DirResource struct {
	ID       string      `json:"id"`
	ParentID string      `json:"parent_id"`
	HostID   string      `json:"host_id"`
	Path     string      `json:"path"`
	OwnerUID int         `json:"owner_uid"`
	OwnerGID int         `json:"owner_gid"`
	Perm     os.FileMode `json:"perm"`
	FullPath string      `json:"full_path"`
}

func (d *DirResource) ResourceVersion() string {
	return "1"
}

func (d *DirResource) DiffIgnore() []string {
	return []string{
		"/full_path",
	}
}

func (d *DirResource) Executor() resource.Executor {
	return resource.HostExecutor(d.HostID)
}

func (d *DirResource) Identifier() resource.Identifier {
	return DirResourceIdentifier(d.ID)
}

func (d *DirResource) Dependencies() []resource.Identifier {
	if d.ParentID == "" {
		return nil
	}
	return []resource.Identifier{
		{
			Type: ResourceTypeDir,
			ID:   d.ParentID,
		},
	}
}

func (d *DirResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	fullPath, err := d.fullPath(ctx, rc)
	if err != nil {
		return err
	}
	d.FullPath = fullPath

	info, err := fs.Stat(d.FullPath)
	if errors.Is(err, afero.ErrFileNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to stat %q: %w", d.FullPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("expected %q to be a directory", d.FullPath)
	}

	return nil
}

func (d *DirResource) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	fullPath, err := d.fullPath(ctx, rc)
	if err != nil {
		return err
	}
	d.FullPath = fullPath

	perm := d.Perm
	if perm == 0 {
		perm = 0o700
	}
	if err := fs.MkdirAll(d.FullPath, perm); err != nil {
		return fmt.Errorf("failed to make directory: %w", err)
	}
	if d.OwnerUID != 0 && d.OwnerGID != 0 {
		if err := fs.Chown(d.FullPath, d.OwnerUID, d.OwnerGID); err != nil {
			return fmt.Errorf("failed to change ownership for directory %q: %w", d.FullPath, err)
		}
	}
	return nil
}

func (d *DirResource) Update(ctx context.Context, rc *resource.Context) error {
	return d.Create(ctx, rc)
}

func (d *DirResource) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	path, err := d.fullPath(ctx, rc)
	if err != nil {
		return err
	}
	if err := fs.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}
	return nil
}

func (d *DirResource) fullPath(ctx context.Context, rc *resource.Context) (string, error) {
	if d.ParentID == "" {
		return d.Path, nil
	}
	parentFullPath, err := DirResourceFullPath(rc, d.ParentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(parentFullPath, d.Path), nil
}

func DirResourceFullPath(rc *resource.Context, resourceID string) (string, error) {
	parent, err := resource.FromContext[*DirResource](rc, DirResourceIdentifier(resourceID))
	if err != nil {
		return "", fmt.Errorf("failed to get dir %q: %w", resourceID, err)
	}
	if parent.FullPath == "" {
		// This should not happen as long as we're calling the resource methods
		// in the correct order.
		return "", fmt.Errorf("dir %q full path is not yet set", resourceID)
	}
	return parent.FullPath, nil
}
