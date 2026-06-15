package common

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

type PatroniConfig struct {
	InstanceID string                  `json:"instance_id"`
	HostID     string                  `json:"host_id"`
	NodeName   string                  `json:"node_name"`
	Generator  *PatroniConfigGenerator `json:"generator"`
	ParentID   string                  `json:"parent_id"`
	OwnerUID   int                     `json:"owner_uid"`
	OwnerGID   int                     `json:"owner_gid"`
}

func (c *PatroniConfig) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
		EtcdCredsIdentifier(c.InstanceID),
		PatroniMemberResourceIdentifier(c.InstanceID),
		PatroniClusterResourceIdentifier(c.NodeName),
	}
	if c.Generator.ArchiveCommand != "" {
		deps = append(deps, PgBackRestConfigIdentifier(c.InstanceID, pgbackrest.ConfigTypeBackup))
	}
	if c.Generator.RestoreCommand != "" {
		deps = append(deps, PgBackRestConfigIdentifier(c.InstanceID, pgbackrest.ConfigTypeRestore))
	}
	return deps
}

func (c *PatroniConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	contents, err := ReadResourceFile(fs, filepath.Join(parentFullPath, "patroni.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read patroni config: %w", err)
	}

	// Test that we can parse the file. We'll want to recreate the file if it's
	// malformed.
	var config patroni.Config
	if err := yaml.Unmarshal(contents, &config); err != nil {
		return fmt.Errorf("%w: failed to unmarshal patroni config", resource.ErrNotFound)
	}

	return nil
}

func (c *PatroniConfig) Create(
	ctx context.Context,
	rc *resource.Context,
	systemAddresses []string,
	extraHbaEntries []hba.Entry,
) error {
	_, err := c.create(ctx, rc, systemAddresses, extraHbaEntries)
	return err
}

func (c *PatroniConfig) Update(
	ctx context.Context,
	rc *resource.Context,
	systemAddresses []string,
	extraHbaEntries []hba.Entry,
	reload func(ctx context.Context, rc *resource.Context, wait time.Duration) error,
) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	cfg, err := c.create(ctx, rc, systemAddresses, extraHbaEntries)
	if err != nil {
		return err
	}

	wait := patroni.DefaultLoopWaitSeconds * time.Second
	if client := c.client(rc); client != nil {
		w, isPrimary := c.getStatusInfo(ctx, client)
		if w > 0 {
			wait = w
		}
		if isPrimary && cfg.Bootstrap != nil && cfg.Bootstrap.DCS != nil {
			_, err := client.PatchDynamicConfig(ctx, cfg.Bootstrap.DCS.ToDynamicConfig())
			if err != nil {
				logger.Warn().
					Str("database_id", c.Generator.DatabaseID).
					Str("instance_id", c.InstanceID).
					Err(err).
					Msg("failed to patch dynamic config")
			}
		}
	}

	// We intentionally leave the reload implementation up to the caller rather
	// than use the Patroni API's reload method. A process signal-based reload
	// has the advantage that it will work regardless of whether the Patroni API
	// is healthy.
	if err := reload(ctx, rc, wait); err != nil {
		return fmt.Errorf("failed to trigger patroni reload: %w", err)
	}

	return nil
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	err = fs.Remove(filepath.Join(parentFullPath, "patroni.yaml"))
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove patroni.yaml: %w", err)
	}

	return nil
}

func (c *PatroniConfig) create(
	ctx context.Context,
	rc *resource.Context,
	systemAddresses []string,
	extraHbaEntries []hba.Entry,
) (*patroni.Config, error) {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return nil, err
	}
	etcdClient, err := do.Invoke[*clientv3.Client](rc.Injector)
	if err != nil {
		return nil, err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent full path: %w", err)
	}

	etcdCreds, err := resource.FromContext[*EtcdCreds](rc, EtcdCredsIdentifier(c.InstanceID))
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd creds from state: %w", err)
	}

	etcdHosts, err := patroni.EtcdHosts(ctx, etcdClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd hosts: %w", err)
	}

	enableFastBasebackup, err := c.isNewNode(rc)
	if err != nil {
		return nil, err
	}

	cfg := c.Generator.Generate(etcdHosts, etcdCreds, GenerateOptions{
		EnableFastBasebackup: enableFastBasebackup,
		SystemAddresses:      systemAddresses,
		ExtraHbaEntries:      extraHbaEntries,
	})

	content, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patroni config: %w", err)
	}

	configPath := filepath.Join(parentFullPath, "patroni.yaml")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, c.OwnerUID, c.OwnerGID); err != nil {
		return nil, fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return cfg, nil
}

func (c *PatroniConfig) isNewNode(rc *resource.Context) (bool, error) {
	_, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(c.NodeName))
	switch {
	case errors.Is(err, resource.ErrNotFound):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("failed to check if node already exists: %w", err)
	default:
		return false, nil
	}
}

func (c *PatroniConfig) client(rc *resource.Context) *patroni.Client {
	// We're not using FromContext here to handle the case where the instance
	// creation failed, but Patroni is still running.
	data, ok := rc.State.Get(database.InstanceResourceIdentifier(c.InstanceID))
	if !ok {
		return nil
	}
	instance, err := resource.ToResource[*database.InstanceResource](data)
	if err == nil && instance.ConnectionInfo != nil {
		return patroni.NewClient(instance.ConnectionInfo.PatroniURL(), nil)
	}

	return nil
}

func (c *PatroniConfig) getStatusInfo(ctx context.Context, client *patroni.Client) (time.Duration, bool) {
	cfg, err := client.GetDynamicConfig(ctx)
	if err != nil {
		return 0, false
	}

	var loopWait time.Duration
	if cfg.LoopWait == nil {
		loopWait = patroni.DefaultLoopWaitSeconds * time.Second
	} else {
		loopWait = time.Duration(*cfg.LoopWait) * time.Second
	}

	wait := loopWait
	status, err := client.GetInstanceStatus(ctx)
	if err != nil {
		return wait, false
	}
	if status.DCSLastSeen != nil {
		lastSeen := time.Unix(*status.DCSLastSeen, 0)
		lowerBound := time.Now().Add(-2 * loopWait)
		upperBound := time.Now()
		// Ignore last seen if clocks are very off in either direction
		if lastSeen.After(lowerBound) && lastSeen.Before(upperBound) {
			// Compute the time until the next run cycle
			wait = time.Until(lastSeen.Add(loopWait))
		}
	}

	return wait, status.IsPrimary()
}
