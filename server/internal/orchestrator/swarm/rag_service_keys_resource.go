package swarm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*RAGServiceKeysResource)(nil)

const ResourceTypeRAGServiceKeys resource.Type = "swarm.rag_service_keys"

func RAGServiceKeysResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGServiceKeys,
	}
}

// RAGServiceKeysResource manages provider API key files on the host filesystem.
// Keys are written to a "keys" subdirectory under the service data directory
// and bind-mounted read-only into the RAG container.
// The directory and all files are removed when the service is deleted.
type RAGServiceKeysResource struct {
	ServiceInstanceID string            `json:"service_instance_id"`
	HostID            string            `json:"host_id"`
	ParentID          string            `json:"parent_id"` // DirResource ID for the service data directory
	Keys              map[string]string `json:"keys"`      // filename → key value
}

func (r *RAGServiceKeysResource) ResourceVersion() string {
	return "1"
}

func (r *RAGServiceKeysResource) DiffIgnore() []string {
	return nil
}

func (r *RAGServiceKeysResource) Identifier() resource.Identifier {
	return RAGServiceKeysResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGServiceKeysResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *RAGServiceKeysResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(r.ParentID),
	}
}

func (r *RAGServiceKeysResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *RAGServiceKeysResource) keysDir(rc *resource.Context) (string, error) {
	parentPath, err := filesystem.DirResourceFullPath(rc, r.ParentID)
	if err != nil {
		return "", fmt.Errorf("failed to get service data dir path: %w", err)
	}
	return filepath.Join(parentPath, "keys"), nil
}

func (r *RAGServiceKeysResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}

	info, err := fs.Stat(keysDir)
	if errors.Is(err, afero.ErrFileNotFound) {
		return resource.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to stat keys directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("expected %q to be a directory", keysDir)
	}

	for name := range r.Keys {
		if err := validateKeyFilename(name); err != nil {
			return fmt.Errorf("invalid key filename in state: %w", err)
		}
		if _, err := fs.Stat(filepath.Join(keysDir, name)); err != nil {
			if errors.Is(err, afero.ErrFileNotFound) {
				return resource.ErrNotFound
			}
			return fmt.Errorf("failed to stat key file %q: %w", name, err)
		}
	}

	return nil
}

func (r *RAGServiceKeysResource) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}
	if err := fs.Chown(keysDir, ragContainerUID, ragContainerUID); err != nil {
		return fmt.Errorf("failed to set keys directory ownership: %w", err)
	}
	return r.writeKeyFiles(fs, keysDir)
}

func (r *RAGServiceKeysResource) Update(ctx context.Context, rc *resource.Context) error {
	// Validate all desired filenames before any filesystem mutation so that an
	// invalid name never leaves the directory in a partially-deleted state.
	for name := range r.Keys {
		if err := validateKeyFilename(name); err != nil {
			return err
		}
	}

	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}
	if err := fs.Chmod(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to set keys directory permissions: %w", err)
	}
	if err := fs.Chown(keysDir, ragContainerUID, ragContainerUID); err != nil {
		return fmt.Errorf("failed to set keys directory ownership: %w", err)
	}
	if err := r.removeStaleKeyFiles(fs, keysDir); err != nil {
		return err
	}
	return r.writeKeyFiles(fs, keysDir)
}

func (r *RAGServiceKeysResource) Delete(ctx context.Context, rc *resource.Context) error {
	if rc == nil {
		return nil
	}
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		// Parent dir is gone or unresolvable; nothing to clean up.
		return nil
	}
	if err := fs.RemoveAll(keysDir); err != nil {
		return fmt.Errorf("failed to remove keys directory: %w", err)
	}
	return nil
}

func (r *RAGServiceKeysResource) writeKeyFiles(fs afero.Fs, keysDir string) error {
	for name, key := range r.Keys {
		if err := validateKeyFilename(name); err != nil {
			return err
		}
		path := filepath.Join(keysDir, name)
		if err := afero.WriteFile(fs, path, []byte(key), 0o600); err != nil {
			return fmt.Errorf("failed to write key file %q: %w", name, err)
		}
		if err := fs.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("failed to set key file %q permissions: %w", name, err)
		}
		if err := fs.Chown(path, ragContainerUID, ragContainerUID); err != nil {
			return fmt.Errorf("failed to set key file %q ownership: %w", name, err)
		}
	}
	return nil
}

// removeStaleKeyFiles deletes key files in keysDir that are no longer in r.Keys.
// This handles the case where a pipeline (and its key files) has been removed.
func (r *RAGServiceKeysResource) removeStaleKeyFiles(fs afero.Fs, keysDir string) error {
	entries, err := afero.ReadDir(fs, keysDir)
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read keys directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := r.Keys[entry.Name()]; !ok {
			path := filepath.Join(keysDir, entry.Name())
			if err := fs.Remove(path); err != nil && !errors.Is(err, afero.ErrFileNotFound) {
				return fmt.Errorf("failed to remove stale key file %q: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

// validateKeyFilename rejects filenames that could escape the keys directory via path traversal.
func validateKeyFilename(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("invalid key filename %q", name)
	}
	if filepath.Clean(name) != name || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid key filename %q", name)
	}
	return nil
}

// extractRAGAPIKeys builds the filename→value map from a parsed RAGServiceConfig.
// Filenames follow the convention: {pipeline_name}_embedding.key and {pipeline_name}_rag.key.
// Providers that do not require an API key (e.g. ollama) produce no entry.
func extractRAGAPIKeys(cfg *database.RAGServiceConfig) map[string]string {
	keys := make(map[string]string)
	for _, p := range cfg.Pipelines {
		if p.EmbeddingLLM.APIKey != nil && *p.EmbeddingLLM.APIKey != "" {
			keys[p.Name+"_embedding.key"] = *p.EmbeddingLLM.APIKey
		}
		if p.RAGLLM.APIKey != nil && *p.RAGLLM.APIKey != "" {
			keys[p.Name+"_rag.key"] = *p.RAGLLM.APIKey
		}
	}
	return keys
}
