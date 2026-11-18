package common

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

const ResourceTypeRAGServiceKeys resource.Type = "common.rag_service_keys"

// RAGKeysDirName is the name of the keys directory, which is created under
// the parent directory of a RAGServiceKeysResource.
const RAGKeysDirName = "keys"

func RAGServiceKeysResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGServiceKeys,
	}
}

// RAGServiceKeysResource manages the provider API key files of a RAG service
// instance. The files are written to a "keys" subdirectory of the parent
// directory, owned by the given user, and removed with it when the service
// instance is deleted.
type RAGServiceKeysResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	HostID            string `json:"host_id"`
	// ParentID is the ID of the DirResource that holds the keys directory.
	ParentID string `json:"parent_id"`
	// Keys maps each key filename to the key's value.
	Keys     map[string]string `json:"keys"`
	OwnerUID int               `json:"owner_uid"`
	OwnerGID int               `json:"owner_gid"`
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
		return "", fmt.Errorf("failed to get RAG keys parent dir path: %w", err)
	}
	return filepath.Join(parentPath, RAGKeysDirName), nil
}

func (r *RAGServiceKeysResource) Refresh(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}

	info, err := afs.Stat(keysDir)
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
		match, err := keyFileMatches(afs, filepath.Join(keysDir, name), r.Keys[name])
		if err != nil {
			return fmt.Errorf("failed to check key file %q: %w", name, err)
		}
		if !match {
			// A missing, non-regular or changed key file makes the resource
			// be recreated, which rewrites it. The key value isn't logged.
			return resource.ErrNotFound
		}
	}

	return nil
}

func (r *RAGServiceKeysResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.apply(rc)
}

func (r *RAGServiceKeysResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.apply(rc)
}

// apply writes the desired key files, then removes the key files that are no
// longer needed. Writing first means a failed write never leaves the
// installed config referencing deleted key files. Stale files are removed on
// Create too, since a resource is recreated when one of its files is missing
// or changed, and the directory may still hold a removed pipeline's keys.
func (r *RAGServiceKeysResource) apply(rc *resource.Context) error {
	// Validate all desired filenames before any filesystem mutation so that an
	// invalid name never leaves the directory in a partially-updated state.
	for name := range r.Keys {
		if err := validateKeyFilename(name); err != nil {
			return err
		}
	}

	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		return err
	}
	if err := afs.MkdirAll(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}
	if err := afs.Chmod(keysDir, 0o700); err != nil {
		return fmt.Errorf("failed to set keys directory permissions: %w", err)
	}
	if err := afs.Chown(keysDir, r.OwnerUID, r.OwnerGID); err != nil {
		return fmt.Errorf("failed to set keys directory ownership: %w", err)
	}
	if err := r.writeKeyFiles(afs, keysDir); err != nil {
		return err
	}
	return r.removeStaleKeyFiles(afs, keysDir)
}

// keyFileMatches reports whether path is a regular file that holds exactly
// the expected key.
func keyFileMatches(afs afero.Fs, path, expected string) (bool, error) {
	info, err := afs.Stat(path)
	if errors.Is(err, afero.ErrFileNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	content, err := afero.ReadFile(afs, path)
	if err != nil {
		return false, err
	}
	return string(content) == expected, nil
}

func (r *RAGServiceKeysResource) Delete(ctx context.Context, rc *resource.Context) error {
	afs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	keysDir, err := r.keysDir(rc)
	if err != nil {
		// Parent dir is gone or unresolvable; nothing to clean up.
		return nil
	}
	if err := afs.RemoveAll(keysDir); err != nil {
		return fmt.Errorf("failed to remove keys directory: %w", err)
	}
	return nil
}

// writeKeyFiles writes each key to a temporary file and renames it into
// place, so that the server, which may watch these files, never reads a
// partially written key.
func (r *RAGServiceKeysResource) writeKeyFiles(afs afero.Fs, keysDir string) error {
	for name, key := range r.Keys {
		path := filepath.Join(keysDir, name)
		tmpPath := path + ".tmp"
		if err := afero.WriteFile(afs, tmpPath, []byte(key), 0o600); err != nil {
			_ = afs.Remove(tmpPath)
			return fmt.Errorf("failed to write key file %q: %w", name, err)
		}
		if err := afs.Chown(tmpPath, r.OwnerUID, r.OwnerGID); err != nil {
			_ = afs.Remove(tmpPath)
			return fmt.Errorf("failed to set key file %q ownership: %w", name, err)
		}
		if err := afs.Rename(tmpPath, path); err != nil {
			_ = afs.Remove(tmpPath)
			return fmt.Errorf("failed to rename key file %q into place: %w", name, err)
		}
	}
	return nil
}

// removeStaleKeyFiles deletes key files in keysDir that are no longer in
// r.Keys, e.g. after a pipeline was removed.
func (r *RAGServiceKeysResource) removeStaleKeyFiles(afs afero.Fs, keysDir string) error {
	entries, err := afero.ReadDir(afs, keysDir)
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
			if err := afs.Remove(path); err != nil && !errors.Is(err, afero.ErrFileNotFound) {
				return fmt.Errorf("failed to remove stale key file %q: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

// validateKeyFilename rejects filenames that could escape the keys directory
// via path traversal.
func validateKeyFilename(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("invalid key filename %q", name)
	}
	if filepath.Clean(name) != name || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid key filename %q", name)
	}
	return nil
}

// ExtractRAGAPIKeys builds the filename→value map from a parsed
// RAGServiceConfig. Filenames follow the convention
// {pipeline_name}_embedding.key and {pipeline_name}_rag.key. Providers that
// do not require an API key (e.g. ollama) produce no entry. A pipeline's
// optional rerank stage gets its own {pipeline_name}_rerank.key file only
// when embedding_llm isn't already supplying a key for the same provider, in
// which case the rerank stage reuses that key file.
func ExtractRAGAPIKeys(cfg *database.RAGServiceConfig) map[string]string {
	keys := make(map[string]string)
	for _, p := range cfg.Pipelines {
		if p.EmbeddingLLM.APIKey != nil && *p.EmbeddingLLM.APIKey != "" {
			keys[p.Name+"_embedding.key"] = *p.EmbeddingLLM.APIKey
		}
		if p.RAGLLM.APIKey != nil && *p.RAGLLM.APIKey != "" {
			keys[p.Name+"_rag.key"] = *p.RAGLLM.APIKey
		}
		if p.Rerank != nil && p.Rerank.APIKey != nil && *p.Rerank.APIKey != "" {
			embeddingHasSameProvider := p.EmbeddingLLM.Provider == p.Rerank.Provider &&
				p.EmbeddingLLM.APIKey != nil && *p.EmbeddingLLM.APIKey != ""
			if !embeddingHasSameProvider {
				keys[p.Name+"_rerank.key"] = *p.Rerank.APIKey
			}
		}
	}
	return keys
}
