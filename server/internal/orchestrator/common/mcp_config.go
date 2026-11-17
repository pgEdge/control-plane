package common

import (
	"fmt"
	"reflect"

	"github.com/goccy/go-yaml"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// mcpYAMLConfig mirrors the MCP server's Config struct for YAML generation.
// Only fields the CP needs to set are included.
type mcpYAMLConfig struct {
	HTTP              mcpHTTPConfig       `yaml:"http"`
	Databases         []mcpDatabaseConfig `yaml:"databases"`
	LLM               *mcpLLMConfig       `yaml:"llm,omitempty"`
	Embedding         *mcpEmbeddingConfig `yaml:"embedding,omitempty"`
	Knowledgebase     *mcpKBConfig        `yaml:"knowledgebase,omitempty"`
	Builtins          mcpBuiltinsConfig   `yaml:"builtins"`
	TraceFile         string              `yaml:"trace_file,omitempty"`
	TraceMetadataOnly *bool               `yaml:"trace_metadata_only,omitempty"`
	DataDir           string              `yaml:"data_dir,omitempty"`
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
	GeminiAPIKey    string  `yaml:"gemini_api_key,omitempty"`
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
	GeminiAPIKey string `yaml:"gemini_api_key,omitempty"`
	OllamaURL    string `yaml:"ollama_url,omitempty"`
}

type mcpKBConfig struct {
	Enabled               bool   `yaml:"enabled"`
	DatabasePath          string `yaml:"database_path"`
	EmbeddingProvider     string `yaml:"embedding_provider"`
	EmbeddingModel        string `yaml:"embedding_model"`
	EmbeddingVoyageAPIKey string `yaml:"embedding_voyage_api_key,omitempty"`
	EmbeddingOpenAIAPIKey string `yaml:"embedding_openai_api_key,omitempty"`
	EmbeddingGeminiAPIKey string `yaml:"embedding_gemini_api_key,omitempty"`
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

// MCPConfigPaths are the orchestrator-specific locations that the MCP server
// listens on and reads and writes its files at, as seen by the server.
type MCPConfigPaths struct {
	// HTTPAddress is the host:port that the server listens on.
	HTTPAddress string `json:"http_address"`
	// TokenFile and UserFile are the application-owned auth stores.
	TokenFile string `json:"token_file"`
	UserFile  string `json:"user_file"`
	// TraceFile is where the audit trace is written when it's enabled.
	TraceFile string `json:"trace_file"`
	// DataDir is the server's writable data directory. It's omitted from the
	// config when empty, so that the server uses its default.
	DataDir string `json:"data_dir,omitempty"`
	// KBDatabasePath is the knowledgebase SQLite file when the
	// knowledgebase is enabled.
	KBDatabasePath string `json:"kb_database_path,omitempty"`
}

// MCPConfigParams holds all inputs needed to generate a config.yaml for the MCP server.
type MCPConfigParams struct {
	Config             *database.MCPServiceConfig
	DatabaseName       string
	DatabaseHosts      []database.ServiceHostEntry
	TargetSessionAttrs string
	Username           string
	Password           string
	Paths              MCPConfigPaths
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
		case "gemini":
			if cfg.GeminiAPIKey != nil {
				l.GeminiAPIKey = *cfg.GeminiAPIKey
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
			case "gemini":
				emb.GeminiAPIKey = *cfg.EmbeddingAPIKey
			}
		}
		if *cfg.EmbeddingProvider == "ollama" && cfg.OllamaURL != nil {
			emb.OllamaURL = *cfg.OllamaURL
		}
		embedding = emb
	}

	// Build KB config (only when kb_enabled is true)
	var kb *mcpKBConfig
	if cfg.KBEnabled != nil && *cfg.KBEnabled {
		if cfg.KBEmbeddingProvider == nil || cfg.KBEmbeddingModel == nil || cfg.KBEmbeddingAPIKey == nil {
			return nil, fmt.Errorf("internal: KB provider/model/key nil despite kb_enabled=true; validation was bypassed")
		}
		if params.Paths.KBDatabasePath == "" {
			return nil, fmt.Errorf("internal: KB database path is empty despite kb_enabled=true")
		}
		k := &mcpKBConfig{
			Enabled:           true,
			DatabasePath:      params.Paths.KBDatabasePath,
			EmbeddingProvider: *cfg.KBEmbeddingProvider,
			EmbeddingModel:    *cfg.KBEmbeddingModel,
		}
		switch *cfg.KBEmbeddingProvider {
		case "voyage":
			k.EmbeddingVoyageAPIKey = *cfg.KBEmbeddingAPIKey
		case "openai":
			k.EmbeddingOpenAIAPIKey = *cfg.KBEmbeddingAPIKey
		case "gemini":
			k.EmbeddingGeminiAPIKey = *cfg.KBEmbeddingAPIKey
		}
		kb = k
	}

	// Build tool toggles
	tools := mcpToolsConfig{
		LLMConnectionSelection: utils.PointerTo(false), // Always disabled
	}
	if cfg.DisableQueryDatabase != nil && *cfg.DisableQueryDatabase {
		tools.QueryDatabase = utils.PointerTo(false)
	}
	if cfg.DisableGetSchemaInfo != nil && *cfg.DisableGetSchemaInfo {
		tools.GetSchemaInfo = utils.PointerTo(false)
	}
	if cfg.DisableSimilaritySearch != nil && *cfg.DisableSimilaritySearch {
		tools.SimilaritySearch = utils.PointerTo(false)
	}
	if cfg.DisableExecuteExplain != nil && *cfg.DisableExecuteExplain {
		tools.ExecuteExplain = utils.PointerTo(false)
	}
	if cfg.DisableGenerateEmbedding != nil && *cfg.DisableGenerateEmbedding {
		tools.GenerateEmbedding = utils.PointerTo(false)
	}
	if cfg.DisableSearchKnowledgebase != nil && *cfg.DisableSearchKnowledgebase {
		tools.SearchKnowledgebase = utils.PointerTo(false)
	}
	if cfg.DisableCountRows != nil && *cfg.DisableCountRows {
		tools.CountRows = utils.PointerTo(false)
	}

	// Map database hosts to MCP config format
	hosts := make([]mcpHostEntry, len(params.DatabaseHosts))
	for i, h := range params.DatabaseHosts {
		hosts[i] = mcpHostEntry{Host: h.Host, Port: h.Port}
	}

	// Metadata-only defaults to true when trace is enabled but the caller
	// didn't say otherwise, so query text and result rows don't land on disk
	// by default.
	var traceFile string
	var traceMetadataOnly *bool
	if cfg.AuditTraceEnabled != nil && *cfg.AuditTraceEnabled {
		traceFile = params.Paths.TraceFile
		metadataOnly := true
		if cfg.AuditTraceMetadataOnly != nil {
			metadataOnly = *cfg.AuditTraceMetadataOnly
		}
		traceMetadataOnly = &metadataOnly
	}

	yamlCfg := &mcpYAMLConfig{
		HTTP: mcpHTTPConfig{
			Enabled: true,
			Address: params.Paths.HTTPAddress,
			Auth: mcpAuthConfig{
				Enabled:   true,
				TokenFile: params.Paths.TokenFile,
				UserFile:  params.Paths.UserFile,
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
				Pool: mcpPoolConfig{
					MaxConns: poolMaxConns,
				},
			},
		},
		LLM:           llm,
		Embedding:     embedding,
		Knowledgebase: kb,
		Builtins: mcpBuiltinsConfig{
			Tools: tools,
		},
		TraceFile:         traceFile,
		TraceMetadataOnly: traceMetadataOnly,
		DataDir:           params.Paths.DataDir,
	}

	data, err := yaml.Marshal(yamlCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP config to YAML: %w", err)
	}
	return data, nil
}

// mcpReloadModes returns how a running MCP server must be reloaded to apply
// the change from the previous to the current config.yaml. The server only
// applies its database connections and trace_metadata_only on SIGHUP, so any
// other change needs a restart. It returns nil if there's no previous config,
// since the server reads the current file when it starts, or if nothing
// changed.
func mcpReloadModes(previous, current []byte) ([]database.ReloadMode, error) {
	if previous == nil || string(previous) == string(current) {
		return nil, nil
	}
	prev, err := mcpRestartRequiredSettings(previous)
	if err != nil {
		return nil, err
	}
	curr, err := mcpRestartRequiredSettings(current)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(prev, curr) {
		return []database.ReloadMode{database.ReloadModeRestart}, nil
	}
	return []database.ReloadMode{database.ReloadModeSighup}, nil
}

// mcpRestartRequiredSettings returns the settings from an MCP config.yaml
// that the server doesn't apply on SIGHUP.
func mcpRestartRequiredSettings(content []byte) (map[string]any, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}
	delete(cfg, "databases")
	delete(cfg, "trace_metadata_only")
	return cfg, nil
}
