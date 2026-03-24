package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// DataDirPathResolver returns the full path to the data directory for a
// Postgres instance. It is called during Refresh/Create to resolve the mount
// path for the Docker service spec. Different backing stores (filesystem
// DirResource vs ZFS Dataset/Clone) provide different resolvers.
type DataDirPathResolver func(rc *resource.Context) (string, error)

// DirResourceDataDirPathResolver returns a DataDirPathResolver that resolves
// via the filesystem.DirResource path mechanism.
func DirResourceDataDirPathResolver(dirResourceID string) DataDirPathResolver {
	return func(rc *resource.Context) (string, error) {
		return filesystem.DirResourceFullPath(rc, dirResourceID)
	}
}

// StaticDataDirPathResolver returns a DataDirPathResolver that always returns
// the given path. This is used for ZFS-backed data directories where the mount
// point is known at construction time.
func StaticDataDirPathResolver(path string) DataDirPathResolver {
	return func(_ *resource.Context) (string, error) {
		return path, nil
	}
}

var _ resource.Resource = (*PostgresServiceSpecResource)(nil)

const ResourceTypePostgresServiceSpec resource.Type = "swarm.postgres_service_spec"

func PostgresServiceSpecResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePostgresServiceSpec,
	}
}

type PostgresServiceSpecResource struct {
	Instance            *database.InstanceSpec `json:"instance"`
	CohortMemberID      string                 `json:"cohort_member_id"`
	Images              *Images                `json:"images"`
	ServiceName         string                 `json:"service_name"`
	InstanceHostname    string                 `json:"instance_hostname"`
	Spec                swarm.ServiceSpec      `json:"spec"`
	DatabaseNetworkName string                 `json:"database_network_name"`
	DataDirID           string                 `json:"data_dir_id"`
	DataDirPath         string                 `json:"data_dir_path,omitempty"`
	DataDirDep          resource.Identifier    `json:"data_dir_dep"`
	ConfigsDirID        string                 `json:"configs_dir_id"`
	CertificatesDirID   string                 `json:"certificates_dir_id"`

	// ResolveDataDirPath resolves the full path to the data directory.
	// This is injected at construction time to abstract the backing store
	// (DirResource vs ZFS Dataset/Clone).
	ResolveDataDirPath DataDirPathResolver `json:"-"`
}

func (s *PostgresServiceSpecResource) ResourceVersion() string {
	return "1"
}

func (s *PostgresServiceSpecResource) DiffIgnore() []string {
	return []string{
		"/spec",
	}
}

func (s *PostgresServiceSpecResource) Identifier() resource.Identifier {
	return PostgresServiceSpecResourceIdentifier(s.Instance.InstanceID)
}

func (s *PostgresServiceSpecResource) Executor() resource.Executor {
	return resource.HostExecutor(s.Instance.HostID)
}

func (s *PostgresServiceSpecResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		s.DataDirDep,
		filesystem.DirResourceIdentifier(s.ConfigsDirID),
		filesystem.DirResourceIdentifier(s.CertificatesDirID),
		NetworkResourceIdentifier(s.DatabaseNetworkName),
		EtcdCredsIdentifier(s.Instance.InstanceID),
		PostgresCertsIdentifier(s.Instance.InstanceID),
		PatroniConfigIdentifier(s.Instance.InstanceID),
	}
}

func (s *PostgresServiceSpecResource) TypeDependencies() []resource.Type {
	return nil
}

func (s *PostgresServiceSpecResource) Refresh(ctx context.Context, rc *resource.Context) error {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(s.DatabaseNetworkName))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}
	dataPath, err := s.resolveDataDirPath(rc)
	if err != nil {
		return fmt.Errorf("failed to get data dir full path: %w", err)
	}
	configsPath, err := filesystem.DirResourceFullPath(rc, s.ConfigsDirID)
	if err != nil {
		return fmt.Errorf("failed to get configs dir full path: %w", err)
	}
	certsPath, err := filesystem.DirResourceFullPath(rc, s.CertificatesDirID)
	if err != nil {
		return fmt.Errorf("failed to get certificates dir full path: %w", err)
	}

	spec, err := DatabaseServiceSpec(s.Instance, &HostOptions{
		ServiceName:       s.ServiceName,
		InstanceHostname:  s.InstanceHostname,
		DatabaseNetworkID: network.NetworkID,
		Images:            s.Images,
		CohortMemberID:    s.CohortMemberID,
		Paths: Paths{
			Data:         dataPath,
			Configs:      configsPath,
			Certificates: certsPath,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to generate postgres service spec: %w", err)
	}
	s.Spec = spec

	return nil
}

// resolveDataDirPath returns the full path to the data directory. If a custom
// resolver is set, it is used; otherwise, the path is resolved via the
// DirResource mechanism for backwards compatibility.
func (s *PostgresServiceSpecResource) resolveDataDirPath(rc *resource.Context) (string, error) {
	if s.ResolveDataDirPath != nil {
		return s.ResolveDataDirPath(rc)
	}
	if s.DataDirPath != "" {
		return s.DataDirPath, nil
	}
	return filesystem.DirResourceFullPath(rc, s.DataDirID)
}

func (s *PostgresServiceSpecResource) Create(ctx context.Context, rc *resource.Context) error {
	return s.Refresh(ctx, rc)
}

func (s *PostgresServiceSpecResource) Update(ctx context.Context, rc *resource.Context) error {
	return s.Refresh(ctx, rc)
}

func (s *PostgresServiceSpecResource) Delete(ctx context.Context, rc *resource.Context) error {
	// This is a virtual resource, so there's nothing to delete.
	return nil
}
