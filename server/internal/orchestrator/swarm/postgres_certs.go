package swarm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

var _ resource.Resource = (*PostgresCerts)(nil)

const ResourceTypePostgresCerts resource.Type = "swarm.postgres_certs"

func PostgresCertsIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypePostgresCerts,
	}
}

type PostgresCerts struct {
	InstanceID       uuid.UUID `json:"instance_id"`
	HostID           uuid.UUID `json:"host_id"`
	InstanceHostname string    `json:"instance_hostname"`
	ParentID         string    `json:"parent_id"`
	OwnerUID         int       `json:"owner_uid"`
	OwnerGID         int       `json:"owner_gid"`
	CaCert           []byte    `json:"ca_cert"`
	ServerCert       []byte    `json:"server_cert"`
	ServerKey        []byte    `json:"server_key"`
	SuperuserCert    []byte    `json:"superuser_cert"`
	SuperuserKey     []byte    `json:"superuser_key"`
	ReplicationCert  []byte    `json:"replication_cert"`
	ReplicationKey   []byte    `json:"replication_key"`
}

func (c *PostgresCerts) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   c.HostID.String(),
	}
}

func (c *PostgresCerts) Identifier() resource.Identifier {
	return PostgresCertsIdentifier(c.InstanceID)
}

func (c *PostgresCerts) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
	}
}

func (c *PostgresCerts) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}
	postgresDir := filepath.Join(parentFullPath, "postgres")

	caCert, err := readResourceFile(fs, filepath.Join(postgresDir, "ca.crt"))
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}
	serverCert, err := readResourceFile(fs, filepath.Join(postgresDir, "server.crt"))
	if err != nil {
		return fmt.Errorf("failed to read server cert: %w", err)
	}
	serverKey, err := readResourceFile(fs, filepath.Join(postgresDir, "server.key"))
	if err != nil {
		return fmt.Errorf("failed to read server key: %w", err)
	}
	superuserCert, err := readResourceFile(fs, filepath.Join(postgresDir, "superuser.crt"))
	if err != nil {
		return fmt.Errorf("failed to read superuser cert: %w", err)
	}
	superuserKey, err := readResourceFile(fs, filepath.Join(postgresDir, "superuser.key"))
	if err != nil {
		return fmt.Errorf("failed to read superuser key: %w", err)
	}

	replicationCert, err := readResourceFile(fs, filepath.Join(postgresDir, "replication.crt"))
	if err != nil {
		return fmt.Errorf("failed to read replication cert: %w", err)
	}

	replicationKey, err := readResourceFile(fs, filepath.Join(postgresDir, "replication.key"))
	if err != nil {
		return fmt.Errorf("failed to read replication key: %w", err)
	}

	c.CaCert = caCert
	c.ServerCert = serverCert
	c.ServerKey = serverKey
	c.SuperuserCert = superuserCert
	c.SuperuserKey = superuserKey
	c.ReplicationCert = replicationCert
	c.ReplicationKey = replicationKey

	return nil
}

func (c *PostgresCerts) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	certService, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}
	postgresDir := filepath.Join(parentFullPath, "postgres")

	pgServerPrincipal, err := certService.PostgresServer(ctx,
		c.InstanceID,
		c.InstanceHostname,
		[]string{c.InstanceHostname, "localhost"},
		[]string{"127.0.0.1"},
	)
	if err != nil {
		return fmt.Errorf("failed to create postgres server principal: %w", err)
	}
	pgSuperuserPrincipal, err := certService.PostgresUser(ctx, c.InstanceID, "pgedge")
	if err != nil {
		return fmt.Errorf("failed to create pgedge postgres user principal: %w", err)
	}
	pgReplicatorPrincipal, err := certService.PostgresUser(ctx, c.InstanceID, "patroni_replicator")
	if err != nil {
		return fmt.Errorf("failed to create patroni_replicator postgres user principal: %w", err)
	}

	c.CaCert = certService.CACert()
	c.ServerCert = pgServerPrincipal.CertPEM
	c.ServerKey = pgServerPrincipal.KeyPEM
	c.SuperuserCert = pgSuperuserPrincipal.CertPEM
	c.SuperuserKey = pgSuperuserPrincipal.KeyPEM
	c.ReplicationCert = pgReplicatorPrincipal.CertPEM
	c.ReplicationKey = pgReplicatorPrincipal.KeyPEM

	if err := fs.MkdirAll(postgresDir, 0o700); err != nil {
		return fmt.Errorf("failed to create postgres certificates directory: %w", err)
	}
	if err := fs.Chown(postgresDir, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for certificates directory: %w", err)
	}

	files := map[string][]byte{
		"ca.crt":          c.CaCert,
		"server.crt":      c.ServerCert,
		"server.key":      c.ServerKey,
		"superuser.crt":   c.SuperuserCert,
		"superuser.key":   c.SuperuserKey,
		"replication.crt": c.ReplicationCert,
		"replication.key": c.ReplicationKey,
	}

	for name, content := range files {
		if err := afero.WriteFile(fs, filepath.Join(postgresDir, name), content, 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
		if err := fs.Chown(filepath.Join(postgresDir, name), c.OwnerUID, c.OwnerGID); err != nil {
			return fmt.Errorf("failed to change ownership for %s: %w", name, err)
		}
	}

	return nil
}

func (c *PostgresCerts) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PostgresCerts) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}
	postgresDir := filepath.Join(parentFullPath, "postgres")

	if err := fs.RemoveAll(postgresDir); err != nil {
		return fmt.Errorf("failed to remove certificates directory: %w", err)
	}

	return nil
}
