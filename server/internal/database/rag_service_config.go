package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ragPipelineNamePattern restricts pipeline names to lowercase alphanumeric
// characters, hyphens, and underscores. This keeps key filenames
// ({name}_embedding.key / {name}_rag.key) safe and auditable.
var ragPipelineNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// RAGPipelineLLMConfig represents LLM configuration for an embedding or RAG step.
type RAGPipelineLLMConfig struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	APIKey   *string `json:"api_key,omitempty"`
	BaseURL  *string `json:"base_url,omitempty"`
}

// RAGPipelineTable represents a table configuration for a pipeline.
type RAGPipelineTable struct {
	Table        string  `json:"table"`
	TextColumn   string  `json:"text_column"`
	VectorColumn string  `json:"vector_column"`
	IDColumn     *string `json:"id_column,omitempty"`
}

// RAGPipelineSearch represents search tuning for a pipeline.
type RAGPipelineSearch struct {
	HybridEnabled *bool    `json:"hybrid_enabled,omitempty"`
	VectorWeight  *float64 `json:"vector_weight,omitempty"`
}

// RAGPipeline represents a single RAG pipeline configuration.
type RAGPipeline struct {
	Name         string               `json:"name"`
	Description  *string              `json:"description,omitempty"`
	Tables       []RAGPipelineTable   `json:"tables"`
	EmbeddingLLM RAGPipelineLLMConfig `json:"embedding_llm"`
	RAGLLM       RAGPipelineLLMConfig `json:"rag_llm"`
	TokenBudget  *int                 `json:"token_budget,omitempty"`
	TopN         *int                 `json:"top_n,omitempty"`
	SystemPrompt *string              `json:"system_prompt,omitempty"`
	Search       *RAGPipelineSearch   `json:"search,omitempty"`
}

// RAGDefaults represents default values applied to all pipelines.
type RAGDefaults struct {
	TokenBudget *int `json:"token_budget,omitempty"`
	TopN        *int `json:"top_n,omitempty"`
}

// RAGServiceConfig is the typed internal representation of RAG service configuration.
// It is parsed from the ServiceSpec.Config map[string]any and validated.
type RAGServiceConfig struct {
	Pipelines []RAGPipeline `json:"pipelines"`
	Defaults  *RAGDefaults  `json:"defaults,omitempty"`
}

var ragEmbeddingProviders = []string{"anthropic", "openai", "voyage", "ollama"}
var ragLLMProviders = []string{"anthropic", "openai", "ollama"}

var ragKnownTopLevelKeys = map[string]bool{
	"pipelines": true,
	"defaults":  true,
}

// ParseRAGServiceConfig parses and validates a config map into a typed RAGServiceConfig.
func ParseRAGServiceConfig(config map[string]any, _ bool) (*RAGServiceConfig, []error) {
	var errs []error

	// Check for unknown top-level keys
	var unknownKeys []string
	for k := range config {
		if !ragKnownTopLevelKeys[k] {
			unknownKeys = append(unknownKeys, fmt.Sprintf("%q", k))
		}
	}
	if len(unknownKeys) > 0 {
		sort.Strings(unknownKeys)
		errs = append(errs, fmt.Errorf("unknown config key(s): %s", strings.Join(unknownKeys, ", ")))
		return nil, errs
	}

	// Re-serialize to JSON so we can decode into the typed struct.
	// Numbers in map[string]any are float64; re-serializing produces valid JSON
	// that unmarshals correctly into integer fields (e.g. token_budget).
	data, err := json.Marshal(config)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to serialize config: %w", err)}
	}

	var cfg RAGServiceConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, []error{fmt.Errorf("invalid config: %w", err)}
	}

	// Validate pipelines (required, non-empty)
	if len(cfg.Pipelines) == 0 {
		errs = append(errs, fmt.Errorf("pipelines is required and must contain at least one pipeline"))
	}
	seenNames := make(map[string]bool, len(cfg.Pipelines))
	for i, p := range cfg.Pipelines {
		errs = append(errs, validateRAGPipeline(p, i, seenNames)...)
	}

	// Validate defaults (optional)
	if cfg.Defaults != nil {
		if cfg.Defaults.TokenBudget != nil && *cfg.Defaults.TokenBudget <= 0 {
			errs = append(errs, fmt.Errorf("defaults.token_budget must be a positive integer"))
		}
		if cfg.Defaults.TopN != nil && *cfg.Defaults.TopN <= 0 {
			errs = append(errs, fmt.Errorf("defaults.top_n must be a positive integer"))
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}
	return &cfg, nil
}

