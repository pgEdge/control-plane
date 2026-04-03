package swarm

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/pgEdge/control-plane/server/internal/database"
)

// mcpYAMLConfig mirrors the MCP server's Config struct for YAML generation.
// Only fields the CP needs to set are included.
type mcpYAMLConfig struct {
	HTTP      mcpHTTPConfig       `yaml:"http"`
	Databases []mcpDatabaseConfig `yaml:"databases"`
	LLM       *mcpLLMConfig       `yaml:"llm,omitempty"`
	Embedding *mcpEmbeddingConfig `yaml:"embedding,omitempty"`
	Builtins  mcpBuiltinsConfig   `yaml:"builtins"`
}

type mcpHTTPConfig struct {
	Enabled bool          `yaml:"enabled"`
	Address string        `yaml:"address"`
	Auth    mcpAuthConfig `yaml:"auth"`
}

type mcpAuthConfig struct {
	Enabled   bool   `yaml:"enabled"`
	TokenFile string `yaml:"token_file"`
	UserFile  string `yaml:"user_file"`
}

type mcpHostEntry struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type mcpDatabaseConfig struct {
	Name               string         `yaml:"name"`
	Hosts              []mcpHostEntry `yaml:"hosts"`
	TargetSessionAttrs string         `yaml:"target_session_attrs,omitempty"`
	Database           string         `yaml:"database"`
	User               string         `yaml:"user"`
	Password           string         `yaml:"password"`
	SSLMode            string         `yaml:"sslmode"`
	AllowWrites        bool           `yaml:"allow_writes"`
	MetadataTTL        string         `yaml:"metadata_ttl,omitempty"`
	Pool               mcpPoolConfig  `yaml:"pool"`
}

type mcpPoolConfig struct {
	MaxConns int `yaml:"max_conns"`
}

type mcpLLMConfig struct {
	Enabled         bool    `yaml:"enabled"`
	Provider        string  `yaml:"provider"`
	Model           string  `yaml:"model"`
	AnthropicAPIKey string  `yaml:"anthropic_api_key,omitempty"`
	OpenAIAPIKey    string  `yaml:"openai_api_key,omitempty"`
	OllamaURL       string  `yaml:"ollama_url,omitempty"`
	Temperature     float64 `yaml:"temperature"`
	MaxTokens       int     `yaml:"max_tokens"`
}

type mcpEmbeddingConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
	VoyageAPIKey string `yaml:"voyage_api_key,omitempty"`
	OpenAIAPIKey string `yaml:"openai_api_key,omitempty"`
	OllamaURL    string `yaml:"ollama_url,omitempty"`
}

type mcpBuiltinsConfig struct {
	Tools mcpToolsConfig `yaml:"tools"`
}

type mcpToolsConfig struct {
	QueryDatabase       *bool `yaml:"query_database,omitempty"`
	GetSchemaInfo       *bool `yaml:"get_schema_info,omitempty"`
	SimilaritySearch    *bool `yaml:"similarity_search,omitempty"`
	ExecuteExplain      *bool `yaml:"execute_explain,omitempty"`
	GenerateEmbedding   *bool `yaml:"generate_embedding,omitempty"`
	SearchKnowledgebase *bool `yaml:"search_knowledgebase,omitempty"`
	CountRows           *bool `yaml:"count_rows,omitempty"`
	// Always disabled — CP owns the DB connection
	LLMConnectionSelection *bool `yaml:"llm_connection_selection"`
}

// MCPConfigParams holds all inputs needed to generate a config.yaml for the MCP server.
type MCPConfigParams struct {
	Config             *database.MCPServiceConfig
	DatabaseName       string
	DatabaseHosts      []database.ServiceHostEntry
	TargetSessionAttrs string
	Username           string
	Password           string
}

