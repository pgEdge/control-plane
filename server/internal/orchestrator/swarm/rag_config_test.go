package swarm

import (
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// parseRAGYAML unmarshals GenerateRAGConfig output into ragYAMLConfig for assertion.
func parseRAGYAML(t *testing.T, data []byte) *ragYAMLConfig {
	t.Helper()
	var cfg ragYAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v\nYAML:\n%s", err, string(data))
	}
	return &cfg
}

func minimalRAGParams() *RAGConfigParams {
	apiKey := "sk-ant-test"
	embedKey := "sk-openai-test"
	return &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name: "default",
					Tables: []database.RAGPipelineTable{
						{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
					},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "openai",
						Model:    "text-embedding-3-small",
						APIKey:   &embedKey,
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "anthropic",
						Model:    "claude-sonnet-4-5",
						APIKey:   &apiKey,
					},
				},
			},
		},
		DatabaseName: "mydb",
		DatabaseHost: "pg-host",
		DatabasePort: 5432,
		Username:     "svc_rag",
		Password:     "secret",
		KeysDir:      "/app/keys",
	}
}

func TestGenerateRAGConfig_ServerDefaults(t *testing.T) {
	data, err := GenerateRAGConfig(minimalRAGParams())
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if cfg.Server.ListenAddress != "0.0.0.0" {
		t.Errorf("server.listen_address = %q, want %q", cfg.Server.ListenAddress, "0.0.0.0")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want 8080", cfg.Server.Port)
	}
}

func TestGenerateRAGConfig_DatabaseConnection(t *testing.T) {
	params := minimalRAGParams()
	params.DatabaseHost = "pg-primary.internal"
	params.DatabasePort = 5433
	params.DatabaseName = "storefront"
	params.Username = "svc_storefront_rag"
	params.Password = "supersecret"

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if len(cfg.Pipelines) != 1 {
		t.Fatalf("pipelines len = %d, want 1", len(cfg.Pipelines))
	}
	db := cfg.Pipelines[0].Database

	if db.Host != "pg-primary.internal" {
		t.Errorf("database.host = %q, want %q", db.Host, "pg-primary.internal")
	}
	if db.Port != 5433 {
		t.Errorf("database.port = %d, want 5433", db.Port)
	}
	if db.Database != "storefront" {
		t.Errorf("database.database = %q, want %q", db.Database, "storefront")
	}
	if db.Username != "svc_storefront_rag" {
		t.Errorf("database.username = %q, want %q", db.Username, "svc_storefront_rag")
	}
	if db.Password != "supersecret" {
		t.Errorf("database.password = %q, want %q", db.Password, "supersecret")
	}
	if db.SSLMode != "prefer" {
		t.Errorf("database.ssl_mode = %q, want %q", db.SSLMode, "prefer")
	}
}

func TestGenerateRAGConfig_APIKeyPaths_DifferentProviders(t *testing.T) {
	// embedding = openai, rag = anthropic → separate api_keys paths
	data, err := GenerateRAGConfig(minimalRAGParams())
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	keys := cfg.Pipelines[0].APIKeys

	if keys == nil {
		t.Fatal("api_keys should be present")
	}
	if keys.OpenAI != "/app/keys/default_embedding.key" {
		t.Errorf("api_keys.openai = %q, want %q", keys.OpenAI, "/app/keys/default_embedding.key")
	}
	if keys.Anthropic != "/app/keys/default_rag.key" {
		t.Errorf("api_keys.anthropic = %q, want %q", keys.Anthropic, "/app/keys/default_rag.key")
	}
	if keys.Voyage != "" {
		t.Errorf("api_keys.voyage should be empty, got %q", keys.Voyage)
	}
}

