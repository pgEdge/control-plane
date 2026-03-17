package swarm

import (
	"context"
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
	// rc with an empty State — parent dir not found → ErrNotFound.
	err := r.Refresh(context.Background(), &resource.Context{State: resource.NewState()})
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
