package swarm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ServiceConfigResource)(nil)

const ResourceTypeServiceConfig resource.Type = "swarm.service_config"

// serviceConfigLabel is used to find an existing Swarm config for a service instance.
const serviceConfigLabel = "pgedge.service.instance.id"

func ServiceConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceConfig,
	}
}

// ServiceConfigResource manages the lifecycle of a Docker Swarm config that
// holds the YAML configuration file for a service instance (e.g. the RAG
// server's pgedge-rag-server.yaml).
//
// Docker Swarm configs are immutable once created — the data cannot be changed
// in place. On Update, the old config is deleted and a new one is created.
// The resulting ConfigID is stored in this resource and read by
// ServiceInstanceSpecResource to wire the config reference into the container spec.
type ServiceConfigResource struct {
	ServiceInstanceID string                `json:"service_instance_id"`
	ServiceSpec       *database.ServiceSpec `json:"service_spec"`
	DatabaseID        string                `json:"database_id"`
	DatabaseName      string                `json:"database_name"`
	DatabaseHost      string                `json:"database_host"`
	DatabasePort      int                   `json:"database_port"`
	HostID            string                `json:"host_id"`
	// ConfigID is populated by Create/Refresh and consumed by ServiceInstanceSpecResource.
	ConfigID string `json:"config_id"`
	// CredentialsVersion is a hash of the service user credentials embedded in
	// the config. When ServiceUserRole regenerates credentials (e.g. after a
	// failed workflow), this hash changes and triggers Update() to replace the
	// Swarm config with one containing the new credentials.
	CredentialsVersion string `json:"credentials_version"`
}

// credentialsVersion returns a short hash of username:password used to detect
// credential changes across workflow runs.
// configInUseServiceName extracts the service name from Docker's "config is in
// use by the following service: <name>" error message, returning "" if the
// error does not match that pattern.
func configInUseServiceName(errMsg string) string {
	const marker = "is in use by the following service: "
	idx := strings.Index(errMsg, marker)
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(errMsg[idx+len(marker):])
}

func credentialsVersion(username, password string) string {
	h := sha256.Sum256([]byte(username + ":" + password))
	return fmt.Sprintf("%x", h[:8])
}

func (r *ServiceConfigResource) ResourceVersion() string {
	return "1"
}

func (r *ServiceConfigResource) DiffIgnore() []string {
	// config_id and credentials_version are internal bookkeeping fields managed
	// entirely by Refresh/Create/Update — never part of the desired spec.
	return []string{"/config_id", "/credentials_version"}
}

func (r *ServiceConfigResource) Identifier() resource.Identifier {
	return ServiceConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *ServiceConfigResource) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (r *ServiceConfigResource) Dependencies() []resource.Identifier {
	// ServiceConfigResource only needs the ServiceUserRole to be ready so it
	// can embed the credentials in the YAML.
	return []resource.Identifier{
		ServiceUserRoleIdentifier(r.ServiceInstanceID),
	}
}

func (r *ServiceConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	configs, err := client.ConfigList(ctx, map[string]string{
		serviceConfigLabel: r.ServiceInstanceID,
	})
	if err != nil {
		return fmt.Errorf("failed to list swarm configs: %w", err)
	}
	if len(configs) == 0 {
		return resource.ErrNotFound
	}
	r.ConfigID = configs[0].ID

	// If the credentials have changed since this config was created, the
	// embedded password in the YAML is stale. Return ErrNotFound so Create()
	// is called to replace the config with one containing the new credentials.
	// r.CredentialsVersion holds the hash that was stored when the config was
	// last written (set by Create/Update); we compare it to the current hash.
	userRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(r.ServiceInstanceID))
	if err == nil {
		currentVersion := credentialsVersion(userRole.Username, userRole.Password)
		if r.CredentialsVersion != currentVersion {
			return resource.ErrNotFound
		}
	}
	return nil
}

func (r *ServiceConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.createConfig(ctx, rc)
}

func (r *ServiceConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	// Delete the old config (immutable — must replace to change content).
	if r.ConfigID != "" {
		if err := client.ConfigRemove(ctx, r.ConfigID); err != nil {
			return fmt.Errorf("failed to remove old swarm config: %w", err)
		}
		r.ConfigID = ""
	}

	return r.createConfig(ctx, rc)
}

