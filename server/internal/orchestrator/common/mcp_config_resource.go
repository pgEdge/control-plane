package common

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*MCPConfigResource)(nil)

const ResourceTypeMCPConfig resource.Type = "common.mcp_config"

const (
	mcpConfigFileName = "config.yaml"
	mcpTokenFileName  = "tokens.yaml"
	mcpUserFileName   = "users.yaml"
)

func MCPConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeMCPConfig,
	}
}

// MCPConfigResource manages the MCP server's files on the host filesystem:
//
//   - config.yaml in the config directory: CP-owned, overwritten on every
//     Create/Update
//   - tokens.yaml and users.yaml in the data directory: application-owned,
//     written only if they don't exist yet, from init_token and init_users
//
// The config and data directories may be the same directory. Paths holds the
// locations as seen by the MCP server, which differ from the host paths when
// the server runs in a container.
//
// On update, the running service instance is made to pick up the new
// config.yaml: it's sent SIGHUP if only the settings that the server applies
// on SIGHUP changed, and otherwise restarted. It's also restarted when the
// configuration it's running with is unknown: after a failed update, or after
// the file was changed outside the Control Plane.
type MCPConfigResource struct {
	ServiceInstanceID   string                      `json:"service_instance_id"`
	ServiceID           string                      `json:"service_id"`
	HostID              string                      `json:"host_id"`
	ConfigDirResourceID string                      `json:"config_dir_resource_id"`
	DataDirResourceID   string                      `json:"data_dir_resource_id"`
	Config              *database.MCPServiceConfig  `json:"config"`
	DatabaseName        string                      `json:"database_name"`
	DatabaseHosts       []database.ServiceHostEntry `json:"database_hosts"`
	TargetSessionAttrs  string                      `json:"target_session_attrs"`
	ConnectAsUsername   string                      `json:"connect_as_username"`
	ConnectAsPassword   string                      `json:"connect_as_password"`
	Paths               MCPConfigPaths              `json:"paths"`
	// KBHostPath is the full path to the KB SQLite file on the host. When
	// non-empty, Create and Update verify that the file exists and that the
	// owner below can read it before allowing deployment to proceed.
	KBHostPath string `json:"kb_host_path,omitempty"`
	OwnerUID   int    `json:"owner_uid"`
	OwnerGID   int    `json:"owner_gid"`
	// ConfigDrifted is set by Refresh when config.yaml on disk no longer
	// matches this resource, so that the next plan updates it.
	ConfigDrifted bool `json:"config_drifted,omitempty"`
}

func (r *MCPConfigResource) ResourceVersion() string {
	return "1"
}

func (r *MCPConfigResource) DiffIgnore() []string {
	return nil
}

func (r *MCPConfigResource) Identifier() resource.Identifier {
	return MCPConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *MCPConfigResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *MCPConfigResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		filesystem.DirResourceIdentifier(r.ConfigDirResourceID),
	}
	if r.DataDirResourceID != r.ConfigDirResourceID {
		deps = append(deps, filesystem.DirResourceIdentifier(r.DataDirResourceID))
	}
	return deps
}

func (r *MCPConfigResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *MCPConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	configDir, err := filesystem.DirResourceFullPath(rc, r.ConfigDirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get MCP config dir path: %w", err)
	}

	// ErrNotFound here triggers Create.
	onDisk, err := ReadResourceFile(afs, filepath.Join(configDir, mcpConfigFileName))
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
	}
	expected, err := r.generateConfig()
	if err != nil {
		return err
	}
	r.ConfigDrifted = !bytes.Equal(onDisk, expected)

	return nil
}

func (r *MCPConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	if err := r.checkKBFile(afs); err != nil {
		return err
	}

	configDir, err := filesystem.DirResourceFullPath(rc, r.ConfigDirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get MCP config dir path: %w", err)
	}
	dataDir, err := filesystem.DirResourceFullPath(rc, r.DataDirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get MCP data dir path: %w", err)
	}

	content, err := r.generateConfig()
	if err != nil {
		return err
	}
	if err := r.writeFile(afs, filepath.Join(configDir, mcpConfigFileName), content); err != nil {
		return err
	}

	// The token and user stores are application-owned. The server only
	// watches them for changes if they exist when it starts, so they're
	// always created, empty when there's nothing to bootstrap.
	tokens, err := r.tokenFile()
	if err != nil {
		return err
	}
	if err := r.writeFileIfNotExists(afs, filepath.Join(dataDir, mcpTokenFileName), tokens); err != nil {
		return err
	}
	users, err := r.userFile()
	if err != nil {
		return err
	}
	if err := r.writeFileIfNotExists(afs, filepath.Join(dataDir, mcpUserFileName), users); err != nil {
		return err
	}

	return nil
}

func (r *MCPConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	var previous *MCPConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(r.Identifier()); ok {
		prev, err := resource.ToResource[*MCPConfigResource](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous MCP config resource: %w", err)
		}
		previous = prev
		previousFailed = stored.Error != ""
	}
	return r.UpdateFrom(ctx, rc, previous, previousFailed)
}

