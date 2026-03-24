package swarm

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/pgEdge/control-plane/server/internal/database"
)

func strPtr(s string) *string { return &s }
func mcpBoolPtr(b bool) *bool { return &b }

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
	// Minimal config: no LLM, no embedding — just database.
	params := &MCPConfigParams{
		Config:        &database.MCPServiceConfig{},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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

	// llm section should be absent
	if cfg.LLM != nil {
		t.Errorf("llm section should be absent when llm_enabled is not set, got %+v", cfg.LLM)
	}

	// builtins.tools.llm_connection_selection must be false
	if cfg.Builtins.Tools.LLMConnectionSelection == nil {
		t.Fatal("builtins.tools.llm_connection_selection is nil, want false")
	}
	if *cfg.Builtins.Tools.LLMConnectionSelection {
		t.Error("builtins.tools.llm_connection_selection should be false")
	}
}

func TestGenerateMCPConfig_LLMDisabled_SectionOmitted(t *testing.T) {
	params := &MCPConfigParams{
		Config:        &database.MCPServiceConfig{LLMEnabled: mcpBoolPtr(false)},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)
	if cfg.LLM != nil {
		t.Errorf("llm section should be absent when llm_enabled is false, got %+v", cfg.LLM)
	}
}

func TestGenerateMCPConfig_LLMEnabled_SectionPresent(t *testing.T) {
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMEnabled:      mcpBoolPtr(true),
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)
	if cfg.LLM == nil {
		t.Fatal("llm section should be present when llm_enabled is true")
	}
	if !cfg.LLM.Enabled {
		t.Error("llm.enabled should be true")
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("llm.provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-sonnet-4-5" {
		t.Errorf("llm.model = %q, want %q", cfg.LLM.Model, "claude-sonnet-4-5")
	}
}

func TestGenerateMCPConfig_LLMEnabled_DefaultTuning(t *testing.T) {
	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			LLMEnabled:      mcpBoolPtr(true),
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)
	if cfg.LLM == nil {
		t.Fatal("llm section should be present")
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("llm.temperature = %v, want 0.7", cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("llm.max_tokens = %d, want 4096", cfg.LLM.MaxTokens)
	}
}

func TestGenerateMCPConfig_DefaultValues(t *testing.T) {
	// No LLM, no optional fields — defaults should apply for database fields.
	params := &MCPConfigParams{
		Config:        &database.MCPServiceConfig{},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	// LLM section absent
	if cfg.LLM != nil {
		t.Errorf("llm section should be absent, got %+v", cfg.LLM)
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
			LLMEnabled:      mcpBoolPtr(true),
			LLMProvider:     "anthropic",
			LLMModel:        "claude-opus-4-6",
			AnthropicAPIKey: strPtr("sk-ant-api03-test"),
			LLMTemperature:  &temp,
			LLMMaxTokens:    &maxTok,
			PoolMaxConns:    &poolMax,
			AllowWrites:     &allowW,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM == nil {
		t.Fatal("llm section should be present")
	}
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
			LLMEnabled:      mcpBoolPtr(true),
			LLMProvider:     "anthropic",
			LLMModel:        "claude-sonnet-4-5",
			AnthropicAPIKey: &apiKey,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM == nil {
		t.Fatal("llm section should be present")
	}
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
			LLMEnabled:   mcpBoolPtr(true),
			LLMProvider:  "openai",
			LLMModel:     "gpt-4",
			OpenAIAPIKey: &apiKey,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM == nil {
		t.Fatal("llm section should be present")
	}
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
			LLMEnabled:  mcpBoolPtr(true),
			LLMProvider: "ollama",
			LLMModel:    "llama3",
			OllamaURL:   &ollamaURL,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM == nil {
		t.Fatal("llm section should be present")
	}
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
			LLMEnabled:        mcpBoolPtr(true),
			LLMProvider:       "anthropic",
			LLMModel:          "claude-sonnet-4-5",
			AnthropicAPIKey:   strPtr("sk-ant-api03-test"),
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
			EmbeddingAPIKey:   &embAPIKey,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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

func TestGenerateMCPConfig_EmbeddingWithoutLLM(t *testing.T) {
	// Embedding enabled without LLM — LLM section absent, embedding present.
	embProvider := "voyage"
	embModel := "voyage-3"
	embAPIKey := "pa-voyage-key"

	params := &MCPConfigParams{
		Config: &database.MCPServiceConfig{
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
			EmbeddingAPIKey:   &embAPIKey,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
	}

	data, err := GenerateMCPConfig(params)
	if err != nil {
		t.Fatalf("GenerateMCPConfig() error = %v", err)
	}

	cfg := parseYAML(t, data)

	if cfg.LLM != nil {
		t.Errorf("llm section should be absent when llm_enabled is not set, got %+v", cfg.LLM)
	}
	if cfg.Embedding == nil {
		t.Fatal("embedding section should be present")
	}
	if !cfg.Embedding.Enabled {
		t.Error("embedding.enabled should be true")
	}
	if cfg.Embedding.Provider != "voyage" {
		t.Errorf("embedding.provider = %q, want %q", cfg.Embedding.Provider, "voyage")
	}
}

func TestGenerateMCPConfig_EmbeddingAbsent(t *testing.T) {
	// No embedding_provider set — embedding section must be omitted.
	params := &MCPConfigParams{
		Config:        &database.MCPServiceConfig{},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
			LLMEnabled:        mcpBoolPtr(true),
			LLMProvider:       "openai",
			LLMModel:          "gpt-4",
			OpenAIAPIKey:      strPtr("sk-openai-llm"),
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
			EmbeddingAPIKey:   &embAPIKey,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
			LLMEnabled:        mcpBoolPtr(true),
			LLMProvider:       "ollama",
			LLMModel:          "llama3",
			OllamaURL:         &ollamaURL,
			EmbeddingProvider: &embProvider,
			EmbeddingModel:    &embModel,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
			DisableQueryDatabase:       &trueVal,
			DisableGetSchemaInfo:       &trueVal,
			DisableSimilaritySearch:    &trueVal,
			DisableExecuteExplain:      &trueVal,
			DisableGenerateEmbedding:   &trueVal,
			DisableSearchKnowledgebase: &trueVal,
			DisableCountRows:           &trueVal,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
		Config:        &database.MCPServiceConfig{},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
			DisableQueryDatabase: &falseVal,
		},
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "db-host", Port: 5432}},
		Username:      "appuser",
		Password:      "secret",
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
		Config:        &database.MCPServiceConfig{},
		DatabaseName:  "myspecialdb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "pg-primary.internal", Port: 5433}},
		Username:      "svc_myspecialdb",
		Password:      "supersecret",
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
	if len(db.Hosts) != 1 {
		t.Fatalf("databases[0].hosts len = %d, want 1", len(db.Hosts))
	}
	if db.Hosts[0].Host != "pg-primary.internal" {
		t.Errorf("databases[0].hosts[0].host = %q, want %q", db.Hosts[0].Host, "pg-primary.internal")
	}
	if db.Hosts[0].Port != 5433 {
		t.Errorf("databases[0].hosts[0].port = %d, want 5433", db.Hosts[0].Port)
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

// TestGenerateMCPConfig_MultiHostTopology exercises the full path from
// service spec → BuildServiceHostList → GenerateMCPConfig → YAML output.
// It verifies that the generated YAML contains the correct ordered hosts
// array and target_session_attrs for various topologies.
func TestGenerateMCPConfig_MultiHostTopology(t *testing.T) {
	// inst builds a minimal InstanceSpec for testing.
	inst := func(instanceID, hostID string) *database.InstanceSpec {
		return &database.InstanceSpec{
			InstanceID: instanceID,
			HostID:     hostID,
		}
	}

	// baseMCPConfig returns a minimal MCPServiceConfig (no LLM).
	baseMCPConfig := func() *database.MCPServiceConfig {
		return &database.MCPServiceConfig{}
	}

	// assertHostEntries verifies the YAML hosts array matches the expected
	// host:port pairs in order.
	assertHostEntries := func(t *testing.T, expected []database.ServiceHostEntry, actual []mcpHostEntry) {
		t.Helper()
		if len(actual) != len(expected) {
			t.Fatalf("hosts length = %d, want %d\n  got:  %v\n  want: %v", len(actual), len(expected), actual, expected)
		}
		for i := range expected {
			if actual[i].Host != expected[i].Host {
				t.Errorf("hosts[%d].host = %q, want %q", i, actual[i].Host, expected[i].Host)
			}
			if actual[i].Port != expected[i].Port {
				t.Errorf("hosts[%d].port = %d, want %d", i, actual[i].Port, expected[i].Port)
			}
		}
	}

	// he builds the expected ServiceHostEntry for a given instance ID
	// (mirrors the "postgres-{instanceID}" convention at port 5432).
	he := func(instanceID string) database.ServiceHostEntry {
		return database.ServiceHostEntry{
			Host: fmt.Sprintf("postgres-%s", instanceID),
			Port: 5432,
		}
	}

	t.Run("2-node multi-active service on host-1", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("inst1", "host-1")}},
			{NodeName: "n2", Instances: []*database.InstanceSpec{inst("inst2", "host-2")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPrimary
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		if len(cfg.Databases) != 1 {
			t.Fatalf("databases len = %d, want 1", len(cfg.Databases))
		}
		db := cfg.Databases[0]

		// Local node (n1 on host-1) should appear first.
		assertHostEntries(t, []database.ServiceHostEntry{
			he("inst1"),
			he("inst2"),
		}, db.Hosts)

		// allow_writes: true → target_session_attrs: primary
		if db.TargetSessionAttrs != database.TargetSessionAttrsPrimary {
			t.Errorf("target_session_attrs = %q, want %q", db.TargetSessionAttrs, database.TargetSessionAttrsPrimary)
		}
	})

	t.Run("HA within node service on replica host", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{
				NodeName: "n1",
				Instances: []*database.InstanceSpec{
					inst("inst1-h1", "host-1"),
					inst("inst1-h2", "host-2"),
				},
			},
			{NodeName: "n2", Instances: []*database.InstanceSpec{inst("inst2-h3", "host-3")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPrimary
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-2",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		db := cfg.Databases[0]

		// Co-located instance (inst1-h2 on host-2) first within n1,
		// then inst1-h1 (other n1 instance), then n2.
		assertHostEntries(t, []database.ServiceHostEntry{
			he("inst1-h2"),
			he("inst1-h1"),
			he("inst2-h3"),
		}, db.Hosts)

		if db.TargetSessionAttrs != database.TargetSessionAttrsPrimary {
			t.Errorf("target_session_attrs = %q, want %q", db.TargetSessionAttrs, database.TargetSessionAttrsPrimary)
		}
	})

	t.Run("target_nodes filter", func(t *testing.T) {
		targetNodes := []string{"n1", "n2"}
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("inst1", "host-1")}},
			{NodeName: "n2", Instances: []*database.InstanceSpec{inst("inst2", "host-2")}},
			{NodeName: "n3", Instances: []*database.InstanceSpec{inst("inst3", "host-3")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPrimary
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetNodes:        targetNodes,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		db := cfg.Databases[0]

		// Only n1 and n2 should be included; n3 excluded.
		assertHostEntries(t, []database.ServiceHostEntry{
			he("inst1"),
			he("inst2"),
		}, db.Hosts)
	})

	t.Run("allow_writes true derives primary", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("inst1", "host-1")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPrimary
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		if cfg.Databases[0].TargetSessionAttrs != database.TargetSessionAttrsPrimary {
			t.Errorf("target_session_attrs = %q, want %q", cfg.Databases[0].TargetSessionAttrs, database.TargetSessionAttrsPrimary)
		}
	})

	t.Run("allow_writes false derives prefer-standby", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("inst1", "host-1")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPreferStandby
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		if cfg.Databases[0].TargetSessionAttrs != database.TargetSessionAttrsPreferStandby {
			t.Errorf("target_session_attrs = %q, want %q", cfg.Databases[0].TargetSessionAttrs, database.TargetSessionAttrsPreferStandby)
		}
	})

	t.Run("explicit database_connection target_session_attrs overrides derived", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("inst1", "host-1")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsReadWrite
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		// Explicit "read-write" should override the allow_writes→"primary" default.
		if cfg.Databases[0].TargetSessionAttrs != database.TargetSessionAttrsReadWrite {
			t.Errorf("target_session_attrs = %q, want %q", cfg.Databases[0].TargetSessionAttrs, database.TargetSessionAttrsReadWrite)
		}
	})

	t.Run("single node single host", func(t *testing.T) {
		nodeInstances := []*database.NodeInstances{
			{NodeName: "n1", Instances: []*database.InstanceSpec{inst("only-inst", "host-1")}},
		}

		targetSessionAttrs := database.TargetSessionAttrsPreferStandby
		connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
			ServiceHostID:      "host-1",
			NodeInstances:      nodeInstances,
			TargetSessionAttrs: targetSessionAttrs,
		})
		if err != nil {
			t.Fatalf("BuildServiceHostList error: %v", err)
		}

		data, err := GenerateMCPConfig(&MCPConfigParams{
			Config:             baseMCPConfig(),
			DatabaseName:       "mydb",
			DatabaseHosts:      connInfo.Hosts,
			TargetSessionAttrs: connInfo.TargetSessionAttrs,
			Username:           "appuser",
			Password:           "secret",
		})
		if err != nil {
			t.Fatalf("GenerateMCPConfig error: %v", err)
		}

		cfg := parseYAML(t, data)
		db := cfg.Databases[0]

		// Single host in structured format (not legacy host/port).
		assertHostEntries(t, []database.ServiceHostEntry{
			he("only-inst"),
		}, db.Hosts)

		// No allow_writes set → prefer-standby
		if db.TargetSessionAttrs != database.TargetSessionAttrsPreferStandby {
			t.Errorf("target_session_attrs = %q, want %q", db.TargetSessionAttrs, database.TargetSessionAttrsPreferStandby)
		}
	})
}