func (r *ServiceConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	if r.ConfigID == "" {
		return nil
	}

	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	if err := client.ConfigRemove(ctx, r.ConfigID); err != nil {
		logger.Warn().Err(err).
			Str("config_id", r.ConfigID).
			Str("service_instance_id", r.ServiceInstanceID).
			Msg("failed to remove swarm config, continuing")
	}
	return nil
}

func (r *ServiceConfigResource) createConfig(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	// Retrieve credentials generated by ServiceUserRole.
	userRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(r.ServiceInstanceID))
	if err != nil {
		return fmt.Errorf("failed to get service user role from state: %w", err)
	}

	yaml, err := generateRAGConfig(&ragConfigOptions{
		ServiceSpec:  r.ServiceSpec,
		DatabaseHost: r.DatabaseHost,
		DatabasePort: r.DatabasePort,
		DatabaseName: r.DatabaseName,
		Username:     userRole.Username,
		Password:     userRole.Password,
	})
	if err != nil {
		return fmt.Errorf("failed to generate RAG config: %w", err)
	}

	configName := fmt.Sprintf("rag-config-%s", r.ServiceInstanceID)
	configID, err := client.ConfigCreate(ctx, dockerswarm.ConfigSpec{
		Annotations: dockerswarm.Annotations{
			Name: configName,
			Labels: map[string]string{
				serviceConfigLabel:   r.ServiceInstanceID,
				"pgedge.database.id": r.DatabaseID,
				"pgedge.service.id":  r.ServiceSpec.ServiceID,
				"pgedge.component":   "service-config",
			},
		},
		Data: []byte(yaml),
	})
	if err != nil {
		// If a config with this name already exists (e.g. from a previous failed
		// workflow run), delete it and recreate with the current credentials.
		// Swarm configs are immutable so we must replace to pick up new content.
		if strings.Contains(err.Error(), "AlreadyExists") {
			configs, listErr := client.ConfigList(ctx, map[string]string{
				serviceConfigLabel: r.ServiceInstanceID,
			})
			if listErr != nil {
				return fmt.Errorf("failed to create swarm config: %w (also failed to look up existing: %v)", err, listErr)
			}
			for _, cfg := range configs {
				if rmErr := client.ConfigRemove(ctx, cfg.ID); rmErr != nil {
					// Config is still mounted by a running service. Remove the
					// service first so the config becomes free to delete.
					if svcName := configInUseServiceName(rmErr.Error()); svcName != "" {
						if svcRmErr := client.ServiceRemove(ctx, svcName); svcRmErr != nil {
							return fmt.Errorf("failed to create swarm config: %w (also failed to remove service %s holding stale config: %v)", err, svcName, svcRmErr)
						}
						// Retry config removal now that the service is gone.
						if rmErr2 := client.ConfigRemove(ctx, cfg.ID); rmErr2 != nil {
							return fmt.Errorf("failed to create swarm config: %w (also failed to remove stale config %s after service removal: %v)", err, cfg.ID, rmErr2)
						}
					} else {
						return fmt.Errorf("failed to create swarm config: %w (also failed to remove stale config %s: %v)", err, cfg.ID, rmErr)
					}
				}
			}
			// Retry creation after removing the stale config.
			configID, err = client.ConfigCreate(ctx, dockerswarm.ConfigSpec{
				Annotations: dockerswarm.Annotations{
					Name: configName,
					Labels: map[string]string{
						serviceConfigLabel:   r.ServiceInstanceID,
						"pgedge.database.id": r.DatabaseID,
						"pgedge.service.id":  r.ServiceSpec.ServiceID,
						"pgedge.component":   "service-config",
					},
				},
				Data: []byte(yaml),
			})
			if err != nil {
				return fmt.Errorf("failed to create swarm config: %w", err)
			}
			r.ConfigID = configID
			r.CredentialsVersion = credentialsVersion(userRole.Username, userRole.Password)
			return nil
		}
		return fmt.Errorf("failed to create swarm config: %w", err)
	}

	r.ConfigID = configID
	r.CredentialsVersion = credentialsVersion(userRole.Username, userRole.Password)
	return nil
}
