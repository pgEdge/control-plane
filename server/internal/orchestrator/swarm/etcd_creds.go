package swarm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/samber/do"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*EtcdCreds)(nil)

const ResourceTypeEtcdCreds resource.Type = "swarm.etcd_creds"

func EtcdCredsIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypeEtcdCreds,
	}
}

type EtcdCreds struct {
	InstanceID uuid.UUID `json:"instance_id"`
	DatabaseID uuid.UUID `json:"database_id"`
	HostID     uuid.UUID `json:"host_id"`
	NodeName   string    `json:"node_name"`
	ParentID   string    `json:"parent_id"`
	OwnerUID   int       `json:"owner_uid"`
	OwnerGID   int       `json:"owner_gid"`
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	CaCert     []byte    `json:"ca_cert"`
	ClientCert []byte    `json:"server_cert"`
	ClientKey  []byte    `json:"server_key"`
}

func (c *EtcdCreds) ResourceVersion() string {
	return "1"
}

func (c *EtcdCreds) DiffIgnore() []string {
	return []string{
		"/username",
		"/password",
		"/ca_cert",
		"/server_cert",
		"/server_key",
	}
}

func (c *EtcdCreds) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   c.HostID.String(),
	}
}

func (c *EtcdCreds) Identifier() resource.Identifier {
	return EtcdCredsIdentifier(c.InstanceID)
}

func (c *EtcdCreds) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
	}
}

func (c *EtcdCreds) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}
	etcdDir := filepath.Join(parentFullPath, "etcd")

	caCert, err := readResourceFile(fs, filepath.Join(etcdDir, "ca.crt"))
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}
	clientCert, err := readResourceFile(fs, filepath.Join(etcdDir, "client.crt"))
	if err != nil {
		return fmt.Errorf("failed to read client cert: %w", err)
	}
	clientKey, err := readResourceFile(fs, filepath.Join(etcdDir, "client.key"))
	if err != nil {
		return fmt.Errorf("failed to read client key: %w", err)
	}

	c.CaCert = caCert
	c.ClientCert = clientCert
	c.ClientKey = clientKey

	return nil
}

func (c *EtcdCreds) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	certService, err := do.Invoke[*certificates.Service](rc.Injector)
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
	etcdDir := filepath.Join(parentFullPath, "etcd")

	etcdCreds, err := etcd.CreateInstanceEtcdUser(ctx,
		etcdClient,
		certService,
		etcd.InstanceUserOptions{
			InstanceID: c.InstanceID,
			KeyPrefix:  patroni.Namespace(c.DatabaseID, c.NodeName),
			Password:   c.Password,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create etcd user: %w", err)
	}

	c.Username = etcdCreds.Username
	c.Password = etcdCreds.Password
	c.CaCert = etcdCreds.CaCert
	c.ClientCert = etcdCreds.ClientCert
	c.ClientKey = etcdCreds.ClientKey

	if err := fs.MkdirAll(etcdDir, 0o700); err != nil {
		return fmt.Errorf("failed to create etcd certificates directory: %w", err)
	}
	if err := fs.Chown(etcdDir, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for certificates directory: %w", err)
	}

	files := map[string][]byte{
		"ca.crt":     c.CaCert,
		"client.crt": c.ClientCert,
		"client.key": c.ClientKey,
	}

	for name, content := range files {
		if err := afero.WriteFile(fs, filepath.Join(etcdDir, name), content, 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
		if err := fs.Chown(filepath.Join(etcdDir, name), c.OwnerUID, c.OwnerGID); err != nil {
			return fmt.Errorf("failed to change ownership for %s: %w", name, err)
		}
	}

	return nil
}

func (c *EtcdCreds) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *EtcdCreds) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	certService, err := do.Invoke[*certificates.Service](rc.Injector)
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
	etcdDir := filepath.Join(parentFullPath, "etcd")

	if err := fs.RemoveAll(etcdDir); err != nil {
		return fmt.Errorf("failed to remove certificates directory: %w", err)
	}
	if err := etcd.RemoveInstanceEtcdUser(ctx, etcdClient, certService, c.InstanceID); err != nil {
		return fmt.Errorf("failed to delete etcd user: %w", err)
	}

	return nil
}

func readResourceFile(fs afero.Fs, path string) ([]byte, error) {
	contents, err := afero.ReadFile(fs, path)
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil, resource.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return contents, nil
}