func TestGenerateRAGConfig_APIKeyPaths_VoyageEmbedding(t *testing.T) {
	voyageKey := "pa-voyage-key"
	antKey := "sk-ant-test"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:   "search",
					Tables: []database.RAGPipelineTable{{Table: "docs", TextColumn: "body", VectorColumn: "vec"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "voyage",
						Model:    "voyage-3",
						APIKey:   &voyageKey,
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "anthropic",
						Model:    "claude-sonnet-4-5",
						APIKey:   &antKey,
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	keys := cfg.Pipelines[0].APIKeys

	if keys == nil {
		t.Fatal("api_keys should be present")
	}
	if keys.Voyage != "/app/keys/search_embedding.key" {
		t.Errorf("api_keys.voyage = %q, want %q", keys.Voyage, "/app/keys/search_embedding.key")
	}
	if keys.Anthropic != "/app/keys/search_rag.key" {
		t.Errorf("api_keys.anthropic = %q, want %q", keys.Anthropic, "/app/keys/search_rag.key")
	}
}

func TestGenerateRAGConfig_APIKeyPaths_SameProvider_RAGTakesPrecedence(t *testing.T) {
	// Both embedding and rag use openai → rag key path overwrites embedding key path.
	key := "sk-openai-key"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:   "default",
					Tables: []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "openai", Model: "text-embedding-3-small", APIKey: &key,
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "openai", Model: "gpt-4o", APIKey: &key,
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	keys := cfg.Pipelines[0].APIKeys

	if keys == nil {
		t.Fatal("api_keys should be present")
	}
	// rag key takes precedence when provider is the same
	if keys.OpenAI != "/app/keys/default_rag.key" {
		t.Errorf("api_keys.openai = %q, want rag key path %q", keys.OpenAI, "/app/keys/default_rag.key")
	}
}

func TestGenerateRAGConfig_OllamaNoAPIKey(t *testing.T) {
	// ollama providers have no API key — api_keys section must be absent.
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:   "default",
					Tables: []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "ollama", Model: "nomic-embed-text",
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "ollama", Model: "llama3",
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if cfg.Pipelines[0].APIKeys != nil {
		t.Errorf("api_keys should be absent for ollama providers, got %+v", cfg.Pipelines[0].APIKeys)
	}
}

func TestGenerateRAGConfig_LLMConfig(t *testing.T) {
	baseURL := "http://ollama.internal:11434"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:   "default",
					Tables: []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "ollama", Model: "nomic-embed-text", BaseURL: &baseURL,
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "ollama", Model: "llama3",
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	p := cfg.Pipelines[0]

	if p.EmbeddingLLM.Provider != "ollama" {
		t.Errorf("embedding_llm.provider = %q, want %q", p.EmbeddingLLM.Provider, "ollama")
	}
	if p.EmbeddingLLM.Model != "nomic-embed-text" {
		t.Errorf("embedding_llm.model = %q, want %q", p.EmbeddingLLM.Model, "nomic-embed-text")
	}
	if p.EmbeddingLLM.BaseURL != baseURL {
		t.Errorf("embedding_llm.base_url = %q, want %q", p.EmbeddingLLM.BaseURL, baseURL)
	}
	if p.RAGLLM.Provider != "ollama" {
		t.Errorf("rag_llm.provider = %q, want %q", p.RAGLLM.Provider, "ollama")
	}
	if p.RAGLLM.Model != "llama3" {
		t.Errorf("rag_llm.model = %q, want %q", p.RAGLLM.Model, "llama3")
	}
}

func TestGenerateRAGConfig_Tables(t *testing.T) {
	idCol := "id"
	antKey := "sk-ant"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name: "default",
					Tables: []database.RAGPipelineTable{
						{Table: "docs", TextColumn: "content", VectorColumn: "embedding", IDColumn: &idCol},
						{Table: "notes", TextColumn: "body", VectorColumn: "vec"},
					},
					EmbeddingLLM: database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m", APIKey: &antKey},
					RAGLLM:       database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m", APIKey: &antKey},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	tables := cfg.Pipelines[0].Tables

	if len(tables) != 2 {
		t.Fatalf("tables len = %d, want 2", len(tables))
	}
	if tables[0].Table != "docs" {
		t.Errorf("tables[0].table = %q, want %q", tables[0].Table, "docs")
	}
	if tables[0].IDColumn != "id" {
		t.Errorf("tables[0].id_column = %q, want %q", tables[0].IDColumn, "id")
	}
	if tables[1].Table != "notes" {
		t.Errorf("tables[1].table = %q, want %q", tables[1].Table, "notes")
	}
	if tables[1].IDColumn != "" {
		t.Errorf("tables[1].id_column should be empty (omitted), got %q", tables[1].IDColumn)
	}
}

func TestGenerateRAGConfig_OptionalPipelineFields(t *testing.T) {
	antKey := "sk-ant"
	desc := "My pipeline"
	budget := 500
	topN := 5
	prompt := "You are a helpful assistant."
	hybridEnabled := false
	weight := 0.7

	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:         "default",
					Description:  &desc,
					Tables:       []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m", APIKey: &antKey},
					RAGLLM:       database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m", APIKey: &antKey},
					TokenBudget:  &budget,
					TopN:         &topN,
					SystemPrompt: &prompt,
					Search: &database.RAGPipelineSearch{
						HybridEnabled: &hybridEnabled,
						VectorWeight:  &weight,
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	p := cfg.Pipelines[0]

	if p.Description != desc {
		t.Errorf("description = %q, want %q", p.Description, desc)
	}
	if p.TokenBudget == nil || *p.TokenBudget != 500 {
		t.Errorf("token_budget = %v, want 500", p.TokenBudget)
	}
	if p.TopN == nil || *p.TopN != 5 {
		t.Errorf("top_n = %v, want 5", p.TopN)
	}
	if p.SystemPrompt != prompt {
		t.Errorf("system_prompt = %q, want %q", p.SystemPrompt, prompt)
	}
	if p.Search == nil {
		t.Fatal("search should be present")
	}
	if p.Search.HybridEnabled == nil || *p.Search.HybridEnabled != false {
		t.Errorf("search.hybrid_enabled = %v, want false", p.Search.HybridEnabled)
	}
	if p.Search.VectorWeight == nil || *p.Search.VectorWeight != 0.7 {
		t.Errorf("search.vector_weight = %v, want 0.7", p.Search.VectorWeight)
	}
}

func TestGenerateRAGConfig_OptionalPipelineFieldsOmitted(t *testing.T) {
	// When optional fields are not set, they must be absent from the YAML.
	data, err := GenerateRAGConfig(minimalRAGParams())
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)
	p := cfg.Pipelines[0]

	if p.Description != "" {
		t.Errorf("description should be empty (omitted), got %q", p.Description)
	}
	if p.TokenBudget != nil {
		t.Errorf("token_budget should be nil (omitted), got %v", *p.TokenBudget)
	}
	if p.TopN != nil {
		t.Errorf("top_n should be nil (omitted), got %v", *p.TopN)
	}
	if p.SystemPrompt != "" {
		t.Errorf("system_prompt should be empty (omitted), got %q", p.SystemPrompt)
	}
	if p.Search != nil {
		t.Errorf("search should be nil (omitted), got %+v", p.Search)
	}
}

func TestGenerateRAGConfig_MultiplePipelines(t *testing.T) {
	key1 := "sk-ant-1"
	key2 := "sk-openai-2"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:         "pipeline-a",
					Tables:       []database.RAGPipelineTable{{Table: "t1", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m1", APIKey: &key1},
					RAGLLM:       database.RAGPipelineLLMConfig{Provider: "anthropic", Model: "m1", APIKey: &key1},
				},
				{
					Name:         "pipeline-b",
					Tables:       []database.RAGPipelineTable{{Table: "t2", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{Provider: "openai", Model: "m2", APIKey: &key2},
					RAGLLM:       database.RAGPipelineLLMConfig{Provider: "openai", Model: "m2", APIKey: &key2},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if len(cfg.Pipelines) != 2 {
		t.Fatalf("pipelines len = %d, want 2", len(cfg.Pipelines))
	}
	if cfg.Pipelines[0].Name != "pipeline-a" {
		t.Errorf("pipelines[0].name = %q, want %q", cfg.Pipelines[0].Name, "pipeline-a")
	}
	if cfg.Pipelines[1].Name != "pipeline-b" {
		t.Errorf("pipelines[1].name = %q, want %q", cfg.Pipelines[1].Name, "pipeline-b")
	}
	// Each pipeline's api_keys paths use their own name prefix
	if cfg.Pipelines[0].APIKeys.Anthropic != "/app/keys/pipeline-a_rag.key" {
		t.Errorf("pipelines[0].api_keys.anthropic = %q, want %q",
			cfg.Pipelines[0].APIKeys.Anthropic, "/app/keys/pipeline-a_rag.key")
	}
	if cfg.Pipelines[1].APIKeys.OpenAI != "/app/keys/pipeline-b_rag.key" {
		t.Errorf("pipelines[1].api_keys.openai = %q, want %q",
			cfg.Pipelines[1].APIKeys.OpenAI, "/app/keys/pipeline-b_rag.key")
	}
}

func TestGenerateRAGConfig_DefaultsSection(t *testing.T) {
	budget := 2000
	topN := 20
	params := minimalRAGParams()
	params.Config.Defaults = &database.RAGDefaults{
		TokenBudget: &budget,
		TopN:        &topN,
	}

	data, err := GenerateRAGConfig(params)
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if cfg.Defaults == nil {
		t.Fatal("defaults section should be present when configured")
	}
	if cfg.Defaults.TokenBudget == nil || *cfg.Defaults.TokenBudget != 2000 {
		t.Errorf("defaults.token_budget = %v, want 2000", cfg.Defaults.TokenBudget)
	}
	if cfg.Defaults.TopN == nil || *cfg.Defaults.TopN != 20 {
		t.Errorf("defaults.top_n = %v, want 20", cfg.Defaults.TopN)
	}
}

func TestGenerateRAGConfig_DefaultsAbsent(t *testing.T) {
	// No Defaults set — defaults section must be omitted.
	data, err := GenerateRAGConfig(minimalRAGParams())
	if err != nil {
		t.Fatalf("GenerateRAGConfig() error = %v", err)
	}

	cfg := parseRAGYAML(t, data)

	if cfg.Defaults != nil {
		t.Errorf("defaults section should be absent when not configured, got %+v", cfg.Defaults)
	}
}

func TestGenerateRAGConfig_SameProviderDifferentKeys_ReturnsError(t *testing.T) {
	key1 := "sk-openai-embed"
	key2 := "sk-openai-rag-different"
	params := &RAGConfigParams{
		Config: &database.RAGServiceConfig{
			Pipelines: []database.RAGPipeline{
				{
					Name:   "default",
					Tables: []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v"}},
					EmbeddingLLM: database.RAGPipelineLLMConfig{
						Provider: "openai", Model: "text-embedding-3-small", APIKey: &key1,
					},
					RAGLLM: database.RAGPipelineLLMConfig{
						Provider: "openai", Model: "gpt-4o", APIKey: &key2,
					},
				},
			},
		},
		DatabaseName: "mydb", DatabaseHost: "host", DatabasePort: 5432,
		Username: "u", Password: "p", KeysDir: "/app/keys",
	}

	_, err := GenerateRAGConfig(params)
	if err == nil {
		t.Fatal("expected error for same-provider mismatched API keys, got nil")
	}
}
