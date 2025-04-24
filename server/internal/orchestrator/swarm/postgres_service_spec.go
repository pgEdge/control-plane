package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgresServiceSpecResource)(nil)

const ResourceTypePostgresServiceSpec resource.Type = "swarm.postgres_service_spec"

func PostgresServiceSpecResourceIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypePostgresServiceSpec,
	}
}

type PostgresServiceSpecResource struct {
	Instance            *database.InstanceSpec `json:"instance"`
	CohortMemberID      string                 `json:"cohort_member_id"`
	Images              *Images                `json:"images"`
	ServiceName         string                 `json:"service_name"`
	Spec                swarm.ServiceSpec      `json:"spec"`
	DatabaseNetworkName string                 `json:"database_network_name"`
	DataDirID           string                 `json:"data_dir_id"`
	ConfigsDirID        string                 `json:"configs_dir_id"`
	CertificatesDirID   string                 `json:"certificates_dir_id"`
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
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   s.Instance.HostID.String(),
	}
}

func (s *PostgresServiceSpecResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(s.DataDirID),
		filesystem.DirResourceIdentifier(s.ConfigsDirID),
		filesystem.DirResourceIdentifier(s.CertificatesDirID),
		NetworkResourceIdentifier(s.DatabaseNetworkName),
		EtcdCredsIdentifier(s.Instance.InstanceID),
		PostgresCertsIdentifier(s.Instance.InstanceID),
		PatroniConfigIdentifier(s.Instance.InstanceID),
	}
}

func (s *PostgresServiceSpecResource) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	service, err := client.ServiceInspect(ctx, s.Instance.Hostname())
	if errors.Is(err, docker.ErrNotFound) {
		return resource.ErrNotFound
	}
	s.Spec = service.Spec

	return nil
}

func (s *PostgresServiceSpecResource) Create(ctx context.Context, rc *resource.Context) error {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(s.DatabaseNetworkName))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}
	dataPath, err := filesystem.DirResourceFullPath(rc, s.DataDirID)
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

func (s *PostgresServiceSpecResource) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}
func (s *PostgresServiceSpecResource) Delete(ctx context.Context, rc *resource.Context) error {
	// This is a virtual resource, so there's nothing to delete.
	return nil
}
