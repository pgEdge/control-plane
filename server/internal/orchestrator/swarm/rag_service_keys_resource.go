package swarm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgEdge/control-plane/server/internal/database"
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
// Keys are written to KeysDir and bind-mounted read-only into the RAG container.
// The directory and all files are removed when the service is deleted.
type RAGServiceKeysResource struct {
	ServiceInstanceID string            `json:"service_instance_id"`
	HostID            string            `json:"host_id"`
	KeysDir           string            `json:"keys_dir"` // absolute path on host
	Keys              map[string]string `json:"keys"`     // filename → key value
}

func (r *RAGServiceKeysResource) ResourceVersion() string {
	return "1"
}

func (r *RAGServiceKeysResource) DiffIgnore() []string {
	return []string{
		"/keys_dir",
	}
}

func (r *RAGServiceKeysResource) Identifier() resource.Identifier {
	return RAGServiceKeysResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGServiceKeysResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *RAGServiceKeysResource) Dependencies() []resource.Identifier {
	return nil
}

func (r *RAGServiceKeysResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *RAGServiceKeysResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if r.KeysDir == "" {
		return resource.ErrNotFound
	}

	info, err := os.Stat(r.KeysDir)
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
		if _, err := os.Stat(filepath.Join(r.KeysDir, name)); os.IsNotExist(err) {
			return resource.ErrNotFound
		}
	}

	return nil
}

func (r *RAGServiceKeysResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := os.MkdirAll(r.KeysDir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}
	return r.writeKeyFiles()
}

func (r *RAGServiceKeysResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.writeKeyFiles()
}

func (r *RAGServiceKeysResource) Delete(ctx context.Context, rc *resource.Context) error {
	if r.KeysDir == "" {
		return nil
	}
	if err := os.RemoveAll(r.KeysDir); err != nil {
		return fmt.Errorf("failed to remove keys directory: %w", err)
	}
	return nil
}

func (r *RAGServiceKeysResource) writeKeyFiles() error {
	for name, key := range r.Keys {
		path := filepath.Join(r.KeysDir, name)
		if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
			return fmt.Errorf("failed to write key file %q: %w", name, err)
		}
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
