package database_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptr helpers to keep test cases terse.
func strPtr(s string) *string       { return &s }
func boolPtr(b bool) *bool          { return &b }
func intPtr(i int) *int             { return &i }
func float64Ptr(f float64) *float64 { return &f }

// anthropicBase returns a minimal valid config for the anthropic provider.
func anthropicBase() map[string]any {
	return map[string]any{
		"llm_provider":      "anthropic",
		"llm_model":         "claude-3-5-sonnet-20241022",
		"anthropic_api_key": "sk-ant-key",
	}
}

// openaiBase returns a minimal valid config for the openai provider.
func openaiBase() map[string]any {
	return map[string]any{
		"llm_provider":   "openai",
		"llm_model":      "gpt-4o",
		"openai_api_key": "sk-openai-key",
	}
}

// ollamaBase returns a minimal valid config for the ollama provider.
func ollamaBase() map[string]any {
	return map[string]any{
		"llm_provider": "ollama",
		"llm_model":    "llama3.2",
		"ollama_url":   "http://localhost:11434",
	}
}

// joinedErr joins a []error into a single error for assertion convenience.
func joinedErr(errs []error) error {
	return errors.Join(errs...)
}

func TestParseMCPServiceConfig(t *testing.T) {
	t.Run("happy paths", func(t *testing.T) {
		t.Run("minimal anthropic config", func(t *testing.T) {
			cfg, errs := database.ParseMCPServiceConfig(anthropicBase(), false)
			require.Empty(t, errs)
			assert.Equal(t, "anthropic", cfg.LLMProvider)
			assert.Equal(t, "claude-3-5-sonnet-20241022", cfg.LLMModel)
			require.NotNil(t, cfg.AnthropicAPIKey)
			assert.Equal(t, "sk-ant-key", *cfg.AnthropicAPIKey)
			assert.Nil(t, cfg.OpenAIAPIKey)
			assert.Nil(t, cfg.OllamaURL)
		})

		t.Run("minimal openai config", func(t *testing.T) {
			cfg, errs := database.ParseMCPServiceConfig(openaiBase(), false)
			require.Empty(t, errs)
			assert.Equal(t, "openai", cfg.LLMProvider)
			assert.Equal(t, "gpt-4o", cfg.LLMModel)
			require.NotNil(t, cfg.OpenAIAPIKey)
			assert.Equal(t, "sk-openai-key", *cfg.OpenAIAPIKey)
			assert.Nil(t, cfg.AnthropicAPIKey)
			assert.Nil(t, cfg.OllamaURL)
		})

		t.Run("minimal ollama config", func(t *testing.T) {
			cfg, errs := database.ParseMCPServiceConfig(ollamaBase(), false)
			require.Empty(t, errs)
			assert.Equal(t, "ollama", cfg.LLMProvider)
			assert.Equal(t, "llama3.2", cfg.LLMModel)
			require.NotNil(t, cfg.OllamaURL)
			assert.Equal(t, "http://localhost:11434", *cfg.OllamaURL)
			assert.Nil(t, cfg.AnthropicAPIKey)
			assert.Nil(t, cfg.OpenAIAPIKey)
		})

		t.Run("all optional fields populated", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":                 "anthropic",
				"llm_model":                    "claude-3-5-sonnet-20241022",
				"anthropic_api_key":            "sk-ant-key",
				"allow_writes":                 true,
				"init_token":                   "my-init-token",
				"init_users":                   []any{map[string]any{"username": "alice", "password": "secret"}},
				"embedding_provider":           "voyage",
				"embedding_model":              "voyage-3",
				"embedding_api_key":            "voy-key",
				"llm_temperature":              float64(0.7),
				"llm_max_tokens":               float64(2048),
				"pool_max_conns":               float64(10),
				"disable_query_database":       true,
				"disable_get_schema_info":      false,
				"disable_similarity_search":    true,
				"disable_execute_explain":      false,
				"disable_generate_embedding":   true,
				"disable_search_knowledgebase": false,
				"disable_count_rows":           true,
			}
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)

			assert.Equal(t, "anthropic", cfg.LLMProvider)
			assert.Equal(t, "claude-3-5-sonnet-20241022", cfg.LLMModel)
			require.NotNil(t, cfg.AllowWrites)
			assert.True(t, *cfg.AllowWrites)
			require.NotNil(t, cfg.InitToken)
			assert.Equal(t, "my-init-token", *cfg.InitToken)
			require.Len(t, cfg.InitUsers, 1)
			assert.Equal(t, "alice", cfg.InitUsers[0].Username)
			assert.Equal(t, "secret", cfg.InitUsers[0].Password)
			require.NotNil(t, cfg.EmbeddingProvider)
			assert.Equal(t, "voyage", *cfg.EmbeddingProvider)
			require.NotNil(t, cfg.EmbeddingModel)
			assert.Equal(t, "voyage-3", *cfg.EmbeddingModel)
			require.NotNil(t, cfg.EmbeddingAPIKey)
			assert.Equal(t, "voy-key", *cfg.EmbeddingAPIKey)
			require.NotNil(t, cfg.LLMTemperature)
			assert.InDelta(t, 0.7, *cfg.LLMTemperature, 1e-9)
			require.NotNil(t, cfg.LLMMaxTokens)
			assert.Equal(t, 2048, *cfg.LLMMaxTokens)
			require.NotNil(t, cfg.PoolMaxConns)
			assert.Equal(t, 10, *cfg.PoolMaxConns)
			require.NotNil(t, cfg.DisableQueryDatabase)
			assert.True(t, *cfg.DisableQueryDatabase)
			require.NotNil(t, cfg.DisableGetSchemaInfo)
			assert.False(t, *cfg.DisableGetSchemaInfo)
			require.NotNil(t, cfg.DisableSimilaritySearch)
			assert.True(t, *cfg.DisableSimilaritySearch)
			require.NotNil(t, cfg.DisableExecuteExplain)
			assert.False(t, *cfg.DisableExecuteExplain)
			require.NotNil(t, cfg.DisableGenerateEmbedding)
			assert.True(t, *cfg.DisableGenerateEmbedding)
			require.NotNil(t, cfg.DisableSearchKnowledgebase)
			assert.False(t, *cfg.DisableSearchKnowledgebase)
			require.NotNil(t, cfg.DisableCountRows)
			assert.True(t, *cfg.DisableCountRows)
		})
	})

	t.Run("required fields", func(t *testing.T) {
		t.Run("missing llm_provider", func(t *testing.T) {
			config := map[string]any{
				"llm_model":         "claude-3-5-sonnet-20241022",
				"anthropic_api_key": "sk-ant-key",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_provider is required")
		})

		t.Run("missing llm_model", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "anthropic",
				"anthropic_api_key": "sk-ant-key",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_model is required")
		})

		t.Run("empty llm_provider", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "",
				"llm_model":         "claude-3-5-sonnet-20241022",
				"anthropic_api_key": "sk-ant-key",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_provider must not be empty")
		})

		t.Run("empty llm_model", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "anthropic",
				"llm_model":         "",
				"anthropic_api_key": "sk-ant-key",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_model must not be empty")
		})
	})

	t.Run("provider cross-validation", func(t *testing.T) {
		t.Run("anthropic without anthropic_api_key", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": "anthropic",
				"llm_model":    "claude-3-5-sonnet-20241022",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "anthropic_api_key is required when llm_provider is")
		})

		t.Run("anthropic with empty anthropic_api_key", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "anthropic",
				"llm_model":         "claude-3-5-sonnet-20241022",
				"anthropic_api_key": "",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "anthropic_api_key must not be empty")
		})

		t.Run("openai without openai_api_key", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": "openai",
				"llm_model":    "gpt-4o",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "openai_api_key is required when llm_provider is")
		})

		t.Run("openai with empty openai_api_key", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":   "openai",
				"llm_model":      "gpt-4o",
				"openai_api_key": "",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "openai_api_key must not be empty")
		})

		t.Run("ollama without ollama_url", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": "ollama",
				"llm_model":    "llama3.2",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "ollama_url is required when llm_provider is")
		})

		t.Run("ollama with empty ollama_url", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": "ollama",
				"llm_model":    "llama3.2",
				"ollama_url":   "",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "ollama_url must not be empty")
		})
	})

	t.Run("invalid provider", func(t *testing.T) {
		t.Run("unknown llm_provider value", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": "bedrock",
				"llm_model":    "some-model",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			combined := joinedErr(errs).Error()
			assert.Contains(t, combined, "llm_provider must be one of")
			assert.Contains(t, combined, "anthropic")
			assert.Contains(t, combined, "openai")
			assert.Contains(t, combined, "ollama")
		})
	})

	t.Run("type errors", func(t *testing.T) {
		t.Run("llm_provider wrong type", func(t *testing.T) {
			config := map[string]any{
				"llm_provider": 42,
				"llm_model":    "some-model",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_provider must be a string")
		})

		t.Run("llm_model wrong type", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "anthropic",
				"llm_model":         true,
				"anthropic_api_key": "sk-ant-key",
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_model must be a string")
		})

		t.Run("anthropic_api_key wrong type", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":      "anthropic",
				"llm_model":         "claude-3-5-sonnet-20241022",
				"anthropic_api_key": 12345,
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "anthropic_api_key must be a string")
		})

		t.Run("allow_writes wrong type", func(t *testing.T) {
			config := anthropicBase()
			config["allow_writes"] = "yes"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "allow_writes must be a boolean")
		})

		t.Run("llm_temperature wrong type (string)", func(t *testing.T) {
			config := anthropicBase()
			config["llm_temperature"] = "warm"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_temperature must be a number")
		})

		t.Run("llm_max_tokens wrong type (string)", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = "lots"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_max_tokens must be an integer")
		})

		t.Run("pool_max_conns wrong type (bool)", func(t *testing.T) {
			config := anthropicBase()
			config["pool_max_conns"] = false
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "pool_max_conns must be an integer")
		})

		t.Run("llm_max_tokens non-integer float", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = float64(10.5)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_max_tokens must be an integer")
		})

		t.Run("init_token wrong type", func(t *testing.T) {
			config := anthropicBase()
			config["init_token"] = 9999
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_token must be a string")
		})
	})

	t.Run("unknown keys", func(t *testing.T) {
		t.Run("single unknown key", func(t *testing.T) {
			config := anthropicBase()
			config["mystery_field"] = "value"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), `"mystery_field"`)
		})

		t.Run("multiple unknown keys", func(t *testing.T) {
			config := anthropicBase()
			config["aaa_unknown"] = "x"
			config["zzz_unknown"] = "y"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			combined := joinedErr(errs).Error()
			assert.Contains(t, combined, `"aaa_unknown"`)
			assert.Contains(t, combined, `"zzz_unknown"`)
		})
	})

	t.Run("optional field validation", func(t *testing.T) {
		t.Run("llm_temperature below zero", func(t *testing.T) {
			config := anthropicBase()
			config["llm_temperature"] = float64(-0.1)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_temperature must be between 0.0 and 2.0")
		})

		t.Run("llm_temperature above 2.0", func(t *testing.T) {
			config := anthropicBase()
			config["llm_temperature"] = float64(2.1)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_temperature must be between 0.0 and 2.0")
		})

		t.Run("llm_temperature boundary values are valid", func(t *testing.T) {
			for _, temp := range []float64{0.0, 2.0} {
				config := anthropicBase()
				config["llm_temperature"] = temp
				cfg, errs := database.ParseMCPServiceConfig(config, false)
				require.Empty(t, errs)
				require.NotNil(t, cfg.LLMTemperature)
				assert.InDelta(t, temp, *cfg.LLMTemperature, 1e-9)
			}
		})

		t.Run("llm_max_tokens zero", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = float64(0)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_max_tokens must be a positive integer")
		})

		t.Run("llm_max_tokens negative", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = float64(-100)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_max_tokens must be a positive integer")
		})

		t.Run("pool_max_conns zero", func(t *testing.T) {
			config := anthropicBase()
			config["pool_max_conns"] = float64(0)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "pool_max_conns must be a positive integer")
		})

		t.Run("pool_max_conns negative", func(t *testing.T) {
			config := anthropicBase()
			config["pool_max_conns"] = float64(-5)
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "pool_max_conns must be a positive integer")
		})
	})

	t.Run("embedding config", func(t *testing.T) {
		t.Run("valid voyage embedding config", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "voyage"
			config["embedding_model"] = "voyage-3"
			config["embedding_api_key"] = "voy-key"
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.EmbeddingProvider)
			assert.Equal(t, "voyage", *cfg.EmbeddingProvider)
			require.NotNil(t, cfg.EmbeddingModel)
			assert.Equal(t, "voyage-3", *cfg.EmbeddingModel)
			require.NotNil(t, cfg.EmbeddingAPIKey)
			assert.Equal(t, "voy-key", *cfg.EmbeddingAPIKey)
		})

		t.Run("valid openai embedding config", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "openai"
			config["embedding_model"] = "text-embedding-3-small"
			config["embedding_api_key"] = "sk-embed-key"
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.EmbeddingProvider)
			assert.Equal(t, "openai", *cfg.EmbeddingProvider)
		})

		t.Run("valid ollama embedding config (no api key required)", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "ollama"
			config["embedding_model"] = "nomic-embed-text"
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.EmbeddingProvider)
			assert.Equal(t, "ollama", *cfg.EmbeddingProvider)
			assert.Nil(t, cfg.EmbeddingAPIKey)
		})

		t.Run("embedding_provider without embedding_model", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "voyage"
			config["embedding_api_key"] = "voy-key"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "embedding_model is required when embedding_provider is set")
		})

		t.Run("voyage without embedding_api_key", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "voyage"
			config["embedding_model"] = "voyage-3"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), `embedding_api_key is required when embedding_provider is "voyage"`)
		})

		t.Run("openai embedding without embedding_api_key", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "openai"
			config["embedding_model"] = "text-embedding-3-small"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), `embedding_api_key is required when embedding_provider is "openai"`)
		})

		t.Run("unknown embedding_provider", func(t *testing.T) {
			config := anthropicBase()
			config["embedding_provider"] = "bedrock"
			config["embedding_model"] = "some-model"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "embedding_provider must be one of")
		})
	})

	t.Run("init_users", func(t *testing.T) {
		t.Run("valid single user", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": "hunter2"},
			}
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.Len(t, cfg.InitUsers, 1)
			assert.Equal(t, "alice", cfg.InitUsers[0].Username)
			assert.Equal(t, "hunter2", cfg.InitUsers[0].Password)
		})

		t.Run("valid multiple users", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": "pass1"},
				map[string]any{"username": "bob", "password": "pass2"},
			}
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.Len(t, cfg.InitUsers, 2)
			assert.Equal(t, "alice", cfg.InitUsers[0].Username)
			assert.Equal(t, "bob", cfg.InitUsers[1].Username)
		})

		t.Run("missing username", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"password": "secret"},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].username is required")
		})

		t.Run("missing password", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice"},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].password is required")
		})

		t.Run("empty username", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "", "password": "secret"},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].username must not be empty")
		})

		t.Run("empty password", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": ""},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].password must not be empty")
		})

		t.Run("duplicate usernames", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": "pass1"},
				map[string]any{"username": "alice", "password": "pass2"},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), `duplicate username "alice"`)
		})

		t.Run("empty array rejected", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users must contain at least one entry")
		})

		t.Run("non-array type", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = "alice"
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users must be an array")
		})

		t.Run("non-object entry", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{"alice"}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0] must be an object")
		})

		t.Run("username wrong type", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": 42, "password": "secret"},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].username must be a string")
		})

		t.Run("password wrong type", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": true},
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users[0].password must be a string")
		})
	})

	t.Run("init_token", func(t *testing.T) {
		t.Run("valid token string", func(t *testing.T) {
			config := anthropicBase()
			config["init_token"] = "my-secret-token"
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.InitToken)
			assert.Equal(t, "my-secret-token", *cfg.InitToken)
		})
	})

	t.Run("isUpdate=true", func(t *testing.T) {
		t.Run("init_token rejected on update", func(t *testing.T) {
			config := anthropicBase()
			config["init_token"] = "some-token"
			_, errs := database.ParseMCPServiceConfig(config, true)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_token can only be set during initial provisioning")
		})

		t.Run("init_users rejected on update", func(t *testing.T) {
			config := anthropicBase()
			config["init_users"] = []any{
				map[string]any{"username": "alice", "password": "pass"},
			}
			_, errs := database.ParseMCPServiceConfig(config, true)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "init_users can only be set during initial provisioning")
		})

		t.Run("valid update without bootstrap fields", func(t *testing.T) {
			cfg, errs := database.ParseMCPServiceConfig(anthropicBase(), true)
			require.Empty(t, errs)
			assert.Equal(t, "anthropic", cfg.LLMProvider)
			assert.Nil(t, cfg.InitToken)
			assert.Nil(t, cfg.InitUsers)
		})
	})

	t.Run("multiple errors", func(t *testing.T) {
		t.Run("missing both required fields returns multiple errors", func(t *testing.T) {
			config := map[string]any{}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			// Both errors are separate entries in the slice
			combined := joinedErr(errs).Error()
			assert.Contains(t, combined, "llm_provider is required")
			assert.Contains(t, combined, "llm_model is required")
			assert.Greater(t, len(errs), 1, "expected multiple errors in slice")
		})

		t.Run("unknown key plus missing required field accumulates errors", func(t *testing.T) {
			config := map[string]any{
				"llm_provider":  "anthropic",
				"mystery_field": "oops",
				// llm_model missing, anthropic_api_key missing
			}
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			combined := joinedErr(errs).Error()
			assert.Contains(t, combined, "mystery_field")
			assert.Contains(t, combined, "llm_model is required")
			assert.Contains(t, combined, "anthropic_api_key")
		})

		t.Run("init_token and init_users both rejected on update", func(t *testing.T) {
			config := anthropicBase()
			config["init_token"] = "tok"
			config["init_users"] = []any{map[string]any{"username": "alice", "password": "pass"}}
			_, errs := database.ParseMCPServiceConfig(config, true)
			require.NotEmpty(t, errs)
			combined := joinedErr(errs).Error()
			assert.Contains(t, combined, "init_token can only be set during initial provisioning")
			assert.Contains(t, combined, "init_users can only be set during initial provisioning")
		})
	})

	t.Run("tool toggles", func(t *testing.T) {
		t.Run("all disable fields parsed correctly when true", func(t *testing.T) {
			config := anthropicBase()
			config["disable_query_database"] = true
			config["disable_get_schema_info"] = true
			config["disable_similarity_search"] = true
			config["disable_execute_explain"] = true
			config["disable_generate_embedding"] = true
			config["disable_search_knowledgebase"] = true
			config["disable_count_rows"] = true

			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)

			require.NotNil(t, cfg.DisableQueryDatabase)
			assert.True(t, *cfg.DisableQueryDatabase)
			require.NotNil(t, cfg.DisableGetSchemaInfo)
			assert.True(t, *cfg.DisableGetSchemaInfo)
			require.NotNil(t, cfg.DisableSimilaritySearch)
			assert.True(t, *cfg.DisableSimilaritySearch)
			require.NotNil(t, cfg.DisableExecuteExplain)
			assert.True(t, *cfg.DisableExecuteExplain)
			require.NotNil(t, cfg.DisableGenerateEmbedding)
			assert.True(t, *cfg.DisableGenerateEmbedding)
			require.NotNil(t, cfg.DisableSearchKnowledgebase)
			assert.True(t, *cfg.DisableSearchKnowledgebase)
			require.NotNil(t, cfg.DisableCountRows)
			assert.True(t, *cfg.DisableCountRows)
		})

		t.Run("all disable fields parsed correctly when false", func(t *testing.T) {
			config := anthropicBase()
			config["disable_query_database"] = false
			config["disable_get_schema_info"] = false
			config["disable_similarity_search"] = false
			config["disable_execute_explain"] = false
			config["disable_generate_embedding"] = false
			config["disable_search_knowledgebase"] = false
			config["disable_count_rows"] = false

			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)

			require.NotNil(t, cfg.DisableQueryDatabase)
			assert.False(t, *cfg.DisableQueryDatabase)
			require.NotNil(t, cfg.DisableGetSchemaInfo)
			assert.False(t, *cfg.DisableGetSchemaInfo)
			require.NotNil(t, cfg.DisableSimilaritySearch)
			assert.False(t, *cfg.DisableSimilaritySearch)
			require.NotNil(t, cfg.DisableExecuteExplain)
			assert.False(t, *cfg.DisableExecuteExplain)
			require.NotNil(t, cfg.DisableGenerateEmbedding)
			assert.False(t, *cfg.DisableGenerateEmbedding)
			require.NotNil(t, cfg.DisableSearchKnowledgebase)
			assert.False(t, *cfg.DisableSearchKnowledgebase)
			require.NotNil(t, cfg.DisableCountRows)
			assert.False(t, *cfg.DisableCountRows)
		})

		t.Run("disable fields absent when not specified", func(t *testing.T) {
			cfg, errs := database.ParseMCPServiceConfig(anthropicBase(), false)
			require.Empty(t, errs)
			assert.Nil(t, cfg.DisableQueryDatabase)
			assert.Nil(t, cfg.DisableGetSchemaInfo)
			assert.Nil(t, cfg.DisableSimilaritySearch)
			assert.Nil(t, cfg.DisableExecuteExplain)
			assert.Nil(t, cfg.DisableGenerateEmbedding)
			assert.Nil(t, cfg.DisableSearchKnowledgebase)
			assert.Nil(t, cfg.DisableCountRows)
		})
	})

	t.Run("json.Number types", func(t *testing.T) {
		t.Run("llm_temperature as json.Number", func(t *testing.T) {
			config := anthropicBase()
			config["llm_temperature"] = json.Number("1.5")
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.LLMTemperature)
			assert.InDelta(t, 1.5, *cfg.LLMTemperature, 1e-9)
		})

		t.Run("llm_max_tokens as json.Number", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = json.Number("4096")
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.LLMMaxTokens)
			assert.Equal(t, 4096, *cfg.LLMMaxTokens)
		})

		t.Run("pool_max_conns as json.Number", func(t *testing.T) {
			config := anthropicBase()
			config["pool_max_conns"] = json.Number("20")
			cfg, errs := database.ParseMCPServiceConfig(config, false)
			require.Empty(t, errs)
			require.NotNil(t, cfg.PoolMaxConns)
			assert.Equal(t, 20, *cfg.PoolMaxConns)
		})

		t.Run("llm_max_tokens as non-integer json.Number", func(t *testing.T) {
			config := anthropicBase()
			config["llm_max_tokens"] = json.Number("10.5")
			_, errs := database.ParseMCPServiceConfig(config, false)
			require.NotEmpty(t, errs)
			assert.Contains(t, joinedErr(errs).Error(), "llm_max_tokens must be an integer")
		})
	})
}

// Ensure the unused ptr helpers don't cause compilation errors.
var _ = strPtr
var _ = boolPtr
var _ = intPtr
var _ = float64Ptr
