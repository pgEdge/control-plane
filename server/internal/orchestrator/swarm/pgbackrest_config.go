package swarm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PgBackRestConfig)(nil)

const ResourceTypePgBackRestConfig resource.Type = "swarm.pgbackrest_config"

func PgBackRestConfigIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypePgBackRestConfig,
	}
}

type PgBackRestConfig struct {
	InstanceID   uuid.UUID                    `json:"instance_id"`
	HostID       uuid.UUID                    `json:"host_id"`
	DatabaseID   uuid.UUID                    `json:"database_id"`
	NodeName     string                       `json:"node_name"`
	Repositories []*database.BackupRepository `json:"repositories"`
	ParentID     string                       `json:"parent_id"`
	Name         string                       `json:"name"`
	OwnerUID     int                          `json:"owner_uid"`
	OwnerGID     int                          `json:"owner_gid"`
}

func (c *PgBackRestConfig) ResourceVersion() string {
	return "1"
}

func (c *PgBackRestConfig) DiffIgnore() []string {
	return nil
}

func (c *PgBackRestConfig) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   c.HostID.String(),
	}
}

func (c *PgBackRestConfig) Identifier() resource.Identifier {
	return PgBackRestConfigIdentifier(c.InstanceID)
}

func (c *PgBackRestConfig) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
	}
}

func (c *PgBackRestConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	_, err = readResourceFile(fs, filepath.Join(parentFullPath, c.Name))
	if err != nil {
		return fmt.Errorf("failed to read pgbackrest config: %w", err)
	}

	return nil
}

func (c *PgBackRestConfig) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	if err := pgbackrest.WriteConfig(&b, pgbackrest.ConfigOptions{
		Repositories: c.Repositories,
		DatabaseID:   c.DatabaseID,
		NodeName:     c.NodeName,
		PgDataPath:   "/opt/pgedge/data",
		HostUser:     "pgedge",
		User:         "pgedge",
	}); err != nil {
		return fmt.Errorf("failed to generate pgBackRest backup configuration: %w", err)
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	configPath := filepath.Join(parentFullPath, c.Name)
	if err := afero.WriteFile(fs, configPath, b.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return nil
}

func (c *PgBackRestConfig) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PgBackRestConfig) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	err = fs.Remove(filepath.Join(parentFullPath, c.Name))
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove pgbackrest config: %w", err)
	}

	return nil
}
