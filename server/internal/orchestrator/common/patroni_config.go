package common

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/samber/do"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/patroni"
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
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	etcdClient, err := do.Invoke[*clientv3.Client](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	etcdCreds, err := resource.FromContext[*EtcdCreds](rc, EtcdCredsIdentifier(c.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get etcd creds from state: %w", err)
	}

	etcdHosts, err := patroni.EtcdHosts(ctx, etcdClient)
	if err != nil {
		return fmt.Errorf("failed to get etcd hosts: %w", err)
	}

	enableFastBasebackup, err := c.isNewNode(rc)
	if err != nil {
		return err
	}

	config := c.Generator.Generate(etcdHosts, etcdCreds, GenerateOptions{
		EnableFastBasebackup: enableFastBasebackup,
		SystemAddresses:      systemAddresses,
		ExtraHbaEntries:      extraHbaEntries,
	})

	content, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal patroni config: %w", err)
	}

	configPath := filepath.Join(parentFullPath, "patroni.yaml")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
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
