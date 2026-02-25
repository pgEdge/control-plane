package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/unit"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*UnitResource)(nil)

const ResourceTypeUnit resource.Type = "systemd.unit"

func UnitResourceIdentifier(name, databaseID, hostID string) resource.Identifier {
	return resource.Identifier{
		ID:   name + ":" + databaseID + ":" + hostID,
		Type: ResourceTypeUnit,
	}
}

type UnitResource struct {
	DatabaseID        string                `json:"database_id"`
	HostID            string                `json:"host_id"`
	ParentDirID       string                `json:"parent_dir_id"`
	Name              string                `json:"name"`
	Options           []*unit.UnitOption    `json:"options"`
	ExtraDependencies []resource.Identifier `json:"extra_dependencies"`
}

func (r *UnitResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *UnitResource) Identifier() resource.Identifier {
	return UnitResourceIdentifier(r.Name, r.DatabaseID, r.HostID)
}

func (r *UnitResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		filesystem.DirResourceIdentifier(r.ParentDirID),
	}
	deps = append(deps, r.ExtraDependencies...)

	return deps
}

func (r *UnitResource) Refresh(ctx context.Context, rc *resource.Context) error {
	parentPath, err := filesystem.DirResourceFullPath(rc, r.ParentDirID)
	if err != nil {
		return fmt.Errorf("failed to get parent dir path: %w", err)
	}
	path := filepath.Join(parentPath, r.Name)

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to open unit file '%s': %w", path, err)
	}
	defer f.Close()

	options, err := unit.Deserialize(f)
	if err != nil {
		return fmt.Errorf("failed to deserialize unit file '%s': %w", path, err)
	}

	r.Options = options

	return nil
}

func (r *UnitResource) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*Client](rc.Injector)
	if err != nil {
		return err
	}

	parentPath, err := filesystem.DirResourceFullPath(rc, r.ParentDirID)
	if err != nil {
		return fmt.Errorf("failed to get parent dir path: %w", err)
	}
	path := filepath.Join(parentPath, r.Name)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open unit file for writing '%s': %w", path, err)
	}
	defer f.Close()

	_, err = io.Copy(f, unit.Serialize(r.Options))
	if err != nil {
		return fmt.Errorf("failed to write unit file '%s': %w", path, err)
	}

	if err := client.LinkUnit(ctx, path); err != nil {
		return fmt.Errorf("failed to link unit '%s': %w", path, err)
	}
	if err := client.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload: %w", err)
	}
	if err := client.EnableUnit(ctx, r.Name); err != nil {
		return fmt.Errorf("failed to enable unit '%s': %w", path, err)
	}
	if err := client.RestartUnit(ctx, r.Name); err != nil {
		return fmt.Errorf("failed to restart unit '%s': %w", path, err)
	}

	return nil
}

func (r *UnitResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *UnitResource) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*Client](rc.Injector)
	if err != nil {
		return err
	}

	err = client.UnitExists(ctx, r.Name)
	switch {
	case errors.Is(err, ErrUnitNotFound):
		// No need to remove the unit if it doesn't exist
	case err != nil:
		return fmt.Errorf("failed to check if unit exists: %w", err)
	default:
		if err := client.StopUnit(ctx, r.Name); err != nil {
			return fmt.Errorf("failed to stop unit: %w", err)
		}
		if err := client.DisableUnit(ctx, r.Name); err != nil {
			return fmt.Errorf("failed to disable unit: %w", err)
		}
		if err := client.RemoveUnitFile(ctx, r.Name); err != nil {
			return fmt.Errorf("failed to remove unit: %w", err)
		}
	}

	parentPath, err := filesystem.DirResourceFullPath(rc, r.ParentDirID)
	if err != nil {
		return fmt.Errorf("failed to get parent dir path: %w", err)
	}
	path := filepath.Join(parentPath, r.Name)

	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove unit file '%s': %w", path, err)
	}

	return nil
}

func (r *UnitResource) DiffIgnore() []string {
	return nil
}

func (r *UnitResource) ResourceVersion() string {
	return "1"
}
