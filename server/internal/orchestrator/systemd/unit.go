package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/coreos/go-systemd/v22/unit"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

const unitsDir = "/etc/systemd/system"

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
	Name              string                `json:"name"`
	Options           []*unit.UnitOption    `json:"options"`
	ExtraDependencies []resource.Identifier `json:"extra_dependencies"`
	// RestartOnChange restarts the unit instead of reloading it when the unit
	// file's contents change, e.g. for changes to Environment= that a reload
	// wouldn't apply.
	RestartOnChange bool `json:"restart_on_change,omitempty"`
}

func (r *UnitResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *UnitResource) Identifier() resource.Identifier {
	return UnitResourceIdentifier(r.Name, r.DatabaseID, r.HostID)
}

func (r *UnitResource) Dependencies() []resource.Identifier {
	return r.ExtraDependencies
}

func (r *UnitResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *UnitResource) Refresh(ctx context.Context, rc *resource.Context) error {
	options, err := readUnitFile(r.Name)
	if errors.Is(err, os.ErrNotExist) {
		return resource.ErrNotFound
	} else if err != nil {
		return err
	}

	r.Options = options

	return nil
}

// readUnitFile reads the options from the named unit file. It returns an error
// that wraps os.ErrNotExist if the unit file does not exist.
func readUnitFile(name string) ([]*unit.UnitOption, error) {
	path := filepath.Join(unitsDir, name)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open unit file '%s': %w", path, err)
	}
	defer f.Close()

	options, err := unit.Deserialize(f)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize unit file '%s': %w", path, err)
	}

	return options, nil
}

func (r *UnitResource) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*Client](rc.Injector)
	if err != nil {
		return err
	}

	changed := false
	if r.RestartOnChange {
		current, err := readUnitFile(r.Name)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// The unit is new, so it gets started below rather than restarted.
		case err != nil:
			return err
		default:
			changed = !unitOptionsEqual(current, r.Options)
		}
	}

	path := filepath.Join(unitsDir, r.Name)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open unit file for writing '%s': %w", path, err)
	}
	defer f.Close()

	_, err = io.Copy(f, unit.Serialize(r.Options))
	if err != nil {
		return fmt.Errorf("failed to write unit file '%s': %w", path, err)
	}

	if err := client.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload: %w", err)
	}
	if err := client.EnableUnit(ctx, r.Name); err != nil {
		return fmt.Errorf("failed to enable unit '%s': %w", path, err)
	}
	if changed {
		if err := client.RestartUnit(ctx, r.Name); err != nil {
			return fmt.Errorf("failed to restart unit '%s': %w", path, err)
		}
		return nil
	}
	if err := client.ReloadOrRestartUnit(ctx, r.Name); err != nil {
		return fmt.Errorf("failed to reload or restart unit '%s': %w", path, err)
	}

	return nil
}

func unitOptionsEqual(a, b []*unit.UnitOption) bool {
	return slices.EqualFunc(a, b, func(x, y *unit.UnitOption) bool {
		return x.Match(y)
	})
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
		if err := client.StopUnit(ctx, r.Name, true); err != nil {
			return fmt.Errorf("failed to stop unit: %w", err)
		}
		if err := client.DisableUnit(ctx, r.Name); err != nil {
			return fmt.Errorf("failed to disable unit: %w", err)
		}
	}

	path := filepath.Join(unitsDir, r.Name)
	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove unit file '%s': %w", path, err)
	}

	if err := client.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload: %w", err)
	}

	return nil
}

func (r *UnitResource) DiffIgnore() []string {
	return nil
}

func (r *UnitResource) ResourceVersion() string {
	return "1"
}
