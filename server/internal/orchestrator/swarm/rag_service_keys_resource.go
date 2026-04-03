package swarm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if rc == nil {
		return resource.ErrNotFound
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return resource.ErrNotFound
	}

	info, err := os.Stat(keysDir)
	if os.IsNotExist(err) {
		return resource.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to stat keys directory: %w", err)
	}
	if !info.IsDir() {
		return resource.ErrNotFound
	}

	for name := range r.Keys {
		if _, err := os.Stat(filepath.Join(keysDir, name)); err != nil {
			if os.IsNotExist(err) {
				return resource.ErrNotFound
			}
			return fmt.Errorf("failed to stat key file %q: %w", name, err)
		}
	}

	return nil
}

func (r *RAGServiceKeysResource) Create(ctx context.Context, rc *resource.Context) error {
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}
	return r.writeKeyFiles(keysDir)
}

func (r *RAGServiceKeysResource) Update(ctx context.Context, rc *resource.Context) error {
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}
	if err := r.removeStaleKeyFiles(keysDir); err != nil {
		return err
	}
	return r.writeKeyFiles(keysDir)
}

func (r *RAGServiceKeysResource) Delete(ctx context.Context, rc *resource.Context) error {
	if rc == nil {
		return nil
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		// Parent dir is gone or unresolvable; nothing to clean up.
		return nil
	}
	if err := os.RemoveAll(keysDir); err != nil {
		return fmt.Errorf("failed to remove keys directory: %w", err)
	}
	return nil
}

func (r *RAGServiceKeysResource) writeKeyFiles(keysDir string) error {
	for name, key := range r.Keys {
		if err := validateKeyFilename(name); err != nil {
			return err
		}
		path := filepath.Join(keysDir, name)
		if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
			return fmt.Errorf("failed to write key file %q: %w", name, err)
		}
	}
	return nil
}

// removeStaleKeyFiles deletes key files in keysDir that are no longer in r.Keys.
// This handles the case where a pipeline (and its key files) has been removed.
func (r *RAGServiceKeysResource) removeStaleKeyFiles(keysDir string) error {
	entries, err := os.ReadDir(keysDir)
	if os.IsNotExist(err) {
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
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove stale key file %q: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

// validateKeyFilename rejects filenames that could escape the keys directory via path traversal.
func validateKeyFilename(name string) error {
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
