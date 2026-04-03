package swarm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestRAGServiceKeysResource_ResourceVersion(t *testing.T) {
	r := &RAGServiceKeysResource{}
	if got := r.ResourceVersion(); got != "1" {
		t.Errorf("ResourceVersion() = %q, want %q", got, "1")
	}
}

func TestRAGServiceKeysResource_Identifier(t *testing.T) {
	r := &RAGServiceKeysResource{ServiceInstanceID: "storefront-rag-host1"}
	id := r.Identifier()
	if id.ID != "storefront-rag-host1" {
		t.Errorf("Identifier().ID = %q, want %q", id.ID, "storefront-rag-host1")
	}
	if id.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Identifier().Type = %q, want %q", id.Type, ResourceTypeRAGServiceKeys)
	}
}

func TestRAGServiceKeysResource_Executor(t *testing.T) {
	r := &RAGServiceKeysResource{HostID: "host-1"}
	exec := r.Executor()
	if exec != resource.HostExecutor("host-1") {
		t.Errorf("Executor() = %v, want HostExecutor(%q)", exec, "host-1")
	}
}

func TestRAGServiceKeysResource_DiffIgnore(t *testing.T) {
	r := &RAGServiceKeysResource{}
	if got := r.DiffIgnore(); len(got) != 0 {
		t.Errorf("DiffIgnore() = %v, want empty", got)
	}
}

func TestRAGServiceKeysResource_Dependencies(t *testing.T) {
	r := &RAGServiceKeysResource{ParentID: "storefront-rag-host1-data"}
	deps := r.Dependencies()
	if len(deps) != 1 {
		t.Fatalf("Dependencies() len = %d, want 1", len(deps))
	}
	want := filesystem.DirResourceIdentifier("storefront-rag-host1-data")
	if deps[0] != want {
		t.Errorf("Dependencies()[0] = %v, want %v", deps[0], want)
	}
}

// ragKeysRC returns a resource.Context containing a DirResource with the given full path.
func ragKeysRC(t *testing.T, parentID, parentFullPath string) *resource.Context {
	t.Helper()
	parentDir := &filesystem.DirResource{
		ID:       parentID,
		HostID:   "host-1",
		Path:     parentFullPath,
		FullPath: parentFullPath,
	}
	data, err := resource.ToResourceData(parentDir)
	if err != nil {
		t.Fatalf("ToResourceData() error = %v", err)
	}
	state := resource.NewState()
	state.Add(data)
	return &resource.Context{State: state}
}

func TestRAGServiceKeysResource_RefreshMissingParentID(t *testing.T) {
	r := &RAGServiceKeysResource{ServiceInstanceID: "inst1"}
	err := r.Refresh(context.Background(), nil)
	if err != resource.ErrNotFound {
		t.Errorf("Refresh() = %v, want ErrNotFound", err)
	}
}

func TestRAGServiceKeysResource_RefreshMissingDir(t *testing.T) {
	parentID := "inst1-data"
	rc := ragKeysRC(t, parentID, "/nonexistent/path/that/does/not/exist")
	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"default_rag.key": "sk-test"},
	}
	err := r.Refresh(context.Background(), rc)
	if err != resource.ErrNotFound {
		t.Errorf("Refresh() = %v, want ErrNotFound", err)
	}
}

func TestRAGServiceKeysResourceIdentifier(t *testing.T) {
	id := RAGServiceKeysResourceIdentifier("my-instance")
	if id.ID != "my-instance" {
		t.Errorf("ID = %q, want %q", id.ID, "my-instance")
	}
	if id.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Type = %q, want %q", id.Type, ResourceTypeRAGServiceKeys)
	}
}

func TestExtractRAGAPIKeys_AllProviders(t *testing.T) {
	embKey := "sk-embed-key"
	ragKey := "sk-ant-key"
	cfg := &database.RAGServiceConfig{
		Pipelines: []database.RAGPipeline{
			{
				Name: "default",
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
					APIKey:   &embKey,
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-5",
					APIKey:   &ragKey,
				},
			},
		},
	}

	keys := extractRAGAPIKeys(cfg)
	if keys["default_embedding.key"] != embKey {
		t.Errorf("default_embedding.key = %q, want %q", keys["default_embedding.key"], embKey)
	}
	if keys["default_rag.key"] != ragKey {
		t.Errorf("default_rag.key = %q, want %q", keys["default_rag.key"], ragKey)
	}
	if len(keys) != 2 {
		t.Errorf("len(keys) = %d, want 2", len(keys))
	}
}

