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

var _ resource.Resource = (*RAGConfigResource)(nil)

const ResourceTypeRAGConfig resource.Type = "swarm.rag_config"

// ragConfigFilename is the config file name expected by pgedge-rag-server.
const ragConfigFilename = "pgedge-rag-server.yaml"

// ragKeysContainerDir is the container-side mount path for the keys directory.
const ragKeysContainerDir = "/app/keys"

func RAGConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGConfig,
	}
}

// RAGConfigResource manages the pgedge-rag-server.yaml config file on the
// host filesystem. The file is written to the service data directory
// (managed by a DirResource) which is bind-mounted into the container at
// /app/data. On every Create or Update the file is regenerated from the
// current RAGServiceConfig and the connect_as credentials sourced from
// database_users.
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
}

func (r *RAGConfigResource) ResourceVersion() string {
	return "2"
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
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	_, err = readResourceFile(fs, filepath.Join(dirPath, ragConfigFilename))
	if err != nil {
		return fmt.Errorf("failed to read RAG config: %w", err)
	}

	return nil
}

func (r *RAGConfigResource) Create(ctx context.Context, rc *resource.Context) error {
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

func (r *RAGConfigResource) Update(ctx context.Context, rc *resource.Context) error {
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

func (r *RAGConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the parent DirResource deletion.
	return nil
}

func (r *RAGConfigResource) writeConfigFile(fs afero.Fs, dirPath string) error {
	content, err := GenerateRAGConfig(&RAGConfigParams{
		Config:       r.Config,
		DatabaseName: r.DatabaseName,
		DatabaseHost: r.DatabaseHost,
		DatabasePort: r.DatabasePort,
		Username:     r.ConnectAsUsername,
		Password:     r.ConnectAsPassword,
		KeysDir:      ragKeysContainerDir,
	})
	if err != nil {
		return fmt.Errorf("failed to generate RAG config: %w", err)
	}

	configPath := filepath.Join(dirPath, ragConfigFilename)
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, ragContainerUID, ragContainerUID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return nil
}
