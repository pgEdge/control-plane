package database_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalRAGConfig returns a minimal valid RAG config map.
func minimalRAGConfig() map[string]any {
	return map[string]any{
		"pipelines": []any{
			map[string]any{
				"name": "my-pipeline",
				"tables": []any{
					map[string]any{
						"table":         "documents_content_chunks",
						"text_column":   "content",
						"vector_column": "embedding",
					},
				},
				"embedding_llm": map[string]any{
					"provider": "openai",
					"model":    "text-embedding-3-small",
					"api_key":  "sk-openai-key",
				},
				"rag_llm": map[string]any{
					"provider": "anthropic",
					"model":    "claude-sonnet-4-20250514",
					"api_key":  "sk-ant-key",
				},
			},
		},
	}
}

func TestParseRAGServiceConfig_MinimalValid(t *testing.T) {
	cfg, errs := database.ParseRAGServiceConfig(minimalRAGConfig(), false)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Pipelines, 1)
	assert.Equal(t, "my-pipeline", cfg.Pipelines[0].Name)
	assert.Equal(t, "openai", cfg.Pipelines[0].EmbeddingLLM.Provider)
	assert.Equal(t, "anthropic", cfg.Pipelines[0].RAGLLM.Provider)
}

func TestParseRAGServiceConfig_FullValid(t *testing.T) {
	tokenBudget := 4000
	topN := 15
	vectorWeight := 0.7
	idCol := "id"

	config := map[string]any{
		"defaults": map[string]any{
			"token_budget": float64(2000),
			"top_n":        float64(10),
		},
		"pipelines": []any{
			map[string]any{
				"name":        "pipeline-a",
				"description": "First pipeline",
				"tables": []any{
					map[string]any{
						"table":         "chunks",
						"text_column":   "text",
						"vector_column": "vec",
						"id_column":     idCol,
					},
				},
				"embedding_llm": map[string]any{
					"provider": "voyage",
					"model":    "voyage-3",
					"api_key":  "voy-key",
				},
				"rag_llm": map[string]any{
					"provider": "openai",
					"model":    "gpt-4o",
					"api_key":  "sk-openai",
				},
				"token_budget":  float64(tokenBudget),
				"top_n":         float64(topN),
				"system_prompt": "You are a helpful assistant.",
				"search": map[string]any{
					"hybrid_enabled": true,
					"vector_weight":  vectorWeight,
				},
			},
		},
	}

	cfg, errs := database.ParseRAGServiceConfig(config, false)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Defaults)
	assert.Equal(t, 2000, *cfg.Defaults.TokenBudget)
	assert.Equal(t, 10, *cfg.Defaults.TopN)
	p := cfg.Pipelines[0]
	assert.Equal(t, "pipeline-a", p.Name)
	assert.NotNil(t, p.Description)
	assert.Equal(t, tokenBudget, *p.TokenBudget)
	assert.Equal(t, topN, *p.TopN)
	assert.NotNil(t, p.Search)
	assert.Equal(t, vectorWeight, *p.Search.VectorWeight)
	assert.Equal(t, &idCol, p.Tables[0].IDColumn)
}

func TestParseRAGServiceConfig_OllamaEmbedding(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["embedding_llm"] = map[string]any{
		"provider": "ollama",
		"model":    "nomic-embed-text",
		"base_url": "http://localhost:11434",
	}

	cfg, errs := database.ParseRAGServiceConfig(config, false)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
	assert.Equal(t, "ollama", cfg.Pipelines[0].EmbeddingLLM.Provider)
	assert.Nil(t, cfg.Pipelines[0].EmbeddingLLM.APIKey)
}

func TestParseRAGServiceConfig_IsUpdateIgnored(t *testing.T) {
	// RAG has no bootstrap-only fields; isUpdate=true should behave identically.
	cfg, errs := database.ParseRAGServiceConfig(minimalRAGConfig(), true)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
}

func TestParseRAGServiceConfig_MissingPipelines(t *testing.T) {
	config := map[string]any{}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "pipelines")
}

func TestParseRAGServiceConfig_EmptyPipelines(t *testing.T) {
	config := map[string]any{
		"pipelines": []any{},
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "pipelines")
}

func TestParseRAGServiceConfig_UnknownTopLevelKey(t *testing.T) {
	config := minimalRAGConfig()
	config["unknown_key"] = "value"
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "unknown_key")
}

func TestParseRAGServiceConfig_UnknownNestedKey(t *testing.T) {
	config := minimalRAGConfig()
	pipeline := config["pipelines"].([]any)[0].(map[string]any)
	pipeline["unknown_nested"] = "value"
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
}

func TestParseRAGServiceConfig_MissingPipelineName(t *testing.T) {
	config := minimalRAGConfig()
	delete(config["pipelines"].([]any)[0].(map[string]any), "name")
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "pipelines[0].name")
}

func TestParseRAGServiceConfig_DuplicatePipelineNames(t *testing.T) {
	config := minimalRAGConfig()
	second := map[string]any{
		"name": "my-pipeline", // duplicate
		"tables": []any{
			map[string]any{
				"table":         "other_chunks",
				"text_column":   "text",
				"vector_column": "vec",
			},
		},
		"embedding_llm": map[string]any{
			"provider": "openai",
			"model":    "text-embedding-3-small",
			"api_key":  "sk-key",
		},
		"rag_llm": map[string]any{
			"provider": "anthropic",
			"model":    "claude-sonnet-4-20250514",
			"api_key":  "sk-ant-key",
		},
	}
	config["pipelines"] = append(config["pipelines"].([]any), second)
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "duplicate name")
}

