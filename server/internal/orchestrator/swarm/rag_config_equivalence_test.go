package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// ragEquivalenceConfigs covers every field and API-key combination that the
// RAG renderer handles.
func ragEquivalenceConfigs() map[string]*database.RAGServiceConfig {
	llm := func(provider, model string, key *string) database.RAGPipelineLLMConfig {
		return database.RAGPipelineLLMConfig{Provider: provider, Model: model, APIKey: key}
	}
	tables := []database.RAGPipelineTable{{Table: "docs", TextColumn: "content", VectorColumn: "embedding"}}

	return map[string]*database.RAGServiceConfig{
		"minimal": minimalRAGParams().Config,
		"optional pipeline fields": {
			Pipelines: []database.RAGPipeline{{
				Name:         "default",
				Description:  utils.PointerTo("My pipeline"),
				Tables:       []database.RAGPipelineTable{{Table: "t", TextColumn: "c", VectorColumn: "v", IDColumn: utils.PointerTo("id")}},
				EmbeddingLLM: llm("openai", "text-embedding-3-small", utils.PointerTo("sk-openai")),
				RAGLLM:       llm("anthropic", "claude-sonnet-4-5", utils.PointerTo("sk-ant")),
				TokenBudget:  utils.PointerTo(500),
				TopN:         utils.PointerTo(5),
				SystemPrompt: utils.PointerTo("You are a helpful assistant."),
				Search: &database.RAGPipelineSearch{
					HybridEnabled: utils.PointerTo(false),
					VectorWeight:  utils.PointerTo(0.7),
				},
				AllowIncludeSources: utils.PointerTo(true),
			}},
			Defaults: &database.RAGDefaults{TokenBudget: utils.PointerTo(1000), TopN: utils.PointerTo(10)},
		},
		"same provider for embedding and rag": {
			Pipelines: []database.RAGPipeline{{
				Name:         "default",
				Tables:       tables,
				EmbeddingLLM: llm("openai", "text-embedding-3-small", utils.PointerTo("sk-openai")),
				RAGLLM:       llm("openai", "gpt-4o", utils.PointerTo("sk-openai")),
			}},
		},
		"gemini with base urls": {
			Pipelines: []database.RAGPipeline{{
				Name:   "default",
				Tables: tables,
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "gemini", Model: "text-embedding-004", APIKey: utils.PointerTo("g-key"),
					BaseURL: utils.PointerTo("https://example.test/embed"),
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "gemini", Model: "gemini-2.0-flash", APIKey: utils.PointerTo("g-key"),
					BaseURL: utils.PointerTo("https://example.test/llm"),
				},
			}},
		},
		"ollama without keys": {
			Pipelines: []database.RAGPipeline{{
				Name:         "local",
				Tables:       tables,
				EmbeddingLLM: llm("ollama", "nomic-embed-text", nil),
				RAGLLM:       llm("ollama", "llama3", nil),
			}},
		},
		"rerank with its own key": {
			Pipelines: []database.RAGPipeline{{
				Name:         "default",
				Tables:       tables,
				EmbeddingLLM: llm("openai", "text-embedding-3-small", utils.PointerTo("sk-openai")),
				RAGLLM:       llm("anthropic", "claude-sonnet-4-5", utils.PointerTo("sk-ant")),
				Rerank:       &database.RAGRerankConfig{Provider: "voyage", Model: "rerank-2", APIKey: utils.PointerTo("v-key"), TopK: utils.PointerTo(3)},
			}},
		},
		"rerank reusing the voyage embedding key": {
			Pipelines: []database.RAGPipeline{{
				Name:         "default",
				Tables:       tables,
				EmbeddingLLM: llm("voyage", "voyage-3", utils.PointerTo("v-key")),
				RAGLLM:       llm("anthropic", "claude-sonnet-4-5", utils.PointerTo("sk-ant")),
				Rerank:       &database.RAGRerankConfig{Provider: "voyage", Model: "rerank-2", APIKey: utils.PointerTo("v-key")},
			}},
		},
		"multiple pipelines": {
			Pipelines: []database.RAGPipeline{
				{
					Name:         "a",
					Tables:       tables,
					EmbeddingLLM: llm("openai", "text-embedding-3-small", utils.PointerTo("sk-a")),
					RAGLLM:       llm("anthropic", "claude-sonnet-4-5", utils.PointerTo("sk-ant-a")),
				},
				{
					Name:         "b",
					Tables:       tables,
					EmbeddingLLM: llm("voyage", "voyage-3", utils.PointerTo("v-b")),
					RAGLLM:       llm("openai", "gpt-4o", utils.PointerTo("sk-b")),
				},
			},
		},
	}
}

