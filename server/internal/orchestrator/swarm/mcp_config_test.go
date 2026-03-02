package swarm

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"gopkg.in/yaml.v3"
)

func strPtr(s string) *string       { return &s }
func float64Ptr(f float64) *float64 { return &f }
func intPtrMCP(i int) *int          { return &i }
func boolPtrMCP(b bool) *bool       { return &b }

// parseYAML unmarshals GenerateMCPConfig output into mcpYAMLConfig for assertion.
func parseYAML(t *testing.T, data []byte) *mcpYAMLConfig {
	t.Helper()
	var cfg mcpYAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v\nYAML:\n%s", err, string(data))
	}
	return &cfg
}

func TestGenerateMCPConfig_MinimalConfig(t *testing.T) {
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	// http section
	if !cfg.HTTP.Enabled {
		t.Error("http.enabled should be true")
	}
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("http.address = %q, want %q", cfg.HTTP.Address, ":8080")
	}
	if !cfg.HTTP.Auth.Enabled {
		t.Error("http.auth.enabled should be true")
	}
	if cfg.HTTP.Auth.TokenFile != "/app/data/tokens.yaml" {
		t.Errorf("http.auth.token_file = %q, want %q", cfg.HTTP.Auth.TokenFile, "/app/data/tokens.yaml")
	}
	if cfg.HTTP.Auth.UserFile != "/app/data/users.yaml" {
		t.Errorf("http.auth.user_file = %q, want %q", cfg.HTTP.Auth.UserFile, "/app/data/users.yaml")
	}

	// databases section
	if len(cfg.Databases) != 1 {
		t.Fatalf("databases len = %d, want 1", len(cfg.Databases))
	}

	// llm section
	if !cfg.LLM.Enabled {
		t.Error("llm.enabled should be true")
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("llm.provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}

	// builtins.tools.llm_connection_selection must be false
	if cfg.Builtins.Tools.LLMConnectionSelection == nil {
		t.Fatal("builtins.tools.llm_connection_selection is nil, want false")
	}
	if *cfg.Builtins.Tools.LLMConnectionSelection {
		t.Error("builtins.tools.llm_connection_selection should be false")
	}
}

func TestGenerateMCPConfig_DefaultValues(t *testing.T) {
	// No optional fields set — defaults should apply.
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
			// LLMTemperature, LLMMaxTokens, PoolMaxConns, AllowWrites all nil
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("llm.temperature = %v, want 0.7", cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("llm.max_tokens = %d, want 4096", cfg.LLM.MaxTokens)
	}
	if len(cfg.Databases) != 1 {
		t.Fatalf("databases len = %d, want 1", len(cfg.Databases))
	}
	if cfg.Databases[0].Pool.MaxConns != 4 {
		t.Errorf("databases[0].pool.max_conns = %d, want 4", cfg.Databases[0].Pool.MaxConns)
	}
	if cfg.Databases[0].AllowWrites {
		t.Error("databases[0].allow_writes should default to false")
	}
}

func TestGenerateMCPConfig_CustomValues(t *testing.T) {
	temp := 1.2
	maxTok := 8192
	poolMax := 10
	allowW := true

	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-opus-4-6",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
			LLMTemperature:  &temp,
			LLMMaxTokens:    &maxTok,
			PoolMaxConns:    &poolMax,
			AllowWrites:     &allowW,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM.Temperature != 1.2 {
		t.Errorf("llm.temperature = %v, want 1.2", cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens != 8192 {
		t.Errorf("llm.max_tokens = %d, want 8192", cfg.LLM.MaxTokens)
	}
	if len(cfg.Databases) != 1 {
		t.Fatalf("databases len = %d, want 1", len(cfg.Databases))
	}
	if cfg.Databases[0].Pool.MaxConns != 10 {
		t.Errorf("databases[0].pool.max_conns = %d, want 10", cfg.Databases[0].Pool.MaxConns)
	}
	if !cfg.Databases[0].AllowWrites {
		t.Error("databases[0].allow_writes should be true")
	}
}

func TestGenerateMCPConfig_ProviderKeys_Anthropic(t *testing.T) {
	apiKey := "sk-ant-api03-test"
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: &apiKey,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM.AnthropicAPIKey != apiKey {
		t.Errorf("llm.anthropic_api_key = %q, want %q", cfg.LLM.AnthropicAPIKey, apiKey)
	}
	if cfg.LLM.OpenAIAPIKey != "" {
		t.Errorf("llm.openai_api_key should be empty for anthropic provider, got %q", cfg.LLM.OpenAIAPIKey)
	}
	if cfg.LLM.OllamaURL != "" {
		t.Errorf("llm.ollama_url should be empty for anthropic provider, got %q", cfg.LLM.OllamaURL)
	}
}

func TestGenerateMCPConfig_ProviderKeys_OpenAI(t *testing.T) {
	apiKey := "sk-openai-test"
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:  "openai",
			LLMModel:     "gpt-4",
			OpenAIAPIKey: &apiKey,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM.OpenAIAPIKey != apiKey {
		t.Errorf("llm.openai_api_key = %q, want %q", cfg.LLM.OpenAIAPIKey, apiKey)
	}
	if cfg.LLM.AnthropicAPIKey != "" {
		t.Errorf("llm.anthropic_api_key should be empty for openai provider, got %q", cfg.LLM.AnthropicAPIKey)
	}
	if cfg.LLM.OllamaURL != "" {
		t.Errorf("llm.ollama_url should be empty for openai provider, got %q", cfg.LLM.OllamaURL)
	}
}

func TestGenerateMCPConfig_ProviderKeys_Ollama(t *testing.T) {
	ollamaURL := "http://localhost:11434"
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider: "ollama",
			LLMModel:    "llama3",
			OllamaURL:   &ollamaURL,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM.OllamaURL != ollamaURL {
		t.Errorf("llm.ollama_url = %q, want %q", cfg.LLM.OllamaURL, ollamaURL)
	}
	if cfg.LLM.AnthropicAPIKey != "" {
		t.Errorf("llm.anthropic_api_key should be empty for ollama provider, got %q", cfg.LLM.AnthropicAPIKey)
	}
	if cfg.LLM.OpenAIAPIKey != "" {
		t.Errorf("llm.openai_api_key should be empty for ollama provider, got %q", cfg.LLM.OpenAIAPIKey)
	}
}

func TestGenerateMCPConfig_EmbeddingPresent(t *testing.T) {
	embProvider := "voyage"
	embModel := "voyage-3"
	embAPIKey := "pa-voyage-key"

	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:       "anthropic",
			LLMModel:          "claude-sonnet-4-5",
			AnthropicAPIKey:   strPtr("sk-ant-api03-test"),
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
			EmbeddingAPIKey:   &embAPIKey,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.Embedding == nil {
		t.Fatal("embedding section should be present when embedding_provider is set")
	}
	if !cfg.Embedding.Enabled {
		t.Error("embedding.enabled should be true")
	}
	if cfg.Embedding.Provider != "voyage" {
		t.Errorf("embedding.provider = %q, want %q", cfg.Embedding.Provider, "voyage")
	}
	if cfg.Embedding.Model != "voyage-3" {
		t.Errorf("embedding.model = %q, want %q", cfg.Embedding.Model, "voyage-3")
	}
	if cfg.Embedding.VoyageAPIKey != "pa-voyage-key" {
		t.Errorf("embedding.voyage_api_key = %q, want %q", cfg.Embedding.VoyageAPIKey, "pa-voyage-key")
	}
}

func TestGenerateMCPConfig_EmbeddingAbsent(t *testing.T) {
	// No embedding_provider set — embedding section must be omitted.
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.Embedding != nil {
		t.Errorf("embedding section should be absent when embedding_provider is not set, got %+v", cfg.Embedding)
	}
}

func TestGenerateMCPConfig_EmbeddingOpenAI(t *testing.T) {
	embProvider := "openai"
	embModel := "text-embedding-3-small"
	embAPIKey := "sk-openai-embed"

	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:       "openai",
			LLMModel:          "gpt-4",
			OpenAIAPIKey:      strPtr("sk-openai-llm"),
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
			EmbeddingAPIKey:   &embAPIKey,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.Embedding == nil {
		t.Fatal("embedding section should be present")
	}
	if cfg.Embedding.OpenAIAPIKey != "sk-openai-embed" {
		t.Errorf("embedding.openai_api_key = %q, want %q", cfg.Embedding.OpenAIAPIKey, "sk-openai-embed")
	}
	if cfg.Embedding.VoyageAPIKey != "" {
		t.Errorf("embedding.voyage_api_key should be empty for openai embedding, got %q", cfg.Embedding.VoyageAPIKey)
	}
}

func TestGenerateMCPConfig_EmbeddingOllama(t *testing.T) {
	embProvider := "ollama"
	embModel := "nomic-embed-text"
	ollamaURL := "http://localhost:11434"

	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:       "ollama",
			LLMModel:          "llama3",
			OllamaURL:         &ollamaURL,
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.Embedding == nil {
		t.Fatal("embedding section should be present")
	}
	if cfg.Embedding.OllamaURL != ollamaURL {
		t.Errorf("embedding.ollama_url = %q, want %q", cfg.Embedding.OllamaURL, ollamaURL)
	}
}

func TestGenerateMCPConfig_ToolToggles_AllDisabled(t *testing.T) {
	trueVal := true
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:                "anthropic",
			LLMModel:                   "claude-sonnet-4-5",
			AnthropicAPIKey:            strPtr("sk-ant-api03-test"),
			DisableQueryDatabase:       &trueVal,
			DisableGetSchemaInfo:       &trueVal,
			DisableSimilaritySearch:    &trueVal,
			DisableExecuteExplain:      &trueVal,
			DisableGenerateEmbedding:   &trueVal,
			DisableSearchKnowledgebase: &trueVal,
			DisableCountRows:           &trueVal,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)
	tools := cfg.Builtins.Tools

	assertBoolPtrFalse := func(name string, ptr *bool) {
		t.Helper()
		if ptr == nil {
			t.Errorf("%s: expected *false, got nil", name)
			return
		}
		if *ptr {
			t.Errorf("%s: expected false, got true", name)
		}
	}

	assertBoolPtrFalse("query_database", tools.QueryDatabase)
	assertBoolPtrFalse("get_schema_info", tools.GetSchemaInfo)
	assertBoolPtrFalse("similarity_search", tools.SimilaritySearch)
	assertBoolPtrFalse("execute_explain", tools.ExecuteExplain)
	assertBoolPtrFalse("generate_embedding", tools.GenerateEmbedding)
	assertBoolPtrFalse("search_knowledgebase", tools.SearchKnowledgebase)
	assertBoolPtrFalse("count_rows", tools.CountRows)

	// llm_connection_selection always false
	assertBoolPtrFalse("llm_connection_selection", tools.LLMConnectionSelection)
}

func TestGenerateMCPConfig_ToolToggles_NoneDisabled(t *testing.T) {
	// No disable_* flags set — all tool fields should be omitted (nil), except
	// llm_connection_selection which is always explicitly false.
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)
	tools := cfg.Builtins.Tools

	assertNil := func(name string, ptr *bool) {
		t.Helper()
		if ptr != nil {
			t.Errorf("%s: expected nil (omitted), got %v", name, *ptr)
		}
	}

	assertNil("query_database", tools.QueryDatabase)
	assertNil("get_schema_info", tools.GetSchemaInfo)
	assertNil("similarity_search", tools.SimilaritySearch)
	assertNil("execute_explain", tools.ExecuteExplain)
	assertNil("generate_embedding", tools.GenerateEmbedding)
	assertNil("search_knowledgebase", tools.SearchKnowledgebase)
	assertNil("count_rows", tools.CountRows)

	// llm_connection_selection always present as false
	if tools.LLMConnectionSelection == nil {
		t.Fatal("llm_connection_selection should always be present")
	}
	if *tools.LLMConnectionSelection {
		t.Error("llm_connection_selection should always be false")
	}
}

func TestGenerateMCPConfig_ToolToggles_DisableFalseIsNoop(t *testing.T) {
	// Setting disable_* to false should NOT write the field (field stays nil/omitted).
	falseVal := false
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:          "anthropic",
			LLMModel:             "claude-sonnet-4-5",
			AnthropicAPIKey:      strPtr("sk-ant-api03-test"),
			DisableQueryDatabase: &falseVal,
		},
		DatabaseName: "mydb",
		DatabaseHost: "db-host",
		DatabasePort: 5432,
		Username:     "appuser",
		Password:     "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	// query_database: disable flag is false → not explicitly disabled → field should be nil
	if cfg.Builtins.Tools.QueryDatabase != nil {
		t.Errorf("query_database: expected nil when disable flag is false, got %v", *cfg.Builtins.Tools.QueryDatabase)
	}
}

