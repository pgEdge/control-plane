package swarm

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// swarmMCPConfigPaths returns the paths that the swarm MCP container sees,
// for rendering the given config with the common renderer.
func swarmMCPConfigPaths(cfg *database.MCPServiceConfig) common.MCPConfigPaths {
	paths := common.MCPConfigPaths{
		HTTPAddress: ":8080",
		TokenFile:   "/app/data/tokens.yaml",
		UserFile:    "/app/data/users.yaml",
		TraceFile:   "/app/data/audit-trace.log",
	}
	if cfg.KBEnabled != nil && *cfg.KBEnabled {
		paths.KBDatabasePath = "/app/kb/nla-kb.db"
		if cfg.KBDatabaseHostPath != nil {
			paths.KBDatabasePath = "/app/kb/" + filepath.Base(*cfg.KBDatabaseHostPath)
		}
	}
	return paths
}

// TestMCPConfigEquivalence pins the swarm MCP config.yaml output and proves
// that the common renderer produces byte-identical output for swarm, so that
// moving swarm onto the common resources doesn't change its config files.
func TestMCPConfigEquivalence(t *testing.T) {
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

	kbConfig := func(provider string, hostPath *string) *database.MCPServiceConfig {
		return &database.MCPServiceConfig{
			KBEnabled:           utils.PointerTo(true),
			KBEmbeddingProvider: utils.PointerTo(provider),
			KBEmbeddingModel:    utils.PointerTo("kb-model"),
			KBEmbeddingAPIKey:   utils.PointerTo("kb-key"),
			KBDatabaseHostPath:  hostPath,
		}
	}

	for _, tc := range []struct {
		name   string
		config *database.MCPServiceConfig
		hosts  []database.ServiceHostEntry
		tsa    string
	}{
		{
			name:   "minimal",
			config: &database.MCPServiceConfig{},
		},
		{
			name: "llm anthropic with tuning",
			config: &database.MCPServiceConfig{
				LLMEnabled:      utils.PointerTo(true),
				LLMProvider:     "anthropic",
				LLMModel:        "claude-sonnet-4-5",
				AnthropicAPIKey: utils.PointerTo("sk-ant"),
				LLMTemperature:  utils.PointerTo(0.2),
				LLMMaxTokens:    utils.PointerTo(1024),
			},
		},
		{
			name: "llm openai",
			config: &database.MCPServiceConfig{
				LLMEnabled:   utils.PointerTo(true),
				LLMProvider:  "openai",
				LLMModel:     "gpt-4o",
				OpenAIAPIKey: utils.PointerTo("sk-openai"),
			},
		},
		{
			name: "llm gemini",
			config: &database.MCPServiceConfig{
				LLMEnabled:   utils.PointerTo(true),
				LLMProvider:  "gemini",
				LLMModel:     "gemini-2.5-pro",
				GeminiAPIKey: utils.PointerTo("gemini-key"),
			},
		},
		{
			name: "llm and embedding ollama",
			config: &database.MCPServiceConfig{
				LLMEnabled:        utils.PointerTo(true),
				LLMProvider:       "ollama",
				LLMModel:          "llama3",
				OllamaURL:         utils.PointerTo("http://ollama:11434"),
				EmbeddingProvider: utils.PointerTo("ollama"),
				EmbeddingModel:    utils.PointerTo("nomic-embed-text"),
			},
		},
		{
			name: "embedding voyage",
			config: &database.MCPServiceConfig{
				EmbeddingProvider: utils.PointerTo("voyage"),
				EmbeddingModel:    utils.PointerTo("voyage-3"),
				EmbeddingAPIKey:   utils.PointerTo("voyage-key"),
			},
		},
		{
			name: "embedding openai",
			config: &database.MCPServiceConfig{
				EmbeddingProvider: utils.PointerTo("openai"),
				EmbeddingModel:    utils.PointerTo("text-embedding-3-small"),
				EmbeddingAPIKey:   utils.PointerTo("openai-key"),
			},
		},
		{
			name: "embedding gemini",
			config: &database.MCPServiceConfig{
				EmbeddingProvider: utils.PointerTo("gemini"),
				EmbeddingModel:    utils.PointerTo("text-embedding-004"),
				EmbeddingAPIKey:   utils.PointerTo("gemini-key"),
			},
		},
		{
			name:   "kb default path voyage",
			config: kbConfig("voyage", nil),
		},
		{
			name:   "kb host path openai",
			config: kbConfig("openai", utils.PointerTo("/srv/kb/custom-kb.db")),
		},
		{
			name:   "kb gemini",
			config: kbConfig("gemini", nil),
		},
		{
			name: "tools disabled",
			config: &database.MCPServiceConfig{
				DisableQueryDatabase:       utils.PointerTo(true),
				DisableGetSchemaInfo:       utils.PointerTo(true),
				DisableSimilaritySearch:    utils.PointerTo(true),
				DisableExecuteExplain:      utils.PointerTo(true),
				DisableGenerateEmbedding:   utils.PointerTo(true),
				DisableSearchKnowledgebase: utils.PointerTo(true),
				DisableCountRows:           utils.PointerTo(true),
			},
		},
		{
			name: "tool toggles set to false are omitted",
			config: &database.MCPServiceConfig{
				DisableQueryDatabase: utils.PointerTo(false),
			},
		},
		{
			name: "audit trace defaults to metadata only",
			config: &database.MCPServiceConfig{
				AuditTraceEnabled: utils.PointerTo(true),
			},
		},
		{
			name: "audit trace with full content",
			config: &database.MCPServiceConfig{
				AuditTraceEnabled:      utils.PointerTo(true),
				AuditTraceMetadataOnly: utils.PointerTo(false),
			},
		},
		{
			name: "pool and writes",
			config: &database.MCPServiceConfig{
				AllowWrites:  utils.PointerTo(true),
				PoolMaxConns: utils.PointerTo(16),
			},
		},
		{
			name:   "multi host with target session attrs",
			config: &database.MCPServiceConfig{},
			hosts: []database.ServiceHostEntry{
				{Host: "postgres-inst-1", Port: 5432},
				{Host: "postgres-inst-2", Port: 5432},
				{Host: "postgres-inst-3", Port: 5432},
			},
			tsa: "primary",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hosts := tc.hosts
			if hosts == nil {
				hosts = []database.ServiceHostEntry{{Host: "postgres-inst-1", Port: 5432}}
			}

			swarmOutput, err := GenerateMCPConfig(&MCPConfigParams{
				Config:             tc.config,
				DatabaseName:       "mydb",
				DatabaseHosts:      hosts,
				TargetSessionAttrs: tc.tsa,
				Username:           "app",
				Password:           "secret",
			})
			require.NoError(t, err)

			commonOutput, err := common.GenerateMCPConfig(&common.MCPConfigParams{
				Config:             tc.config,
				DatabaseName:       "mydb",
				DatabaseHosts:      hosts,
				TargetSessionAttrs: tc.tsa,
				Username:           "app",
				Password:           "secret",
				Paths:              swarmMCPConfigPaths(tc.config),
			})
			require.NoError(t, err)

			assert.Equal(t, string(swarmOutput), string(commonOutput), "the common renderer must match swarm")
			golden.Run(t, string(swarmOutput), update)
		})
	}
}
