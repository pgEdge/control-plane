package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ServiceInstanceSpecResource)(nil)

const ResourceTypeServiceInstanceSpec resource.Type = "swarm.service_instance_spec"

func ServiceInstanceSpecResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceInstanceSpec,
	}
}

type ServiceInstanceSpecResource struct {
	ServiceInstanceID string                `json:"service_instance_id"`
	ServiceSpec       *database.ServiceSpec `json:"service_spec"`
	DatabaseID        string                `json:"database_id"`
	DatabaseName      string                `json:"database_name"`
	HostID            string                `json:"host_id"`
	ServiceName       string                `json:"service_name"`
	Hostname          string                `json:"hostname"`
	CohortMemberID    string                `json:"cohort_member_id"`
	ServiceImage      *ServiceImage         `json:"service_image"`
	Credentials       *database.ServiceUser `json:"credentials"`
	DatabaseNetworkID string                `json:"database_network_id"`
	DatabaseHost      string                `json:"database_host"` // Postgres instance hostname
	DatabasePort      int                   `json:"database_port"` // Postgres instance port
	Port              *int                  `json:"port"`          // Service published port (optional, 0 = random)
	Spec              swarm.ServiceSpec     `json:"spec"`
}

func (s *ServiceInstanceSpecResource) ResourceVersion() string {
	return "1"
}

func (s *ServiceInstanceSpecResource) DiffIgnore() []string {
	return []string{
		"/spec",
	}
}

func (s *ServiceInstanceSpecResource) Identifier() resource.Identifier {
	return ServiceInstanceSpecResourceIdentifier(s.ServiceInstanceID)
}

func (s *ServiceInstanceSpecResource) Executor() resource.Executor {
	return resource.HostExecutor(s.HostID)
}

func (s *ServiceInstanceSpecResource) Dependencies() []resource.Identifier {
	// Service instances depend on the database network existing
	return []resource.Identifier{
		NetworkResourceIdentifier(s.DatabaseNetworkID),
	}
}

func (s *ServiceInstanceSpecResource) Refresh(ctx context.Context, rc *resource.Context) error {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(s.DatabaseNetworkID))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}

	// DatabaseHost and DatabasePort are populated by the ProvisionServices workflow,
	// which prefers a co-located instance for lower latency but falls back to any
	// instance in the database when no local instance exists.
	// TODO: consider alternatives and discuss with the team

	spec, err := ServiceContainerSpec(&ServiceContainerSpecOptions{
		ServiceSpec:       s.ServiceSpec,
		ServiceInstanceID: s.ServiceInstanceID,
		DatabaseID:        s.DatabaseID,
		DatabaseName:      s.DatabaseName,
		HostID:            s.HostID,
		ServiceName:       s.ServiceName,
		Hostname:          s.Hostname,
		CohortMemberID:    s.CohortMemberID,
		ServiceImage:      s.ServiceImage,
		Credentials:       s.Credentials,
		DatabaseNetworkID: network.NetworkID,
		DatabaseHost:      s.DatabaseHost,
		DatabasePort:      s.DatabasePort,
		Port:              s.Port,
	})
	if err != nil {
		return fmt.Errorf("failed to generate service container spec: %w", err)
	}
	s.Spec = spec

	return nil
}

func (s *ServiceInstanceSpecResource) Create(ctx context.Context, rc *resource.Context) error {
	return s.Refresh(ctx, rc)
}

func (s *ServiceInstanceSpecResource) Update(ctx context.Context, rc *resource.Context) error {
	return s.Refresh(ctx, rc)
}

func (s *ServiceInstanceSpecResource) Delete(ctx context.Context, rc *resource.Context) error {
	// This is a virtual resource, so there's nothing to delete.
	return nil
}
