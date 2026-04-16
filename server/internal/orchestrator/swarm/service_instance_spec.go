package swarm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/docker/docker/api/types/swarm"
	"github.com/rs/zerolog/log"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
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
	ServiceInstanceID  string                      `json:"service_instance_id"`
	ServiceSpec        *database.ServiceSpec       `json:"service_spec"`
	DatabaseID         string                      `json:"database_id"`
	DatabaseName       string                      `json:"database_name"`
	HostID             string                      `json:"host_id"`
	ServiceName        string                      `json:"service_name"`
	Hostname           string                      `json:"hostname"`
	CohortMemberID     string                      `json:"cohort_member_id"`
	ServiceImage       *ServiceImage               `json:"service_image"`
	Credentials        *database.ServiceUser       `json:"credentials"`
	DatabaseNetworkID  string                      `json:"database_network_id"`
	DatabaseHosts      []database.ServiceHostEntry `json:"database_hosts"`       // Ordered Postgres host:port entries
	TargetSessionAttrs string                      `json:"target_session_attrs"` // libpq target_session_attrs
	Port               *int                        `json:"port"`                 // Service published port (optional, 0 = random)
	DataDirID          string                      `json:"data_dir_id"`          // DirResource ID for the service data directory
	Spec               swarm.ServiceSpec           `json:"spec"`
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
	deps := []resource.Identifier{
		NetworkResourceIdentifier(s.DatabaseNetworkID),
	}

	// MCP and RAG get credentials from database_users (connect_as) —
	// no ServiceUserRole dependency. PostgREST still uses ServiceUserRole.
	if s.ServiceSpec.ServiceType == "postgrest" {
		deps = append(deps, ServiceUserRoleIdentifier(s.ServiceSpec.ServiceID, ServiceUserRoleRO))
		deps = append(deps, ServiceUserRoleIdentifier(s.ServiceSpec.ServiceID, ServiceUserRoleRW))
	}

	switch s.ServiceSpec.ServiceType {
	case "mcp":
		deps = append(deps, MCPConfigResourceIdentifier(s.ServiceInstanceID))
	case "postgrest":
		deps = append(deps, PostgRESTConfigResourceIdentifier(s.ServiceInstanceID))
	case "rag":
		deps = append(deps,
			RAGConfigResourceIdentifier(s.ServiceInstanceID),
			RAGServiceKeysResourceIdentifier(s.ServiceInstanceID),
		)
	default:
		log.Warn().Str("service_type", s.ServiceSpec.ServiceType).Msg("unknown service type in dependencies")
	}
	return deps
}

func (s *ServiceInstanceSpecResource) TypeDependencies() []resource.Type {
	return nil
}

func (s *ServiceInstanceSpecResource) populateCredentials(rc *resource.Context) error {
	// MCP and RAG source credentials from database_users (connect_as).
	// RAG credentials go into the config file via RAGConfigResource, not the
	// container spec, so s.Credentials remains nil for RAG regardless.
	// Clear any stale credentials that may have been persisted by the legacy
	// ServiceUserRole path before this migration.
	if s.ServiceSpec.ServiceType == "mcp" || s.ServiceSpec.ServiceType == "rag" {
		s.Credentials = nil
		return nil
	}

	// PostgREST authenticates to Postgres as the RW service user (NOINHERIT,
	// granted the anon role). All other service types use the RO service user.
	mode := ServiceUserRoleRO
	role := "pgedge_application_read_only"
	if s.ServiceSpec.ServiceType == "postgrest" {
		mode = ServiceUserRoleRW
		role = "postgrest_authenticator"
	}
	userRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(s.ServiceSpec.ServiceID, mode))
	if err != nil {
		return fmt.Errorf("failed to get service user role from state: %w", err)
	}
	s.Credentials = &database.ServiceUser{
		Username: userRole.Username,
		Password: userRole.Password,
		Role:     role,
	}
	return nil
}

func (s *ServiceInstanceSpecResource) Refresh(ctx context.Context, rc *resource.Context) error {
	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(s.DatabaseNetworkID))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}

	// Populate credentials from the ServiceUserRole resource
	if err := s.populateCredentials(rc); err != nil {
		return err
	}

	// Resolve the data directory path from the DirResource (only if one exists).
	var dataPath string
	if s.DataDirID != "" {
		dataPath, err = filesystem.DirResourceFullPath(rc, s.DataDirID)
		if err != nil {
			return fmt.Errorf("failed to get service data dir path: %w", err)
		}
	}

	// Resolve the keys directory path (RAG only): it lives at {dataPath}/keys.
	var keysPath string
	if s.ServiceSpec.ServiceType == "rag" {
		keysPath = filepath.Join(dataPath, "keys")
	}

	spec, err := ServiceContainerSpec(&ServiceContainerSpecOptions{
		ServiceSpec:        s.ServiceSpec,
		ServiceInstanceID:  s.ServiceInstanceID,
		DatabaseID:         s.DatabaseID,
		DatabaseName:       s.DatabaseName,
		HostID:             s.HostID,
		ServiceName:        s.ServiceName,
		Hostname:           s.Hostname,
		CohortMemberID:     s.CohortMemberID,
		ServiceImage:       s.ServiceImage,
		Credentials:        s.Credentials,
		DatabaseNetworkID:  network.NetworkID,
		DatabaseHosts:      s.DatabaseHosts,
		TargetSessionAttrs: s.TargetSessionAttrs,
		Port:               s.Port,
		DataPath:           dataPath,
		KeysPath:           keysPath,
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