func validateRAGPipeline(p RAGPipeline, i int, seenNames map[string]bool) []error {
	var errs []error
	prefix := fmt.Sprintf("pipelines[%d]", i)

	// name (required, allowlist, unique)
	if p.Name == "" {
		errs = append(errs, fmt.Errorf("%s.name is required", prefix))
	} else if !ragPipelineNamePattern.MatchString(p.Name) {
		errs = append(errs, fmt.Errorf("%s.name %q is invalid: must match ^[a-z0-9_-]+$", prefix, p.Name))
	} else if seenNames[p.Name] {
		errs = append(errs, fmt.Errorf("pipelines contains duplicate name %q", p.Name))
	} else {
		seenNames[p.Name] = true
	}

	// tables (required, non-empty)
	if len(p.Tables) == 0 {
		errs = append(errs, fmt.Errorf("%s.tables is required and must contain at least one table", prefix))
	}
	for j, t := range p.Tables {
		errs = append(errs, validateRAGTable(t, prefix, j)...)
	}

	// embedding_llm (required)
	errs = append(errs, validateRAGLLMConfig(p.EmbeddingLLM, prefix+".embedding_llm", ragEmbeddingProviders)...)

	// rag_llm (required)
	errs = append(errs, validateRAGLLMConfig(p.RAGLLM, prefix+".rag_llm", ragLLMProviders)...)

	// token_budget (optional, > 0)
	if p.TokenBudget != nil && *p.TokenBudget <= 0 {
		errs = append(errs, fmt.Errorf("%s.token_budget must be a positive integer", prefix))
	}

	// top_n (optional, > 0)
	if p.TopN != nil && *p.TopN <= 0 {
		errs = append(errs, fmt.Errorf("%s.top_n must be a positive integer", prefix))
	}

	// search.vector_weight (optional, [0.0, 1.0])
	if p.Search != nil && p.Search.VectorWeight != nil {
		vw := *p.Search.VectorWeight
		if vw < 0.0 || vw > 1.0 {
			errs = append(errs, fmt.Errorf("%s.search.vector_weight must be between 0.0 and 1.0", prefix))
		}
	}

	return errs
}

func validateRAGTable(t RAGPipelineTable, prefix string, j int) []error {
	var errs []error
	tPrefix := fmt.Sprintf("%s.tables[%d]", prefix, j)
	if t.Table == "" {
		errs = append(errs, fmt.Errorf("%s.table is required", tPrefix))
	}
	if t.TextColumn == "" {
		errs = append(errs, fmt.Errorf("%s.text_column is required", tPrefix))
	}
	if t.VectorColumn == "" {
		errs = append(errs, fmt.Errorf("%s.vector_column is required", tPrefix))
	}
	return errs
}

func validateRAGLLMConfig(llm RAGPipelineLLMConfig, prefix string, validProviders []string) []error {
	var errs []error

	// provider (required)
	if llm.Provider == "" {
		return []error{fmt.Errorf("%s.provider is required", prefix)}
	}
	if !slices.Contains(validProviders, llm.Provider) {
		return []error{fmt.Errorf("%s.provider must be one of: %s", prefix, strings.Join(validProviders, ", "))}
	}

	// model (required)
	if llm.Model == "" {
		errs = append(errs, fmt.Errorf("%s.model is required", prefix))
	}

	// Provider-specific: api_key required for non-ollama providers
	switch llm.Provider {
	case "anthropic", "openai", "voyage":
		if llm.APIKey == nil || *llm.APIKey == "" {
			errs = append(errs, fmt.Errorf("%s.api_key is required when provider is %q", prefix, llm.Provider))
		}
	}

	return errs
}