// GenerateMCPConfig generates the YAML config file content for the MCP server.
func GenerateMCPConfig(params *MCPConfigParams) ([]byte, error) {
	cfg := params.Config

	// Apply defaults for overridable fields
	poolMaxConns := 4
	if cfg.PoolMaxConns != nil {
		poolMaxConns = *cfg.PoolMaxConns
	}
	allowWrites := false
	if cfg.AllowWrites != nil {
		allowWrites = *cfg.AllowWrites
	}
	var metadataTTL string
	if cfg.MetadataTTL != nil {
		metadataTTL = *cfg.MetadataTTL
	}

	// Build LLM config (only when llm_enabled is true)
	var llm *mcpLLMConfig
	if cfg.LLMEnabled != nil && *cfg.LLMEnabled {
		temperature := 0.7
		if cfg.LLMTemperature != nil {
			temperature = *cfg.LLMTemperature
		}
		maxTokens := 4096
		if cfg.LLMMaxTokens != nil {
			maxTokens = *cfg.LLMMaxTokens
		}
		l := &mcpLLMConfig{
			Enabled:     true,
			Provider:    cfg.LLMProvider,
			Model:       cfg.LLMModel,
			Temperature: temperature,
			MaxTokens:   maxTokens,
		}
		switch cfg.LLMProvider {
		case "anthropic":
			if cfg.AnthropicAPIKey != nil {
				l.AnthropicAPIKey = *cfg.AnthropicAPIKey
			}
		case "openai":
			if cfg.OpenAIAPIKey != nil {
				l.OpenAIAPIKey = *cfg.OpenAIAPIKey
			}
		case "ollama":
			if cfg.OllamaURL != nil {
				l.OllamaURL = *cfg.OllamaURL
			}
		}
		llm = l
	}

	// Build embedding config (only if provider is set)
	var embedding *mcpEmbeddingConfig
	if cfg.EmbeddingProvider != nil {
		emb := &mcpEmbeddingConfig{
			Enabled:  true,
			Provider: *cfg.EmbeddingProvider,
		}
		if cfg.EmbeddingModel != nil {
			emb.Model = *cfg.EmbeddingModel
		}
		if cfg.EmbeddingAPIKey != nil {
			switch *cfg.EmbeddingProvider {
			case "voyage":
				emb.VoyageAPIKey = *cfg.EmbeddingAPIKey
			case "openai":
				emb.OpenAIAPIKey = *cfg.EmbeddingAPIKey
			}
		}
		if *cfg.EmbeddingProvider == "ollama" && cfg.OllamaURL != nil {
			emb.OllamaURL = *cfg.OllamaURL
		}
		embedding = emb
	}

	// Build tool toggles
	falseVal := false
	tools := mcpToolsConfig{
		LLMConnectionSelection: &falseVal, // Always disabled
	}
	if cfg.DisableQueryDatabase != nil && *cfg.DisableQueryDatabase {
		tools.QueryDatabase = boolPtr(false)
	}
	if cfg.DisableGetSchemaInfo != nil && *cfg.DisableGetSchemaInfo {
		tools.GetSchemaInfo = boolPtr(false)
	}
	if cfg.DisableSimilaritySearch != nil && *cfg.DisableSimilaritySearch {
		tools.SimilaritySearch = boolPtr(false)
	}
	if cfg.DisableExecuteExplain != nil && *cfg.DisableExecuteExplain {
		tools.ExecuteExplain = boolPtr(false)
	}
	if cfg.DisableGenerateEmbedding != nil && *cfg.DisableGenerateEmbedding {
		tools.GenerateEmbedding = boolPtr(false)
	}
	if cfg.DisableSearchKnowledgebase != nil && *cfg.DisableSearchKnowledgebase {
		tools.SearchKnowledgebase = boolPtr(false)
	}
	if cfg.DisableCountRows != nil && *cfg.DisableCountRows {
		tools.CountRows = boolPtr(false)
	}

	// Map database hosts to MCP config format
	hosts := make([]mcpHostEntry, len(params.DatabaseHosts))
	for i, h := range params.DatabaseHosts {
		hosts[i] = mcpHostEntry{Host: h.Host, Port: h.Port}
	}

	yamlCfg := &mcpYAMLConfig{
		HTTP: mcpHTTPConfig{
			Enabled: true,
			Address: ":8080",
			Auth: mcpAuthConfig{
				Enabled:   true,
				TokenFile: "/app/data/tokens.yaml",
				UserFile:  "/app/data/users.yaml",
			},
		},
		Databases: []mcpDatabaseConfig{
			{
				Name:               params.DatabaseName,
				Hosts:              hosts,
				TargetSessionAttrs: params.TargetSessionAttrs,
				Database:           params.DatabaseName,
				User:               params.Username,
				Password:           params.Password,
				SSLMode:            "prefer",
				AllowWrites:        allowWrites,
				MetadataTTL:        metadataTTL,
				Pool: mcpPoolConfig{
					MaxConns: poolMaxConns,
				},
			},
		},
		LLM:       llm,
		Embedding: embedding,
		Builtins: mcpBuiltinsConfig{
			Tools: tools,
		},
	}

	data, err := yaml.Marshal(yamlCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP config to YAML: %w", err)
	}
	return data, nil
}

func boolPtr(b bool) *bool {
	return &b
}
