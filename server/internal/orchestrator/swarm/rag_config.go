package swarm

import (
	"fmt"
	"path"

	"github.com/goccy/go-yaml"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// ragYAMLConfig mirrors the pgedge-rag-server Config struct for YAML generation.
// Only the fields the control plane needs to set are included.
type ragYAMLConfig struct {
	Server    ragServerYAML    `yaml:"server"`
	Pipelines []ragPipelineYAML `yaml:"pipelines"`
	Defaults  *ragDefaultsYAML  `yaml:"defaults,omitempty"`
}

type ragServerYAML struct {
	ListenAddress string `yaml:"listen_address"`
	Port          int    `yaml:"port"`
}

type ragPipelineYAML struct {
	Name         string          `yaml:"name"`
	Description  string          `yaml:"description,omitempty"`
	Database     ragDatabaseYAML `yaml:"database"`
	Tables       []ragTableYAML  `yaml:"tables"`
	EmbeddingLLM ragLLMYAML      `yaml:"embedding_llm"`
	RAGLLM       ragLLMYAML      `yaml:"rag_llm"`
	APIKeys      *ragAPIKeysYAML `yaml:"api_keys,omitempty"`
	TokenBudget  *int            `yaml:"token_budget,omitempty"`
	TopN         *int            `yaml:"top_n,omitempty"`
	SystemPrompt string          `yaml:"system_prompt,omitempty"`
	Search       *ragSearchYAML  `yaml:"search,omitempty"`
}

type ragDatabaseYAML struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`
}

type ragTableYAML struct {
	Table        string `yaml:"table"`
	TextColumn   string `yaml:"text_column"`
	VectorColumn string `yaml:"vector_column"`
	IDColumn     string `yaml:"id_column,omitempty"`
}

type ragLLMYAML struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url,omitempty"`
}

// ragAPIKeysYAML holds container-side file paths for each provider's API key.
type ragAPIKeysYAML struct {
	Anthropic string `yaml:"anthropic,omitempty"`
	OpenAI    string `yaml:"openai,omitempty"`
	Voyage    string `yaml:"voyage,omitempty"`
}

type ragSearchYAML struct {
	HybridEnabled *bool    `yaml:"hybrid_enabled,omitempty"`
	VectorWeight  *float64 `yaml:"vector_weight,omitempty"`
}

type ragDefaultsYAML struct {
	TokenBudget *int `yaml:"token_budget,omitempty"`
	TopN        *int `yaml:"top_n,omitempty"`
}

// RAGConfigParams holds all inputs needed to generate pgedge-rag-server.yaml.
type RAGConfigParams struct {
	Config       *database.RAGServiceConfig
	DatabaseName string
	DatabaseHost string
	DatabasePort int
	Username     string
	Password     string
	// KeysDir is the container-side directory where API key files are mounted,
	// e.g. "/app/keys". Key filenames follow the {pipeline}_{embedding|rag}.key
	// convention produced by extractRAGAPIKeys.
	KeysDir string
}