func TestGenerateMCPConfig_DatabaseConfig(t *testing.T) {
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName: "myspecialdb",
		DatabaseHost: "pg-primary.internal",
		DatabasePort: 5433,
		Username:     "svc_myspecialdb",
		Password:     "supersecret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if len(cfg.Databases) != 1 {
		t.Fatalf("databases len = %d, want 1", len(cfg.Databases))
	}
	db := cfg.Databases[0]

	if db.Name != "myspecialdb" {
		t.Errorf("databases[0].name = %q, want %q", db.Name, "myspecialdb")
	}
	if db.Database != "myspecialdb" {
		t.Errorf("databases[0].database = %q, want %q", db.Database, "myspecialdb")
	}
	if db.Host != "pg-primary.internal" {
		t.Errorf("databases[0].host = %q, want %q", db.Host, "pg-primary.internal")
	}
	if db.Port != 5433 {
		t.Errorf("databases[0].port = %d, want 5433", db.Port)
	}
	if db.User != "svc_myspecialdb" {
		t.Errorf("databases[0].user = %q, want %q", db.User, "svc_myspecialdb")
	}
	if db.Password != "supersecret" {
		t.Errorf("databases[0].password = %q, want %q", db.Password, "supersecret")
	}
	if db.SSLMode != "prefer" {
		t.Errorf("databases[0].sslmode = %q, want %q", db.SSLMode, "prefer")
	}
}