// UpdateFrom writes the config file and reloads the running service instance
// as needed to apply the change from the given previous resource. Resources
// that wrap this one use it to pass the previous resource from their own
// state. previous may be nil if there's no prior state. previousFailed
// indicates that the previous resource's last operation failed, so the
// configuration that the server is running with is unknown.
func (r *MCPConfigResource) UpdateFrom(
	ctx context.Context,
	rc *resource.Context,
	previous *MCPConfigResource,
	previousFailed bool,
) error {
	if err := r.Create(ctx, rc); err != nil {
		return err
	}

	var modes []database.ReloadMode
	if previous != nil && (previousFailed || previous.ConfigDrifted) {
		// A failed update may have written the file without applying it, and
		// a drifted file may have been loaded, so restart to be sure that the
		// server runs with the current file.
		modes = []database.ReloadMode{database.ReloadModeRestart}
	} else if previous != nil {
		previousConfig, err := previous.generateConfig()
		if err != nil {
			return err
		}
		current, err := r.generateConfig()
		if err != nil {
			return err
		}
		modes, err = mcpReloadModes(previousConfig, current)
		if err != nil {
			return err
		}
	}
	if len(modes) == 0 {
		return nil
	}

	orch, err := do.Invoke[database.Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	for _, mode := range modes {
		logger.Info().
			Str("service_instance_id", r.ServiceInstanceID).
			Stringer("mode", mode).
			Msg("reloading MCP service instance for config change")
		if err := orch.ReloadServiceInstance(ctx, r.ServiceInstanceID, mode); err != nil {
			return fmt.Errorf("failed to reload MCP service instance: %w", err)
		}
	}

	return nil
}

func (r *MCPConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the directory resources' deletion.
	return nil
}

func (r *MCPConfigResource) generateConfig() ([]byte, error) {
	content, err := GenerateMCPConfig(&MCPConfigParams{
		Config:             r.Config,
		DatabaseName:       r.DatabaseName,
		DatabaseHosts:      r.DatabaseHosts,
		TargetSessionAttrs: r.TargetSessionAttrs,
		Username:           r.ConnectAsUsername,
		Password:           r.ConnectAsPassword,
		Paths:              r.Paths,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate MCP config: %w", err)
	}
	return content, nil
}

func (r *MCPConfigResource) tokenFile() ([]byte, error) {
	if r.Config.InitToken != nil {
		content, err := GenerateTokenFile(*r.Config.InitToken)
		if err != nil {
			return nil, fmt.Errorf("failed to generate token file: %w", err)
		}
		return content, nil
	}
	content, err := GenerateEmptyTokenFile()
	if err != nil {
		return nil, fmt.Errorf("failed to generate empty token file: %w", err)
	}
	return content, nil
}

func (r *MCPConfigResource) userFile() ([]byte, error) {
	if len(r.Config.InitUsers) > 0 {
		content, err := GenerateUserFile(r.Config.InitUsers)
		if err != nil {
			return nil, fmt.Errorf("failed to generate user file: %w", err)
		}
		return content, nil
	}
	content, err := GenerateEmptyUserFile()
	if err != nil {
		return nil, fmt.Errorf("failed to generate empty user file: %w", err)
	}
	return content, nil
}

func (r *MCPConfigResource) writeFile(afs afero.Fs, path string, content []byte) error {
	if err := afero.WriteFile(afs, path, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := afs.Chown(path, r.OwnerUID, r.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", path, err)
	}
	return nil
}

func (r *MCPConfigResource) writeFileIfNotExists(afs afero.Fs, path string, content []byte) error {
	exists, err := afero.Exists(afs, path)
	if err != nil {
		return fmt.Errorf("failed to check if %s exists: %w", path, err)
	}
	if exists {
		return nil
	}
	return r.writeFile(afs, path, content)
}

// checkKBFile blocks deployment when kb_enabled is set but the host KB file
// is missing, is not a regular file, or can't be read by the MCP server's
// user. The Control Plane never changes the ownership or permissions of a
// user-staged KB file. Called from Create and Update, not Refresh, because
// Refresh is only invoked for resources already in state, so a check there
// would not fire on first deploy.
func (r *MCPConfigResource) checkKBFile(afs afero.Fs) error {
	if r.KBHostPath == "" {
		return nil
	}
	info, err := afs.Stat(r.KBHostPath)
	if err != nil {
		if exists, existsErr := afero.Exists(afs, r.KBHostPath); existsErr == nil && !exists {
			return fmt.Errorf("KB database file not found at %s — stage the file on the host before deploying with kb_enabled: true", r.KBHostPath)
		}
		return fmt.Errorf("failed to check KB database file at %s: %w", r.KBHostPath, err)
	}
	// A directory passes the existence check above but cannot be opened as a
	// SQLite database, which would only surface as a confusing error at query
	// time. Reject it here so the failure is clear at deploy time.
	if info.IsDir() {
		return fmt.Errorf("KB database path %s is a directory, not a file — kb_database_host_path must point to the KB SQLite file", r.KBHostPath)
	}
	if !readableBy(info, r.OwnerUID, r.OwnerGID) {
		return fmt.Errorf("KB database file %s is not readable by uid %d / gid %d — grant the MCP server's user read access to it", r.KBHostPath, r.OwnerUID, r.OwnerGID)
	}
	return nil
}

// readableBy reports whether the file's permission bits let the given user
// and group read it. Supplementary groups aren't considered. It returns true
// when the file's ownership isn't available, e.g. on an in-memory filesystem.
func readableBy(info fs.FileInfo, uid, gid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	perm := info.Mode().Perm()
	switch {
	case uid == 0:
		return true
	case int(st.Uid) == uid:
		return perm&0o400 != 0
	case int(st.Gid) == gid:
		return perm&0o040 != 0
	default:
		return perm&0o004 != 0
	}
}
