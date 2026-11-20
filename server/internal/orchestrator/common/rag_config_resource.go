package common

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*RAGConfigResource)(nil)

const ResourceTypeRAGConfig resource.Type = "common.rag_config"

// RAGConfigFileName is the config file name expected by pgedge-rag-server.
const RAGConfigFileName = "pgedge-rag-server.yaml"

func RAGConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGConfig,
	}
}

// RAGConfigResource manages pgedge-rag-server.yaml in the given directory. On
// every Create or Update the file is regenerated from the current
// RAGServiceConfig and the connect_as credentials.
//
// On update, the running service instance is restarted when the config file
// or the API keys changed, so that the server runs with the new configuration
// whether or not it reloads changed files by itself. It's also restarted when
// the configuration it's running with is unknown: after a failed update, or
// after the file was changed outside the Control Plane.
type RAGConfigResource struct {
	ServiceInstanceID string                     `json:"service_instance_id"`
	ServiceID         string                     `json:"service_id"`
	HostID            string                     `json:"host_id"`
	DirResourceID     string                     `json:"dir_resource_id"`
	Config            *database.RAGServiceConfig `json:"config"`
	DatabaseName      string                     `json:"database_name"`
	DatabaseHost      string                     `json:"database_host"`
	DatabasePort      int                        `json:"database_port"`
	ConnectAsUsername string                     `json:"connect_as_username"`
	ConnectAsPassword string                     `json:"connect_as_password"`
	// KeysDir is the directory where the server reads its API key files, as
	// seen by the server.
	KeysDir string `json:"keys_dir"`
	// ListenAddress and Port are the address and port that the server
	// listens on.
	ListenAddress string `json:"listen_address"`
	Port          int    `json:"port"`
	OwnerUID      int    `json:"owner_uid"`
	OwnerGID      int    `json:"owner_gid"`
	// ConfigDrifted is set by Refresh when the file on disk no longer matches
	// this resource, so that the next plan updates it.
	ConfigDrifted bool `json:"config_drifted,omitempty"`
}

func (r *RAGConfigResource) ResourceVersion() string {
	return "1"
}

func (r *RAGConfigResource) DiffIgnore() []string {
	return nil
}

func (r *RAGConfigResource) Identifier() resource.Identifier {
	return RAGConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGConfigResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *RAGConfigResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(r.DirResourceID),
		RAGServiceKeysResourceIdentifier(r.ServiceInstanceID),
		RAGPreflightResourceIdentifier(r.ServiceInstanceID),
	}
}

func (r *RAGConfigResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *RAGConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	path, err := r.configPath(rc)
	if err != nil {
		return err
	}

	// ErrNotFound here triggers Create.
	onDisk, err := ReadResourceFile(afs, path)
	if err != nil {
		return fmt.Errorf("failed to read RAG config: %w", err)
	}
	expected, err := r.generateConfig()
	if err != nil {
		return err
	}
	r.ConfigDrifted = !bytes.Equal(onDisk, expected)

	return nil
}

func (r *RAGConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	path, err := r.configPath(rc)
	if err != nil {
		return err
	}
	content, err := r.generateConfig()
	if err != nil {
		return err
	}
	return r.writeFile(afs, path, content)
}

func (r *RAGConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	var previous *RAGConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(r.Identifier()); ok {
		prev, err := resource.ToResource[*RAGConfigResource](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous RAG config resource: %w", err)
		}
		previous = prev
		previousFailed = stored.Error != ""
	}
	return r.UpdateFrom(ctx, rc, previous, previousFailed)
}

// UpdateFrom writes the config file and restarts the running service
// instance if needed to apply the change from the given previous resource.
// Resources that wrap this one use it to pass the previous resource from
// their own state. previous may be nil if there's no prior state.
// previousFailed indicates that the previous resource's last operation
// failed, so the configuration that the server is running with is unknown.
func (r *RAGConfigResource) UpdateFrom(
	ctx context.Context,
	rc *resource.Context,
	previous *RAGConfigResource,
	previousFailed bool,
) error {
	if err := r.Create(ctx, rc); err != nil {
		return err
	}
	if previous == nil {
		return nil
	}

	restart := previousFailed || previous.ConfigDrifted
	if !restart {
		changed, err := r.changedFrom(previous)
		if err != nil {
			return err
		}
		restart = changed
	}
	if !restart {
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
	logger.Info().
		Str("service_instance_id", r.ServiceInstanceID).
		Msg("restarting RAG service instance for config change")
	if err := orch.ReloadServiceInstance(ctx, r.ServiceInstanceID, database.ReloadModeRestart); err != nil {
		return fmt.Errorf("failed to restart RAG service instance: %w", err)
	}

	return nil
}

// changedFrom reports whether the config file or the API key values differ
// from the previous resource's. Key values aren't in the config file, which
// only references the key files.
func (r *RAGConfigResource) changedFrom(previous *RAGConfigResource) (bool, error) {
	previousConfig, err := previous.generateConfig()
	if err != nil {
		return false, err
	}
	current, err := r.generateConfig()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(previousConfig, current) {
		return true, nil
	}
	return !maps.Equal(ExtractRAGAPIKeys(previous.Config), ExtractRAGAPIKeys(r.Config)), nil
}

func (r *RAGConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the directory resource's deletion.
	return nil
}

func (r *RAGConfigResource) configPath(rc *resource.Context) (string, error) {
	dir, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return "", fmt.Errorf("failed to get RAG config dir path: %w", err)
	}
	return filepath.Join(dir, RAGConfigFileName), nil
}

func (r *RAGConfigResource) generateConfig() ([]byte, error) {
	content, err := GenerateRAGConfig(&RAGConfigParams{
		Config:        r.Config,
		DatabaseName:  r.DatabaseName,
		DatabaseHost:  r.DatabaseHost,
		DatabasePort:  r.DatabasePort,
		Username:      r.ConnectAsUsername,
		Password:      r.ConnectAsPassword,
		KeysDir:       r.KeysDir,
		ListenAddress: r.ListenAddress,
		Port:          r.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate RAG config: %w", err)
	}
	return content, nil
}

// writeFile writes the config to a temporary file and renames it into place,
// so that the server, which may watch the file, never reads a partial file.
func (r *RAGConfigResource) writeFile(afs afero.Fs, path string, content []byte) error {
	tmpPath := path + ".tmp"
	if err := afero.WriteFile(afs, tmpPath, content, 0o600); err != nil {
		_ = afs.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", tmpPath, err)
	}
	if err := afs.Chown(tmpPath, r.OwnerUID, r.OwnerGID); err != nil {
		_ = afs.Remove(tmpPath)
		return fmt.Errorf("failed to change ownership for %s: %w", tmpPath, err)
	}
	if err := afs.Rename(tmpPath, path); err != nil {
		_ = afs.Remove(tmpPath)
		return fmt.Errorf("failed to rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
