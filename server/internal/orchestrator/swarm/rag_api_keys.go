package swarm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

var _ resource.Resource = (*RAGAPIKeysResource)(nil)

const ResourceTypeRAGAPIKeys resource.Type = "swarm.rag_api_keys"

// ragKeysContainerPath is the container-internal directory where key files are mounted.
const ragKeysContainerPath = "/etc/pgedge/keys"

func RAGAPIKeysResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGAPIKeys,
	}
}

// RAGAPIKeysResource writes LLM provider API keys as individual files on the
// host filesystem so they can be bind-mounted into the RAG container. The RAG
// server reads key file paths from the api_keys section of its YAML config.
//
// Keys are never placed in the Swarm config object (stored in Swarm state and
// readable via docker config inspect).
type RAGAPIKeysResource struct {
	ServiceInstanceID string                `json:"service_instance_id"`
	HostID            string                `json:"host_id"`
	ServiceSpec       *database.ServiceSpec `json:"service_spec"`
	// KeysDirPath is the absolute host path where key files are written.
	// Populated by Create/Refresh and consumed by ServiceInstanceSpecResource for the bind mount.
	KeysDirPath string `json:"keys_dir_path"`
}

func (r *RAGAPIKeysResource) ResourceVersion() string {
	return "1"
}

func (r *RAGAPIKeysResource) DiffIgnore() []string {
	return []string{"/keys_dir_path"}
}

func (r *RAGAPIKeysResource) Identifier() resource.Identifier {
	return RAGAPIKeysResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGAPIKeysResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *RAGAPIKeysResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		ServiceUserRoleIdentifier(r.ServiceInstanceID),
	}
}

func (r *RAGAPIKeysResource) keysDir() string {
	return filepath.Join("/var/lib/pgedge/services", r.ServiceInstanceID, "keys")
}

func (r *RAGAPIKeysResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dir := r.keysDir()
	info, err := fs.Stat(dir)
	if errors.Is(err, afero.ErrFileNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to stat keys dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return resource.ErrNotFound
	}

	r.KeysDirPath = dir
	return nil
}

func (r *RAGAPIKeysResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.writeKeys(ctx, rc)
}

func (r *RAGAPIKeysResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.writeKeys(ctx, rc)
}

func (r *RAGAPIKeysResource) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	if err := fs.RemoveAll(r.keysDir()); err != nil {
		return fmt.Errorf("failed to remove keys dir: %w", err)
	}
	return nil
}

func (r *RAGAPIKeysResource) writeKeys(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dir := r.keysDir()
	if err := fs.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys dir %q: %w", dir, err)
	}

	for filename, key := range collectRAGAPIKeys(r.ServiceSpec.Config) {
		path := filepath.Join(dir, filename)
		if err := afero.WriteFile(fs, path, []byte(key), 0o600); err != nil {
			return fmt.Errorf("failed to write key file %q: %w", path, err)
		}
	}

	r.KeysDirPath = dir
	return nil
}

// collectRAGAPIKeys scans the top-level config and all pipeline entries,
// returning a map of filename → key value for each API key found.
// The filename matches what the RAG server expects under api_keys in its YAML.
func collectRAGAPIKeys(config map[string]any) map[string]string {
	type keyDef struct{ field, filename string }
	defs := []keyDef{
		{"openai_api_key", "openai"},
		{"anthropic_api_key", "anthropic"},
		{"voyage_api_key", "voyage"},
	}

	sources := []map[string]any{config}
	if pipelines, ok := config["pipelines"].([]any); ok {
		for _, raw := range pipelines {
			if p, ok := raw.(map[string]any); ok {
				sources = append(sources, p)
			}
		}
	}

	result := make(map[string]string)
	for _, def := range defs {
		for _, src := range sources {
			if val, ok := src[def.field].(string); ok && val != "" {
				result[def.filename] = val
				break // first non-empty value wins
			}
		}
	}
	return result
}