func TestExtractRAGAPIKeys_OllamaSkipped(t *testing.T) {
	cfg := &database.RAGServiceConfig{
		Pipelines: []database.RAGPipeline{
			{
				Name: "local",
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "ollama",
					Model:    "nomic-embed-text",
					// APIKey is nil
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "ollama",
					Model:    "llama3",
					// APIKey is nil
				},
			},
		},
	}

	keys := extractRAGAPIKeys(cfg)
	if len(keys) != 0 {
		t.Errorf("len(keys) = %d, want 0 (ollama has no api_key)", len(keys))
	}
}

func TestExtractRAGAPIKeys_MultiPipeline(t *testing.T) {
	k1 := "sk-openai-1"
	k2 := "sk-ant-2"
	cfg := &database.RAGServiceConfig{
		Pipelines: []database.RAGPipeline{
			{
				Name: "pipeline-a",
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
					APIKey:   &k1,
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-5",
					APIKey:   &k2,
				},
			},
			{
				Name: "pipeline-b",
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "ollama",
					Model:    "nomic-embed-text",
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "ollama",
					Model:    "llama3",
				},
			},
		},
	}

	keys := extractRAGAPIKeys(cfg)
	if _, ok := keys["pipeline-a_embedding.key"]; !ok {
		t.Error("missing pipeline-a_embedding.key")
	}
	if _, ok := keys["pipeline-a_rag.key"]; !ok {
		t.Error("missing pipeline-a_rag.key")
	}
	if _, ok := keys["pipeline-b_embedding.key"]; ok {
		t.Error("unexpected pipeline-b_embedding.key (ollama has no api_key)")
	}
	if len(keys) != 2 {
		t.Errorf("len(keys) = %d, want 2", len(keys))
	}
}

func TestGenerateRAGInstanceResources_IncludesKeysResource(t *testing.T) {
	o := &Orchestrator{}
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config: map[string]any{
				"pipelines": []any{
					map[string]any{
						"name": "default",
						"tables": []any{
							map[string]any{
								"table":         "docs",
								"text_column":   "content",
								"vector_column": "embedding",
							},
						},
						"embedding_llm": map[string]any{
							"provider": "openai",
							"model":    "text-embedding-3-small",
							"api_key":  "sk-embed",
						},
						"rag_llm": map[string]any{
							"provider": "anthropic",
							"model":    "claude-sonnet-4-5",
							"api_key":  "sk-ant",
						},
					},
				},
			},
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	result, err := o.generateRAGInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateRAGInstanceResources() error = %v", err)
	}

	var foundKeys bool
	for _, rd := range result.Resources {
		if rd.Identifier.Type == ResourceTypeRAGServiceKeys {
			foundKeys = true
			break
		}
	}
	if !foundKeys {
		t.Errorf("expected ResourceTypeRAGServiceKeys in resources, not found")
	}
}

// ragKeysRCWithTempDir returns a resource.Context whose DirResource points at a
// fresh temporary directory that is automatically cleaned up after the test.
func ragKeysRCWithTempDir(t *testing.T, parentID string) (*resource.Context, string) {
	t.Helper()
	parentPath := t.TempDir()
	return ragKeysRC(t, parentID, parentPath), parentPath
}

func TestRAGServiceKeysResource_Create(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys: map[string]string{
			"default_embedding.key": "sk-embed",
			"default_rag.key":       "sk-rag",
		},
	}

	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	keysDir := filepath.Join(parentPath, "keys")
	for name, want := range r.Keys {
		got, err := os.ReadFile(filepath.Join(keysDir, name))
		if err != nil {
			t.Errorf("ReadFile(%q) error = %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("key file %q = %q, want %q", name, string(got), want)
		}
		info, err := os.Stat(filepath.Join(keysDir, name))
		if err != nil {
			t.Errorf("Stat(%q) error = %v", name, err)
			continue
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("key file %q perm = %04o, want 0600", name, perm)
		}
	}

	// Refresh must succeed now that the directory and files exist.
	if err := r.Refresh(context.Background(), rc); err != nil {
		t.Errorf("Refresh() after Create = %v, want nil", err)
	}
}

