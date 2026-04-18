package common

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PgBackRestConfig)(nil)

const ResourceTypePgBackRestConfig resource.Type = "common.pgbackrest_config"

func PgBackRestConfigIdentifier(instanceID string, configType pgbackrest.ConfigType) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID + "-" + configType.String(),
		Type: ResourceTypePgBackRestConfig,
	}
}

type PgBackRestConfig struct {
	InstanceID   string                   `json:"instance_id"`
	HostID       string                   `json:"host_id"`
	DatabaseID   string                   `json:"database_id"`
	NodeName     string                   `json:"node_name"`
	Repositories []*pgbackrest.Repository `json:"repositories"`
	ParentID     string                   `json:"parent_id"`
	Type         pgbackrest.ConfigType    `json:"type"`
	OwnerUID     int                      `json:"owner_uid"`
	OwnerGID     int                      `json:"owner_gid"`
	Paths        database.InstancePaths   `json:"paths"`
	Port         int                      `json:"port"`
}

func (c *PgBackRestConfig) ResourceVersion() string {
	return "1"
}

func (c *PgBackRestConfig) DiffIgnore() []string {
	return nil
}

func (c *PgBackRestConfig) Executor() resource.Executor {
	return resource.HostExecutor(c.HostID)
}

func (c *PgBackRestConfig) Identifier() resource.Identifier {
	return PgBackRestConfigIdentifier(c.InstanceID, c.Type)
}

func (c *PgBackRestConfig) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
	}
}

func (c *PgBackRestConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *PgBackRestConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	hostPath, err := c.HostPath(rc)
	if err != nil {
		return err
	}

	_, err = ReadResourceFile(fs, hostPath)
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
		PgDataPath:   c.Paths.Instance.PgData(),
		HostUser:     "pgedge",
		User:         "pgedge",
		Port:         c.Port,
	}); err != nil {
		return fmt.Errorf("failed to generate pgBackRest configuration: %w", err)
	}

	hostPath, err := c.HostPath(rc)
	if err != nil {
		return err
	}

	if err := afero.WriteFile(fs, hostPath, b.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostPath, err)
	}
	if err := fs.Chown(hostPath, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", hostPath, err)
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

	hostPath, err := c.HostPath(rc)
	if err != nil {
		return err
	}

	err = fs.Remove(hostPath)
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove pgbackrest config: %w", err)
	}

	return nil
}

func (c *PgBackRestConfig) BaseName() string {
	return fmt.Sprintf("pgbackrest.%s.conf", c.Type)
}

func (c *PgBackRestConfig) HostPath(rc *resource.Context) (string, error) {
	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return "", fmt.Errorf("failed to get parent full path: %w", err)
	}

	return filepath.Join(parentFullPath, c.BaseName()), nil
}
