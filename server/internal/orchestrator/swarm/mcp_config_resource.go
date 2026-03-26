package swarm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*MCPConfigResource)(nil)

const ResourceTypeMCPConfig resource.Type = "swarm.mcp_config"

func MCPConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeMCPConfig,
	}
}

// MCPConfigResource manages the MCP server config files on the host filesystem.
// It follows the same pattern as PatroniConfig: generates config files and writes
// them to a host-side directory that is bind-mounted into the container.
//
// Files managed:
//   - config.yaml: CP-owned, overwritten on every Create/Update
//   - tokens.yaml: Application-owned, written only on first Create if init_token is set
//   - users.yaml: Application-owned, written only on first Create if init_users is set
type MCPConfigResource struct {
	ServiceInstanceID  string                      `json:"service_instance_id"`
	ServiceID          string                      `json:"service_id"`
	HostID             string                      `json:"host_id"`
	DirResourceID      string                      `json:"dir_resource_id"`
	Config             *database.MCPServiceConfig  `json:"config"`
	DatabaseName       string                      `json:"database_name"`
	DatabaseHosts      []database.ServiceHostEntry `json:"database_hosts"`
	TargetSessionAttrs string                      `json:"target_session_attrs"`
	ROUsername         string                      `json:"ro_username"`
	ROPassword         string                      `json:"ro_password"`
	RWUsername         string                      `json:"rw_username"`
	RWPassword         string                      `json:"rw_password"`
}

func (r *MCPConfigResource) ResourceVersion() string {
	return "2"
}

func (r *MCPConfigResource) DiffIgnore() []string {
	return []string{
		// Credentials are populated from ServiceUserRole resources during refresh.
		"/ro_username",
		"/ro_password",
		"/rw_username",
		"/rw_password",
	}
}

func (r *MCPConfigResource) Identifier() resource.Identifier {
	return MCPConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *MCPConfigResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *MCPConfigResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(r.DirResourceID),
		ServiceUserRoleIdentifier(r.ServiceID, ServiceUserRoleRO),
		ServiceUserRoleIdentifier(r.ServiceID, ServiceUserRoleRW),
	}
}

