package common

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgRESTConfigResource)(nil)

const ResourceTypePostgRESTConfig resource.Type = "common.postgrest_config"

// postgrestNonReloadableKeys are the postgrest.conf settings that PostgREST
// only reads at startup. Changes to them require a restart rather than a
// config reload.
//
// See: https://docs.postgrest.org/en/stable/references/configuration.html
var postgrestNonReloadableKeys = []string{
	"db-uri",
	"db-pool",
	"server-cors-allowed-origins",
}

func PostgRESTConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypePostgRESTConfig,
	}
}

// PostgRESTConfigResource manages the postgrest.conf file on the host
// filesystem. The file includes the db-uri with embedded credentials from the
// connect_as database user.
//
// On update, the running service instance is made to pick up the new file:
// it's restarted if a setting that PostgREST only reads at startup changed,
// and otherwise sent SIGUSR2 to reload its configuration and SIGUSR1 to
// reload its schema cache. It's also restarted when the configuration it's
// running with is unknown: after a failed update, or after the file was
// changed outside the Control Plane.
type PostgRESTConfigResource struct {
	ServiceInstanceID  string                           `json:"service_instance_id"`
	ServiceID          string                           `json:"service_id"`
	HostID             string                           `json:"host_id"`
	DirResourceID      string                           `json:"dir_resource_id"`
	Config             *database.PostgRESTServiceConfig `json:"config"`
	ConnectAsUsername  string                           `json:"connect_as_username"`
	ConnectAsPassword  string                           `json:"connect_as_password"`
	DatabaseName       string                           `json:"database_name"`
	DatabaseHosts      []database.ServiceHostEntry      `json:"database_hosts"`
	TargetSessionAttrs string                           `json:"target_session_attrs,omitempty"`
	OwnerUID           int                              `json:"owner_uid"`
	OwnerGID           int                              `json:"owner_gid"`
	// ConfigDrifted is set by Refresh when the file on disk no longer matches
	// this resource, so that the next plan updates it.
	ConfigDrifted bool `json:"config_drifted,omitempty"`
}

func (r *PostgRESTConfigResource) ResourceVersion() string {
	return "1"
}

func (r *PostgRESTConfigResource) DiffIgnore() []string {
	return nil
}

func (r *PostgRESTConfigResource) Identifier() resource.Identifier {
	return PostgRESTConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *PostgRESTConfigResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *PostgRESTConfigResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(r.DirResourceID),
	}
}

func (r *PostgRESTConfigResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *PostgRESTConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	onDisk, err := ReadResourceFile(fs, filepath.Join(dirPath, "postgrest.conf"))
	if err != nil {
		return fmt.Errorf("failed to read PostgREST config: %w", err)
	}
	expected, err := r.generateConf()
	if err != nil {
		return err
	}
	r.ConfigDrifted = !bytes.Equal(onDisk, expected)

	return nil
}

func (r *PostgRESTConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	return r.writeConfigFile(fs, dirPath)
}

func (r *PostgRESTConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	var previous *PostgRESTConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(r.Identifier()); ok {
		prev, err := resource.ToResource[*PostgRESTConfigResource](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous PostgREST config resource: %w", err)
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
// configuration that the service is running with is unknown.
func (r *PostgRESTConfigResource) UpdateFrom(
	ctx context.Context,
	rc *resource.Context,
	previous *PostgRESTConfigResource,
	previousFailed bool,
) error {
	if err := r.Create(ctx, rc); err != nil {
		return err
	}

	var modes []database.ReloadMode
	if previous != nil && (previousFailed || previous.ConfigDrifted) {
		// A failed update may have written the file without applying it, and
		// a drifted file may have been loaded, so restart to be sure that the
		// service runs with the current file.
		modes = []database.ReloadMode{database.ReloadModeRestart}
	} else {
		var previousConf []byte
		if previous != nil {
			var err error
			previousConf, err = previous.generateConf()
			if err != nil {
				return err
			}
		}
		current, err := r.generateConf()
		if err != nil {
			return err
		}
		modes = postgrestReloadModes(previousConf, current)
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
			Msg("reloading PostgREST service instance for config change")
		if err := orch.ReloadServiceInstance(ctx, r.ServiceInstanceID, mode); err != nil {
			return fmt.Errorf("failed to reload PostgREST service instance: %w", err)
		}
	}

	return nil
}

func (r *PostgRESTConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the parent directory resource deletion.
	return nil
}

func (r *PostgRESTConfigResource) generateConf() ([]byte, error) {
	content, err := r.Config.GenerateConf(database.PostgRESTConnParams{
		Username:           r.ConnectAsUsername,
		Password:           r.ConnectAsPassword,
		DatabaseName:       r.DatabaseName,
		DatabaseHosts:      r.DatabaseHosts,
		TargetSessionAttrs: r.TargetSessionAttrs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate PostgREST config: %w", err)
	}
	return content, nil
}

func (r *PostgRESTConfigResource) writeConfigFile(fs afero.Fs, dirPath string) error {
	content, err := r.generateConf()
	if err != nil {
		return err
	}

	configPath := filepath.Join(dirPath, "postgrest.conf")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, r.OwnerUID, r.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return nil
}

// postgrestReloadModes returns how a running PostgREST must be reloaded to
// apply the change from the previous to the current postgrest.conf. It
// returns nil if there's no previous config, since the service reads the
// current file when it starts, or if nothing changed.
func postgrestReloadModes(previous, current []byte) []database.ReloadMode {
	if previous == nil || bytes.Equal(previous, current) {
		return nil
	}
	prev := parsePostgRESTConf(previous)
	curr := parsePostgRESTConf(current)
	for _, key := range postgrestNonReloadableKeys {
		if prev[key] != curr[key] {
			return []database.ReloadMode{database.ReloadModeRestart}
		}
	}
	if maps.Equal(prev, curr) {
		return nil
	}
	// SIGUSR2 reloads the configuration. Settings such as db-schemas and
	// db-anon-role also affect the schema cache, so we reload that as well.
	return []database.ReloadMode{
		database.ReloadModeSigusr2,
		database.ReloadModeSigusr1,
	}
}

// parsePostgRESTConf parses the "key = value" lines of a postgrest.conf.
func parsePostgRESTConf(content []byte) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(string(content), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}
