package common

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const (
	postgresCaCertName         = "ca.crt"
	postgresServerCertName     = "server.crt"
	postgresServerKeyName      = "server.key"
	postgresSuperuserCertName  = "superuser.crt"
	postgresSuperuserKeyName   = "superuser.key"
	postgresReplicatorCertName = "replication.crt"
	postgresReplicatorKeyName  = "replication.key"
)

var _ resource.Resource = (*PostgresCerts)(nil)

const ResourceTypePostgresCerts resource.Type = "common.postgres_certs"

func PostgresCertsIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePostgresCerts,
	}
}

type PostgresCerts struct {
	InstanceID        string   `json:"instance_id"`
	HostID            string   `json:"host_id"`
	InstanceAddresses []string `json:"instance_addresses"`
	ParentID          string   `json:"parent_id"`
	OwnerUID          int      `json:"owner_uid"`
	OwnerGID          int      `json:"owner_gid"`
	CaCert            []byte   `json:"ca_cert"`
	ServerCert        []byte   `json:"server_cert"`
	ServerKey         []byte   `json:"server_key"`
	SuperuserCert     []byte   `json:"superuser_cert"`
	SuperuserKey      []byte   `json:"superuser_key"`
	ReplicationCert   []byte   `json:"replication_cert"`
	ReplicationKey    []byte   `json:"replication_key"`
}

func (c *PostgresCerts) ResourceVersion() string {
	return "1"
}

func (c *PostgresCerts) DiffIgnore() []string {
	return []string{
		"/ca_cert",
		"/server_cert",
		"/server_key",
		"/superuser_cert",
		"/superuser_key",
		"/replication_cert",
		"/replication_key",
	}
}

func (c *PostgresCerts) Executor() resource.Executor {
	return resource.HostExecutor(c.HostID)
}

func (c *PostgresCerts) Identifier() resource.Identifier {
	return PostgresCertsIdentifier(c.InstanceID)
}

func (c *PostgresCerts) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
	}
}

func (c *PostgresCerts) TypeDependencies() []resource.Type {
	return nil
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
	certsDir := filepath.Join(parentFullPath, "postgres")

	caCert, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresCaCertName))
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}
	serverCert, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresServerCertName))
	if err != nil {
		return fmt.Errorf("failed to read server cert: %w", err)
	}
	serverKey, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresServerKeyName))
	if err != nil {
		return fmt.Errorf("failed to read server key: %w", err)
	}
	superuserCert, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresSuperuserCertName))
	if err != nil {
		return fmt.Errorf("failed to read superuser cert: %w", err)
	}
	superuserKey, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresSuperuserKeyName))
	if err != nil {
		return fmt.Errorf("failed to read superuser key: %w", err)
	}
	replicationCert, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresReplicatorCertName))
	if err != nil {
		return fmt.Errorf("failed to read replication cert: %w", err)
	}
	replicationKey, err := ReadResourceFile(fs, filepath.Join(certsDir, postgresReplicatorKeyName))
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
	certsDir := filepath.Join(parentFullPath, "postgres")

	// Ensure that localhost is included in the addresses
	combined := ds.NewSet(c.InstanceAddresses...)
	combined.Add("127.0.0.1", "localhost", "::1")

	pgServerPrincipal, err := certService.PostgresServer(ctx,
		c.InstanceID,
		combined.ToSortedSlice(strings.Compare),
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

	if err := fs.MkdirAll(certsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create postgres certificates directory: %w", err)
	}
	if err := fs.Chown(certsDir, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for certificates directory: %w", err)
	}

	files := map[string][]byte{
		postgresCaCertName:         c.CaCert,
		postgresServerCertName:     c.ServerCert,
		postgresServerKeyName:      c.ServerKey,
		postgresSuperuserCertName:  c.SuperuserCert,
		postgresSuperuserKeyName:   c.SuperuserKey,
		postgresReplicatorCertName: c.ReplicationCert,
		postgresReplicatorKeyName:  c.ReplicationKey,
	}

	for name, content := range files {
		path := filepath.Join(certsDir, name)

		if err := afero.WriteFile(fs, path, content, 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		if err := fs.Chown(path, c.OwnerUID, c.OwnerGID); err != nil {
			return fmt.Errorf("failed to change ownership for %s: %w", path, err)
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
	certService, err := do.Invoke[*certificates.Service](rc.Injector)
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

	if err := certService.RemovePostgresUser(ctx, c.InstanceID, "pgedge"); err != nil {
		return fmt.Errorf("failed to remove pgedge postgres user principal: %w", err)
	}
	if err := certService.RemovePostgresUser(ctx, c.InstanceID, "patroni_replicator"); err != nil {
		return fmt.Errorf("failed to remove patroni_replicator postgres user principal: %w", err)
	}
	if err := certService.RemovePostgresServer(ctx, c.InstanceID); err != nil {
		return fmt.Errorf("failed to remove postgres server principal: %w", err)
	}

	return nil
}
