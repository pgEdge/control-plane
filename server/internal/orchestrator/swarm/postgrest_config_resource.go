package swarm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgRESTConfigResource)(nil)

const ResourceTypePostgRESTConfig resource.Type = "swarm.postgrest_config"

func PostgRESTConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypePostgRESTConfig,
	}
}

// PostgRESTConfigResource manages the postgrest.conf file on the host filesystem.
// The file is bind-mounted read-only into the container and includes the db-uri
// with embedded credentials.
type PostgRESTConfigResource struct {
	ServiceInstanceID  string                           `json:"service_instance_id"`
	ServiceID          string                           `json:"service_id"`
	HostID             string                           `json:"host_id"`
	DirResourceID      string                           `json:"dir_resource_id"`
	Config             *database.PostgRESTServiceConfig `json:"config"`
	Username           string                           `json:"username"`
	Password           string                           `json:"password"`
	DatabaseName       string                           `json:"database_name"`
	DatabaseHosts      []database.ServiceHostEntry      `json:"database_hosts"`
	TargetSessionAttrs string                           `json:"target_session_attrs,omitempty"`
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

	_, err = readResourceFile(fs, filepath.Join(dirPath, "postgrest.conf"))
	if err != nil {
		return fmt.Errorf("failed to read PostgREST config: %w", err)
	}

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
	return r.Create(ctx, rc)
}

func (r *PostgRESTConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the parent directory resource deletion.
	return nil
}

func (r *PostgRESTConfigResource) writeConfigFile(fs afero.Fs, dirPath string) error {
	content, err := r.Config.GenerateConf(database.PostgRESTConnParams{
		Username:           r.Username,
		Password:           r.Password,
		DatabaseName:       r.DatabaseName,
		DatabaseHosts:      r.DatabaseHosts,
		TargetSessionAttrs: r.TargetSessionAttrs,
	})
	if err != nil {
		return fmt.Errorf("failed to generate PostgREST config: %w", err)
	}

	configPath := filepath.Join(dirPath, "postgrest.conf")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, postgrestContainerUID, postgrestContainerUID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return nil
}