// TestRAGConfigEquivalence pins the swarm pgedge-rag-server.yaml output and
// proves that the common renderer and key extraction produce identical output
// for swarm, so that moving swarm onto the common resources doesn't change
// its config or key files.
func TestRAGConfigEquivalence(t *testing.T) {
	golden := &testutils.GoldenTest[string]{
		FileExtension: ".yaml",
		Marshal: func(v any) ([]byte, error) {
			return []byte(v.(string)), nil
		},
		Unmarshal: func(data []byte, v any) error {
			*v.(*string) = string(data)
			return nil
		},
	}

	for name, cfg := range ragEquivalenceConfigs() {
		t.Run(name, func(t *testing.T) {
			swarmOut, err := GenerateRAGConfig(&RAGConfigParams{
				Config:       cfg,
				DatabaseName: "storefront",
				DatabaseHost: "postgres-storefront-n1-aaaa",
				DatabasePort: 5432,
				Username:     "rag_user",
				Password:     "secret",
				KeysDir:      ragKeysContainerDir,
			})
			require.NoError(t, err)

			commonOut, err := common.GenerateRAGConfig(&common.RAGConfigParams{
				Config:        cfg,
				DatabaseName:  "storefront",
				DatabaseHost:  "postgres-storefront-n1-aaaa",
				DatabasePort:  5432,
				Username:      "rag_user",
				Password:      "secret",
				KeysDir:       ragKeysContainerDir,
				ListenAddress: "0.0.0.0",
				Port:          8080,
			})
			require.NoError(t, err)
			assert.Equal(t, string(swarmOut), string(commonOut))

			// The listen address and port default to swarm's.
			defaultsOut, err := common.GenerateRAGConfig(&common.RAGConfigParams{
				Config:       cfg,
				DatabaseName: "storefront",
				DatabaseHost: "postgres-storefront-n1-aaaa",
				DatabasePort: 5432,
				Username:     "rag_user",
				Password:     "secret",
				KeysDir:      ragKeysContainerDir,
			})
			require.NoError(t, err)
			assert.Equal(t, string(swarmOut), string(defaultsOut))

			assert.Equal(t, extractRAGAPIKeys(cfg), common.ExtractRAGAPIKeys(cfg))

			golden.Run(t, string(swarmOut), update)
		})
	}

	t.Run("same error for mismatched keys", func(t *testing.T) {
		cfg := &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{{
			Name:         "default",
			EmbeddingLLM: database.RAGPipelineLLMConfig{Provider: "openai", Model: "e", APIKey: utils.PointerTo("k1")},
			RAGLLM:       database.RAGPipelineLLMConfig{Provider: "openai", Model: "m", APIKey: utils.PointerTo("k2")},
		}}}
		_, swarmErr := GenerateRAGConfig(&RAGConfigParams{Config: cfg, KeysDir: ragKeysContainerDir})
		_, commonErr := common.GenerateRAGConfig(&common.RAGConfigParams{Config: cfg, KeysDir: ragKeysContainerDir})
		require.Error(t, swarmErr)
		assert.EqualError(t, commonErr, swarmErr.Error())
	})
}