// GenerateRAGConfig generates the pgedge-rag-server.yaml content from the
// given parameters. API key paths in the generated YAML reference files under
// KeysDir so the RAG server reads them from the bind-mounted keys directory.
func GenerateRAGConfig(params *RAGConfigParams) ([]byte, error) {
	pipelines := make([]ragPipelineYAML, 0, len(params.Config.Pipelines))
	for _, p := range params.Config.Pipelines {
		pl, err := buildRAGPipelineYAML(p, params)
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, pl)
	}

	var defaults *ragDefaultsYAML
	if params.Config.Defaults != nil {
		src := params.Config.Defaults
		if src.TokenBudget != nil || src.TopN != nil {
			defaults = &ragDefaultsYAML{
				TokenBudget: src.TokenBudget,
				TopN:        src.TopN,
			}
		}
	}

	cfg := &ragYAMLConfig{
		Server: ragServerYAML{
			ListenAddress: "0.0.0.0",
			Port:          8080,
		},
		Pipelines: pipelines,
		Defaults:  defaults,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func buildRAGPipelineYAML(p database.RAGPipeline, params *RAGConfigParams) (ragPipelineYAML, error) {
	tables := make([]ragTableYAML, 0, len(p.Tables))
	for _, t := range p.Tables {
		tbl := ragTableYAML{
			Table:        t.Table,
			TextColumn:   t.TextColumn,
			VectorColumn: t.VectorColumn,
		}
		if t.IDColumn != nil {
			tbl.IDColumn = *t.IDColumn
		}
		tables = append(tables, tbl)
	}

	embLLM := ragLLMYAML{
		Provider: p.EmbeddingLLM.Provider,
		Model:    p.EmbeddingLLM.Model,
	}
	if p.EmbeddingLLM.BaseURL != nil {
		embLLM.BaseURL = *p.EmbeddingLLM.BaseURL
	}

	ragLLM := ragLLMYAML{
		Provider: p.RAGLLM.Provider,
		Model:    p.RAGLLM.Model,
	}
	if p.RAGLLM.BaseURL != nil {
		ragLLM.BaseURL = *p.RAGLLM.BaseURL
	}

	apiKeys, err := buildRAGAPIKeysYAML(p, params.KeysDir)
	if err != nil {
		return ragPipelineYAML{}, err
	}

	pipeline := ragPipelineYAML{
		Name: p.Name,
		Database: ragDatabaseYAML{
			Host:     params.DatabaseHost,
			Port:     params.DatabasePort,
			Database: params.DatabaseName,
			Username: params.Username,
			Password: params.Password,
			SSLMode:  "prefer",
		},
		Tables:       tables,
		EmbeddingLLM: embLLM,
		RAGLLM:       ragLLM,
		APIKeys:      apiKeys,
	}

	if p.Description != nil {
		pipeline.Description = *p.Description
	}
	pipeline.TokenBudget = p.TokenBudget
	pipeline.TopN = p.TopN
	if p.SystemPrompt != nil {
		pipeline.SystemPrompt = *p.SystemPrompt
	}
	if p.Search != nil {
		pipeline.Search = &ragSearchYAML{
			HybridEnabled: p.Search.HybridEnabled,
			VectorWeight:  p.Search.VectorWeight,
		}
	}

	return pipeline, nil
}

// buildRAGAPIKeysYAML maps each LLM provider that requires a key to the
// corresponding bind-mounted key file path inside the container.
// Embedding key: {keysDir}/{pipeline}_embedding.key
// RAG key:       {keysDir}/{pipeline}_rag.key
// If embedding and RAG use the same provider, the RAG key path takes precedence
// (both files contain the same value). Returns an error if both LLMs share a
// provider but were configured with different API keys.
func buildRAGAPIKeysYAML(p database.RAGPipeline, keysDir string) (*ragAPIKeysYAML, error) {
	// Reject mismatched keys for the same provider — the RAG server has a
	// single key slot per provider and cannot reconcile two different values.
	if p.EmbeddingLLM.Provider == p.RAGLLM.Provider &&
		p.EmbeddingLLM.APIKey != nil && *p.EmbeddingLLM.APIKey != "" &&
		p.RAGLLM.APIKey != nil && *p.RAGLLM.APIKey != "" &&
		*p.EmbeddingLLM.APIKey != *p.RAGLLM.APIKey {
		return nil, fmt.Errorf("pipeline %q: embedding_llm and rag_llm share provider %q but have different API keys",
			p.Name, p.EmbeddingLLM.Provider)
	}

	keys := &ragAPIKeysYAML{}

	// Embedding provider key
	if p.EmbeddingLLM.APIKey != nil && *p.EmbeddingLLM.APIKey != "" {
		keyPath := path.Join(keysDir, p.Name+"_embedding.key")
		switch p.EmbeddingLLM.Provider {
		case "anthropic":
			keys.Anthropic = keyPath
		case "openai":
			keys.OpenAI = keyPath
		case "voyage":
			keys.Voyage = keyPath
		}
	}

	// RAG provider key (overwrites if same provider as embedding)
	if p.RAGLLM.APIKey != nil && *p.RAGLLM.APIKey != "" {
		keyPath := path.Join(keysDir, p.Name+"_rag.key")
		switch p.RAGLLM.Provider {
		case "anthropic":
			keys.Anthropic = keyPath
		case "openai":
			keys.OpenAI = keyPath
		}
	}

	if keys.Anthropic == "" && keys.OpenAI == "" && keys.Voyage == "" {
		return nil, nil
	}
	return keys, nil
}