func (r *MCPConfigResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *MCPConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	// Check if config.yaml exists
	_, err = readResourceFile(fs, filepath.Join(dirPath, "config.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
	}

	return nil
}

func (r *MCPConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	// Populate credentials from ServiceUserRole
	if err := r.populateCredentials(rc); err != nil {
		return err
	}

	// Generate and write config.yaml (always)
	if err := r.writeConfigFile(fs, dirPath); err != nil {
		return err
	}

	// Write token file (only if it doesn't exist yet)
	tokensPath := filepath.Join(dirPath, "tokens.yaml")
	if err := r.writeTokenFileIfNeeded(fs, tokensPath); err != nil {
		return err
	}

	// Write user file (only if it doesn't exist yet)
	usersPath := filepath.Join(dirPath, "users.yaml")
	if err := r.writeUserFileIfNeeded(fs, usersPath); err != nil {
		return err
	}

	return nil
}

func (r *MCPConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	// Populate credentials from ServiceUserRole
	if err := r.populateCredentials(rc); err != nil {
		return err
	}

	// Overwrite config.yaml (CP-owned, always regenerated)
	if err := r.writeConfigFile(fs, dirPath); err != nil {
		return err
	}

	// Do NOT touch tokens.yaml or users.yaml — they are application-owned

	// Signal the running MCP container to reload config.
	// This is a best-effort operation — if the container isn't running yet
	// (e.g., initial creation) or is restarting, we log and move on.
	// The config file is already correct on disk; the container will pick
	// it up on its next start.
	if err := r.signalConfigReload(ctx, rc); err != nil {
		return err
	}

	return nil
}

func (r *MCPConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the parent directory resource deletion
	return nil
}

// activeCredentials returns the username and password for the active service
// user based on the AllowWrites config setting.
func (r *MCPConfigResource) activeCredentials() (username, password string) {
	if r.Config.AllowWrites != nil && *r.Config.AllowWrites {
		return r.RWUsername, r.RWPassword
	}
	return r.ROUsername, r.ROPassword
}

// writeConfigFile generates and writes the config.yaml file.
func (r *MCPConfigResource) writeConfigFile(fs afero.Fs, dirPath string) error {
	username, password := r.activeCredentials()
	content, err := GenerateMCPConfig(&MCPConfigParams{
		Config:             r.Config,
		DatabaseName:       r.DatabaseName,
		DatabaseHosts:      r.DatabaseHosts,
		TargetSessionAttrs: r.TargetSessionAttrs,
		Username:           username,
		Password:           password,
	})
	if err != nil {
		return fmt.Errorf("failed to generate MCP config: %w", err)
	}

	configPath := filepath.Join(dirPath, "config.yaml")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	// Chown to MCP user
	if err := fs.Chown(configPath, mcpContainerUID, mcpContainerUID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}
	return nil
}

// writeTokenFileIfNeeded writes tokens.yaml only if the file doesn't exist yet.
func (r *MCPConfigResource) writeTokenFileIfNeeded(fs afero.Fs, tokensPath string) error {
	exists, err := afero.Exists(fs, tokensPath)
	if err != nil {
		return fmt.Errorf("failed to check if tokens.yaml exists: %w", err)
	}
	if exists {
		return nil // Preserve existing application-owned token store
	}

	var content []byte
	if r.Config.InitToken != nil {
		content, err = GenerateTokenFile(*r.Config.InitToken)
		if err != nil {
			return fmt.Errorf("failed to generate token file: %w", err)
		}
	} else {
		content, err = GenerateEmptyTokenFile()
		if err != nil {
			return fmt.Errorf("failed to generate empty token file: %w", err)
		}
	}

	if err := afero.WriteFile(fs, tokensPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", tokensPath, err)
	}
	if err := fs.Chown(tokensPath, mcpContainerUID, mcpContainerUID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", tokensPath, err)
	}
	return nil
}

// writeUserFileIfNeeded writes users.yaml only if the file doesn't exist yet.
func (r *MCPConfigResource) writeUserFileIfNeeded(fs afero.Fs, usersPath string) error {
	exists, err := afero.Exists(fs, usersPath)
	if err != nil {
		return fmt.Errorf("failed to check if users.yaml exists: %w", err)
	}
	if exists {
		return nil // Preserve existing application-owned user store
	}

	var content []byte
	if len(r.Config.InitUsers) > 0 {
		content, err = GenerateUserFile(r.Config.InitUsers)
		if err != nil {
			return fmt.Errorf("failed to generate user file: %w", err)
		}
	} else {
		content, err = GenerateEmptyUserFile()
		if err != nil {
			return fmt.Errorf("failed to generate empty user file: %w", err)
		}
	}

	if err := afero.WriteFile(fs, usersPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", usersPath, err)
	}
	if err := fs.Chown(usersPath, mcpContainerUID, mcpContainerUID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", usersPath, err)
	}
	return nil
}

// signalConfigReload sends SIGHUP to the running MCP container to trigger
// a config reload. Signal delivery failures are logged as warnings and
// return nil — the config file is already correct on disk and will be
// picked up on the next container restart. Injector failures are returned
// as errors since they indicate a systemic problem.
func (r *MCPConfigResource) signalConfigReload(ctx context.Context, rc *resource.Context) error {
	dockerClient, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return fmt.Errorf("failed to get docker client: %w", err)
	}

	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return fmt.Errorf("failed to get logger: %w", err)
	}

	container, err := GetServiceContainer(ctx, dockerClient, r.ServiceInstanceID)
	if err != nil {
		if errors.Is(err, ErrNoServiceContainer) {
			logger.Debug().Msg("no running MCP container found, skipping config reload signal")
			return nil
		}
		logger.Warn().Err(err).Msg("failed to find service container for config reload signal")
		return nil
	}

	if err := dockerClient.ContainerSignal(ctx, container.ID, "SIGHUP"); err != nil {
		logger.Warn().Err(err).
			Str("container_id", container.ID).
			Msg("failed to send SIGHUP to MCP container")
		return nil
	}

	logger.Info().
		Str("container_id", container.ID).
		Msg("sent SIGHUP to MCP container for config reload")

	return nil
}

// populateCredentials fetches credentials from both ServiceUserRole resources
// (RO and RW). Credential selection happens at usage time based on AllowWrites.
func (r *MCPConfigResource) populateCredentials(rc *resource.Context) error {
	roRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(r.ServiceID, ServiceUserRoleRO))
	if err != nil {
		return fmt.Errorf("failed to get RO service user role from state: %w", err)
	}
	r.ROUsername = roRole.Username
	r.ROPassword = roRole.Password

	rwRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(r.ServiceID, ServiceUserRoleRW))
	if err != nil {
		return fmt.Errorf("failed to get RW service user role from state: %w", err)
	}
	r.RWUsername = rwRole.Username
	r.RWPassword = rwRole.Password

	return nil
}