func TestRAGServiceKeysResource_Update_WritesNewKeys(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"old_rag.key": "sk-old"},
	}
	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	r.Keys = map[string]string{"new_rag.key": "sk-new"}
	if err := r.Update(context.Background(), rc); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	keysDir := filepath.Join(parentPath, "keys")

	if _, err := os.Stat(filepath.Join(keysDir, "old_rag.key")); !os.IsNotExist(err) {
		t.Errorf("old_rag.key should be removed after Update, got err = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(keysDir, "new_rag.key"))
	if err != nil {
		t.Fatalf("ReadFile(new_rag.key) error = %v", err)
	}
	if string(got) != "sk-new" {
		t.Errorf("new_rag.key = %q, want %q", string(got), "sk-new")
	}
}

func TestRAGServiceKeysResource_Delete(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"default_rag.key": "sk-test"},
	}
	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := r.Delete(context.Background(), rc); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	keysDir := filepath.Join(parentPath, "keys")
	if _, err := os.Stat(keysDir); !os.IsNotExist(err) {
		t.Errorf("keys directory should not exist after Delete, got err = %v", err)
	}
}

func TestRAGServiceKeysResource_Delete_NilRC(t *testing.T) {
	r := &RAGServiceKeysResource{ServiceInstanceID: "inst1"}
	// Delete with nil rc (parent unresolvable) must not error.
	if err := r.Delete(context.Background(), nil); err != nil {
		t.Errorf("Delete() with nil rc = %v, want nil", err)
	}
}

func TestValidateKeyFilename(t *testing.T) {
	valid := []string{
		"default_rag.key",
		"pipeline-a_embedding.key",
		"foo.key",
	}
	for _, name := range valid {
		if err := validateKeyFilename(name); err != nil {
			t.Errorf("validateKeyFilename(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"../escape.key",
		"/absolute/path.key",
		"sub/dir.key",
		`sub\dir.key`,
		"./relative.key",
		".",
		"..",
	}
	for _, name := range invalid {
		if err := validateKeyFilename(name); err == nil {
			t.Errorf("validateKeyFilename(%q) = nil, want error", name)
		}
	}
}

func TestRAGServiceKeysResource_Create_DirPermissions(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"default_rag.key": "sk-test"},
	}
	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	keysDir := filepath.Join(parentPath, "keys")
	info, err := os.Stat(keysDir)
	if err != nil {
		t.Fatalf("Stat(keysDir) error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("keys dir perm = %04o, want 0700", perm)
	}
}

func TestRAGServiceKeysResource_Update_EnforcesPermissionsOnExistingDir(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"default_rag.key": "sk-v1"},
	}
	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Widen permissions to simulate an insecure state.
	keysDir := filepath.Join(parentPath, "keys")
	if err := os.Chmod(keysDir, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	keyFile := filepath.Join(keysDir, "default_rag.key")
	if err := os.Chmod(keyFile, 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	r.Keys = map[string]string{"default_rag.key": "sk-v2"}
	if err := r.Update(context.Background(), rc); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Directory must be back to 0700.
	info, err := os.Stat(keysDir)
	if err != nil {
		t.Fatalf("Stat(keysDir) error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("keys dir perm after Update = %04o, want 0700", perm)
	}

	// File must be 0600.
	info, err = os.Stat(keyFile)
	if err != nil {
		t.Fatalf("Stat(keyFile) error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file perm after Update = %04o, want 0600", perm)
	}
}

func TestRAGServiceKeysResource_Refresh_InvalidFilenameInState(t *testing.T) {
	parentID := "inst1-data"
	rc, _ := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"../escape.key": "sk-bad"},
	}
	err := r.Refresh(context.Background(), rc)
	if err == nil {
		t.Error("Refresh() = nil, want error for invalid key filename in state")
	}
}

func TestRAGServiceKeysResource_Update_InvalidFilenameIsNonDestructive(t *testing.T) {
	parentID := "inst1-data"
	rc, parentPath := ragKeysRCWithTempDir(t, parentID)

	r := &RAGServiceKeysResource{
		ServiceInstanceID: "inst1",
		HostID:            "host-1",
		ParentID:          parentID,
		Keys:              map[string]string{"default_rag.key": "sk-good"},
	}
	if err := r.Create(context.Background(), rc); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Attempt Update with an invalid filename — must fail before any deletion.
	r.Keys = map[string]string{"../escape.key": "sk-bad"}
	if err := r.Update(context.Background(), rc); err == nil {
		t.Fatal("Update() = nil, want error for invalid key filename")
	}

	// The original file must still be present — Update must not have deleted it.
	existing := filepath.Join(parentPath, "keys", "default_rag.key")
	if _, err := os.Stat(existing); err != nil {
		t.Errorf("default_rag.key should still exist after failed Update, got err = %v", err)
	}
}
