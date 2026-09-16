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

// ragPipelineNamePatternText is the allowlist pattern for RAG pipeline names.
// It is kept as a const so that the compiled regexp and the error message both
// reference the same literal and cannot drift apart.
const ragPipelineNamePatternText = `^[a-z0-9_][a-z0-9_-]*$`

// ragPipelineNamePattern restricts pipeline names to lowercase alphanumeric
// characters, hyphens, and underscores. The first character must not be a
// hyphen so that names are safe as filename components and cannot be
// misinterpreted as CLI flags if ever passed to a command.
var ragPipelineNamePattern = regexp.MustCompile(ragPipelineNamePatternText)

// ragPipelineNameMaxLen mirrors the RAG server's own maximum pipeline name
// length (checked at its startup). Enforcing it here too means an
// over-length name is rejected at config-submission time instead of
// crash-looping the container after deployment.
const ragPipelineNameMaxLen = 63

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

// RAGRerankConfig represents an optional reranking stage that reorders search
// results by relevance immediately before context building. The reranked
// provider (Voyage) draws its API key from the same slot as embedding_llm
// when it also uses Voyage; APIKey is only required here when rerank uses a
// provider the pipeline's embedding_llm/rag_llm doesn't already authenticate
// with.
type RAGRerankConfig struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	APIKey   *string `json:"api_key,omitempty"`
	TopK     *int    `json:"top_k,omitempty"`
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
	Rerank       *RAGRerankConfig     `json:"rerank,omitempty"`
	// AllowIncludeSources permits clients querying this pipeline to request
	// the raw content of retrieved documents via include_sources. Defaults
	// to false: exposing a corpus is an explicit per-pipeline decision, not
	// something inherited from defaults.
	AllowIncludeSources *bool `json:"allow_include_sources,omitempty"`
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

// Anthropic has no embeddings API, so it is intentionally absent here even
// though it's a valid rag_llm provider.
var ragEmbeddingProviders = []string{"openai", "voyage", "gemini", "ollama"}
var ragLLMProviders = []string{"anthropic", "openai", "gemini", "ollama"}
var ragRerankProviders = []string{"voyage"}

var ragKnownTopLevelKeys = map[string]bool{
	"pipelines": true,
	"defaults":  true,
}

// ParseRAGServiceConfig parses and validates a config map into a typed
// RAGServiceConfig. When isUpdate is true, api_key is not required on
// providers that normally need one: an update is expected to have already had
// omitted secrets restored from the stored spec (see
// Spec.DefaultOptionalFieldsFrom), and a key that's still missing after that
// (e.g. a brand-new pipeline) is caught at deploy time instead, when
// ParseRAGServiceConfig is called again with isUpdate=false against the final,
// merged config.
func ParseRAGServiceConfig(config map[string]any, isUpdate bool) (*RAGServiceConfig, []error) {
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
		errs = append(errs, validateRAGPipeline(p, i, seenNames, isUpdate)...)
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

func validateRAGPipeline(p RAGPipeline, i int, seenNames map[string]bool, isUpdate bool) []error {
	var errs []error
	prefix := fmt.Sprintf("pipelines[%d]", i)

	// name (required, allowlist, max length, unique)
	if p.Name == "" {
		errs = append(errs, fmt.Errorf("%s.name is required", prefix))
	} else if len(p.Name) > ragPipelineNameMaxLen {
		errs = append(errs, fmt.Errorf("%s.name %q exceeds maximum length of %d characters", prefix, p.Name, ragPipelineNameMaxLen))
	} else if !ragPipelineNamePattern.MatchString(p.Name) {
		errs = append(errs, fmt.Errorf("%s.name %q is invalid: must match %s", prefix, p.Name, ragPipelineNamePatternText))
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
	errs = append(errs, validateRAGLLMConfig(p.EmbeddingLLM, prefix+".embedding_llm", ragEmbeddingProviders, isUpdate)...)

	// rag_llm (required)
	errs = append(errs, validateRAGLLMConfig(p.RAGLLM, prefix+".rag_llm", ragLLMProviders, isUpdate)...)

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

	// rerank (optional)
	if p.Rerank != nil {
		errs = append(errs, validateRAGRerankConfig(*p.Rerank, p.EmbeddingLLM, prefix+".rerank", isUpdate)...)
	}

	return errs
}

// validateRAGRerankConfig validates an optional reranking stage. embeddingLLM
// is the pipeline's embedding_llm config: since the reranker's only supported
// provider (Voyage) shares a single API key slot per pipeline with an
// embedding_llm that also uses Voyage, rerank.api_key is only required when
// embedding_llm isn't already supplying that key — or, on an update, is
// expected to have already been restored from the stored spec (see
// Spec.DefaultOptionalFieldsFrom), same as the LLM provider fields above.
func validateRAGRerankConfig(r RAGRerankConfig, embeddingLLM RAGPipelineLLMConfig, prefix string, isUpdate bool) []error {
	var errs []error

	// provider (required, currently only voyage is supported)
	if r.Provider == "" {
		return []error{fmt.Errorf("%s.provider is required", prefix)}
	}
	if !slices.Contains(ragRerankProviders, r.Provider) {
		return []error{fmt.Errorf("%s.provider must be one of: %s", prefix, strings.Join(ragRerankProviders, ", "))}
	}

	// model (required)
	if r.Model == "" {
		errs = append(errs, fmt.Errorf("%s.model is required", prefix))
	}

	// api_key: required unless embedding_llm already uses this provider and
	// supplies a key, or this is an update (see doc comment above).
	embeddingHasKey := embeddingLLM.Provider == r.Provider && embeddingLLM.APIKey != nil && *embeddingLLM.APIKey != ""
	if !isUpdate && !embeddingHasKey && (r.APIKey == nil || *r.APIKey == "") {
		errs = append(errs, fmt.Errorf("%s.api_key is required when provider is %q and embedding_llm doesn't already supply one", prefix, r.Provider))
	}

	// If both roles supply a key for the shared provider, they must agree —
	// the RAG server has a single key slot per provider and cannot reconcile
	// two different values.
	if embeddingLLM.Provider == r.Provider && embeddingLLM.APIKey != nil && *embeddingLLM.APIKey != "" &&
		r.APIKey != nil && *r.APIKey != "" && *embeddingLLM.APIKey != *r.APIKey {
		errs = append(errs, fmt.Errorf("%s.api_key does not match embedding_llm.api_key: both use provider %q and must share the same key", prefix, r.Provider))
	}

	// top_k (optional, >= 0 — 0 means "reorder all, drop none", matching upstream)
	if r.TopK != nil && *r.TopK < 0 {
		errs = append(errs, fmt.Errorf("%s.top_k must not be negative", prefix))
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

func validateRAGLLMConfig(llm RAGPipelineLLMConfig, prefix string, validProviders []string, isUpdate bool) []error {
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

	// Provider-specific: api_key required for non-ollama providers, except on
	// an update, where an omitted key is expected to already have been
	// restored from the stored spec before validation runs.
	switch llm.Provider {
	case "anthropic", "openai", "voyage", "gemini":
		if !isUpdate && (llm.APIKey == nil || *llm.APIKey == "") {
			errs = append(errs, fmt.Errorf("%s.api_key is required when provider is %q", prefix, llm.Provider))
		}
	}

	return errs
}