func TestParseRAGServiceConfig_MissingTables(t *testing.T) {
	config := minimalRAGConfig()
	delete(config["pipelines"].([]any)[0].(map[string]any), "tables")
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "tables")
}

func TestParseRAGServiceConfig_EmptyTables(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["tables"] = []any{}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "tables")
}

func TestParseRAGServiceConfig_MissingTableFields(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["tables"] = []any{
		map[string]any{
			// missing table, text_column, vector_column
		},
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "table is required")
}

func TestParseRAGServiceConfig_InvalidEmbeddingProvider(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["embedding_llm"] = map[string]any{
		"provider": "invalid-provider",
		"model":    "some-model",
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "embedding_llm.provider")
}

func TestParseRAGServiceConfig_InvalidRAGLLMProvider(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["rag_llm"] = map[string]any{
		"provider": "voyage", // valid for embedding but not for rag_llm
		"model":    "voyage-3",
		"api_key":  "key",
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "rag_llm.provider")
}

func TestParseRAGServiceConfig_MissingAPIKey_Anthropic(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["rag_llm"] = map[string]any{
		"provider": "anthropic",
		"model":    "claude-sonnet-4-20250514",
		// missing api_key
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "api_key")
}

func TestParseRAGServiceConfig_MissingAPIKey_Voyage(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["embedding_llm"] = map[string]any{
		"provider": "voyage",
		"model":    "voyage-3",
		// missing api_key
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "api_key")
}

func TestParseRAGServiceConfig_InvalidVectorWeight(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["search"] = map[string]any{
		"vector_weight": 1.5, // out of range
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "vector_weight")
}

func TestParseRAGServiceConfig_InvalidTokenBudget(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["token_budget"] = float64(0)
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "token_budget")
}

func TestParseRAGServiceConfig_InvalidDefaultsTokenBudget(t *testing.T) {
	config := minimalRAGConfig()
	config["defaults"] = map[string]any{
		"token_budget": float64(-1),
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "defaults.token_budget")
}

func TestParseRAGServiceConfig_MissingEmbeddingLLMModel(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["embedding_llm"] = map[string]any{
		"provider": "openai",
		"api_key":  "sk-key",
		// missing model
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "embedding_llm.model")
}

func TestParseRAGServiceConfig_MissingAPIKey_OpenAI(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["embedding_llm"] = map[string]any{
		"provider": "openai",
		"model":    "text-embedding-3-small",
		// missing api_key
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "api_key")
}

func TestParseRAGServiceConfig_OllamaRAGLLM(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["rag_llm"] = map[string]any{
		"provider": "ollama",
		"model":    "llama3.2",
		// no api_key required for ollama
	}
	cfg, errs := database.ParseRAGServiceConfig(config, false)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
	assert.Equal(t, "ollama", cfg.Pipelines[0].RAGLLM.Provider)
	assert.Nil(t, cfg.Pipelines[0].RAGLLM.APIKey)
}

func TestParseRAGServiceConfig_NegativeTopN(t *testing.T) {
	config := minimalRAGConfig()
	config["pipelines"].([]any)[0].(map[string]any)["top_n"] = float64(-5)
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "top_n")
}

func TestParseRAGServiceConfig_InvalidDefaultsTopN(t *testing.T) {
	config := minimalRAGConfig()
	config["defaults"] = map[string]any{
		"top_n": float64(0),
	}
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "defaults.top_n")
}

func TestParseRAGServiceConfig_VectorWeightBoundaries(t *testing.T) {
	for _, vw := range []float64{0.0, 1.0} {
		config := minimalRAGConfig()
		config["pipelines"].([]any)[0].(map[string]any)["search"] = map[string]any{
			"vector_weight": vw,
		}
		cfg, errs := database.ParseRAGServiceConfig(config, false)
		require.Empty(t, errs, "vector_weight %.1f should be valid", vw)
		require.NotNil(t, cfg)
		assert.Equal(t, vw, *cfg.Pipelines[0].Search.VectorWeight)
	}
}

func TestParseRAGServiceConfig_MissingEmbeddingLLM(t *testing.T) {
	config := minimalRAGConfig()
	delete(config["pipelines"].([]any)[0].(map[string]any), "embedding_llm")
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "embedding_llm.provider")
}

func TestParseRAGServiceConfig_MissingRAGLLM(t *testing.T) {
	config := minimalRAGConfig()
	delete(config["pipelines"].([]any)[0].(map[string]any), "rag_llm")
	_, errs := database.ParseRAGServiceConfig(config, false)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "rag_llm.provider")
}

func TestParseRAGServiceConfig_MultiplePipelines(t *testing.T) {
	config := map[string]any{
		"pipelines": []any{
			map[string]any{
				"name": "pipeline-1",
				"tables": []any{
					map[string]any{"table": "t1", "text_column": "tc", "vector_column": "vc"},
				},
				"embedding_llm": map[string]any{"provider": "openai", "model": "text-embedding-3-small", "api_key": "k1"},
				"rag_llm":       map[string]any{"provider": "anthropic", "model": "claude-3-haiku-20240307", "api_key": "k2"},
			},
			map[string]any{
				"name": "pipeline-2",
				"tables": []any{
					map[string]any{"table": "t2", "text_column": "tc", "vector_column": "vc"},
				},
				"embedding_llm": map[string]any{"provider": "ollama", "model": "nomic-embed-text"},
				"rag_llm":       map[string]any{"provider": "ollama", "model": "llama3.2"},
			},
		},
	}
	cfg, errs := database.ParseRAGServiceConfig(config, false)
	require.Empty(t, errs)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Pipelines, 2)
}
