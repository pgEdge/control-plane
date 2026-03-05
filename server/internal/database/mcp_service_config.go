package database

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// MCPServiceUser represents a bootstrap user account for the MCP service.
type MCPServiceUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// MCPServiceConfig is the typed internal representation of MCP service configuration.
// It is parsed from the ServiceSpec.Config map[string]any and validated.
type MCPServiceConfig struct {
	// Required
	LLMProvider     string  `json:"llm_provider"`
	LLMModel        string  `json:"llm_model"`
	AnthropicAPIKey *string `json:"anthropic_api_key,omitempty"`
	OpenAIAPIKey    *string `json:"openai_api_key,omitempty"`
	OllamaURL       *string `json:"ollama_url,omitempty"`

	// Optional - security
	AllowWrites *bool            `json:"allow_writes,omitempty"`
	InitToken   *string          `json:"init_token,omitempty"`
	InitUsers   []MCPServiceUser `json:"init_users,omitempty"`

	// Optional - embeddings
	EmbeddingProvider *string `json:"embedding_provider,omitempty"`
	EmbeddingModel    *string `json:"embedding_model,omitempty"`
	EmbeddingAPIKey   *string `json:"embedding_api_key,omitempty"`

	// Optional - LLM tuning (overridable defaults)
	LLMTemperature *float64 `json:"llm_temperature,omitempty"`
	LLMMaxTokens   *int     `json:"llm_max_tokens,omitempty"`

	// Optional - connection pool (overridable defaults)
	PoolMaxConns *int `json:"pool_max_conns,omitempty"`

	// Optional - tool toggles (all enabled by default)
	DisableQueryDatabase       *bool `json:"disable_query_database,omitempty"`
	DisableGetSchemaInfo       *bool `json:"disable_get_schema_info,omitempty"`
	DisableSimilaritySearch    *bool `json:"disable_similarity_search,omitempty"`
	DisableExecuteExplain      *bool `json:"disable_execute_explain,omitempty"`
	DisableGenerateEmbedding   *bool `json:"disable_generate_embedding,omitempty"`
	DisableSearchKnowledgebase *bool `json:"disable_search_knowledgebase,omitempty"`
	DisableCountRows           *bool `json:"disable_count_rows,omitempty"`
}

// mcpKnownKeys is the set of all valid config keys for MCP service configuration.
var mcpKnownKeys = map[string]bool{
	"llm_provider":                 true,
	"llm_model":                    true,
	"anthropic_api_key":            true,
	"openai_api_key":               true,
	"ollama_url":                   true,
	"allow_writes":                 true,
	"init_token":                   true,
	"init_users":                   true,
	"embedding_provider":           true,
	"embedding_model":              true,
	"embedding_api_key":            true,
	"llm_temperature":              true,
	"llm_max_tokens":               true,
	"pool_max_conns":               true,
	"disable_query_database":       true,
	"disable_get_schema_info":      true,
	"disable_similarity_search":    true,
	"disable_execute_explain":      true,
	"disable_generate_embedding":   true,
	"disable_search_knowledgebase": true,
	"disable_count_rows":           true,
}

var validLLMProviders = []string{"anthropic", "openai", "ollama"}
var validEmbeddingProviders = []string{"voyage", "openai", "ollama"}

// ParseMCPServiceConfig parses and validates a config map into a typed MCPServiceConfig.
// If isUpdate is true, bootstrap-only fields (init_token, init_users) are rejected.
func ParseMCPServiceConfig(config map[string]any, isUpdate bool) (*MCPServiceConfig, []error) {
	var errs []error

	// Check for unknown keys
	errs = append(errs, validateUnknownKeys(config)...)

	// Check for bootstrap-only fields on update
	if isUpdate {
		if _, ok := config["init_token"]; ok {
			errs = append(errs, fmt.Errorf("init_token can only be set during initial provisioning"))
		}
		if _, ok := config["init_users"]; ok {
			errs = append(errs, fmt.Errorf("init_users can only be set during initial provisioning"))
		}
	}

	// Parse required string fields
	llmProvider, providerErrs := requireString(config, "llm_provider")
	errs = append(errs, providerErrs...)

	llmModel, modelErrs := requireString(config, "llm_model")
	errs = append(errs, modelErrs...)

	// Validate llm_provider enum
	if llmProvider != "" && !slices.Contains(validLLMProviders, llmProvider) {
		errs = append(errs, fmt.Errorf("llm_provider must be one of: %s", strings.Join(validLLMProviders, ", ")))
	}

	// Provider-specific API key cross-validation
	var anthropicKey, openaiKey, ollamaURL *string
	if llmProvider != "" && slices.Contains(validLLMProviders, llmProvider) {
		switch llmProvider {
		case "anthropic":
			key, keyErrs := requireStringForProvider(config, "anthropic_api_key", "anthropic")
			errs = append(errs, keyErrs...)
			if key != "" {
				anthropicKey = &key
			}
		case "openai":
			key, keyErrs := requireStringForProvider(config, "openai_api_key", "openai")
			errs = append(errs, keyErrs...)
			if key != "" {
				openaiKey = &key
			}
		case "ollama":
			url, urlErrs := requireStringForProvider(config, "ollama_url", "ollama")
			errs = append(errs, urlErrs...)
			if url != "" {
				ollamaURL = &url
			}
		}
	}

	// Parse optional fields
	allowWrites, awErrs := optionalBool(config, "allow_writes")
	errs = append(errs, awErrs...)

	initToken, itErrs := optionalString(config, "init_token")
	errs = append(errs, itErrs...)

	initUsers, iuErrs := parseInitUsers(config)
	errs = append(errs, iuErrs...)

	embeddingProvider, epErrs := optionalString(config, "embedding_provider")
	errs = append(errs, epErrs...)

	embeddingModel, emErrs := optionalString(config, "embedding_model")
	errs = append(errs, emErrs...)

	embeddingAPIKey, eakErrs := optionalString(config, "embedding_api_key")
	errs = append(errs, eakErrs...)

	llmTemperature, ltErrs := optionalFloat64(config, "llm_temperature")
	errs = append(errs, ltErrs...)

	llmMaxTokens, lmtErrs := optionalInt(config, "llm_max_tokens")
	errs = append(errs, lmtErrs...)

	poolMaxConns, pmcErrs := optionalInt(config, "pool_max_conns")
	errs = append(errs, pmcErrs...)

	// Tool toggles
	disableQueryDB, dqErrs := optionalBool(config, "disable_query_database")
	errs = append(errs, dqErrs...)
	disableGetSchema, dgsErrs := optionalBool(config, "disable_get_schema_info")
	errs = append(errs, dgsErrs...)
	disableSimilarity, dssErrs := optionalBool(config, "disable_similarity_search")
	errs = append(errs, dssErrs...)
	disableExplain, deErrs := optionalBool(config, "disable_execute_explain")
	errs = append(errs, deErrs...)
	disableGenEmbed, dgeErrs := optionalBool(config, "disable_generate_embedding")
	errs = append(errs, dgeErrs...)
	disableSearchKB, dskErrs := optionalBool(config, "disable_search_knowledgebase")
	errs = append(errs, dskErrs...)
	disableCountRows, dcrErrs := optionalBool(config, "disable_count_rows")
	errs = append(errs, dcrErrs...)

	// Range validations
	if llmTemperature != nil {
		if *llmTemperature < 0.0 || *llmTemperature > 2.0 {
			errs = append(errs, fmt.Errorf("llm_temperature must be between 0.0 and 2.0"))
		}
	}
	if llmMaxTokens != nil {
		if *llmMaxTokens <= 0 {
			errs = append(errs, fmt.Errorf("llm_max_tokens must be a positive integer"))
		}
	}
	if poolMaxConns != nil {
		if *poolMaxConns <= 0 {
			errs = append(errs, fmt.Errorf("pool_max_conns must be a positive integer"))
		}
	}

	// Embedding config cross-validation
	if embeddingProvider == nil {
		if embeddingModel != nil {
			errs = append(errs, fmt.Errorf("embedding_model must not be set without embedding_provider"))
		}
		if embeddingAPIKey != nil {
			errs = append(errs, fmt.Errorf("embedding_api_key must not be set without embedding_provider"))
		}
	}
	if embeddingProvider != nil {
		if !slices.Contains(validEmbeddingProviders, *embeddingProvider) {
			errs = append(errs, fmt.Errorf("embedding_provider must be one of: %s", strings.Join(validEmbeddingProviders, ", ")))
		} else {
			if embeddingModel == nil {
				errs = append(errs, fmt.Errorf("embedding_model is required when embedding_provider is set"))
			}
			// Providers that require an API key
			switch *embeddingProvider {
			case "voyage", "openai":
				if embeddingAPIKey == nil {
					errs = append(errs, fmt.Errorf("embedding_api_key is required when embedding_provider is %q", *embeddingProvider))
				}
			}
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}

	return &MCPServiceConfig{
		LLMProvider:                llmProvider,
		LLMModel:                   llmModel,
		AnthropicAPIKey:            anthropicKey,
		OpenAIAPIKey:               openaiKey,
		OllamaURL:                  ollamaURL,
		AllowWrites:                allowWrites,
		InitToken:                  initToken,
		InitUsers:                  initUsers,
		EmbeddingProvider:          embeddingProvider,
		EmbeddingModel:             embeddingModel,
		EmbeddingAPIKey:            embeddingAPIKey,
		LLMTemperature:             llmTemperature,
		LLMMaxTokens:               llmMaxTokens,
		PoolMaxConns:               poolMaxConns,
		DisableQueryDatabase:       disableQueryDB,
		DisableGetSchemaInfo:       disableGetSchema,
		DisableSimilaritySearch:    disableSimilarity,
		DisableExecuteExplain:      disableExplain,
		DisableGenerateEmbedding:   disableGenEmbed,
		DisableSearchKnowledgebase: disableSearchKB,
		DisableCountRows:           disableCountRows,
	}, nil
}

// validateUnknownKeys checks for keys not in the known set.
func validateUnknownKeys(config map[string]any) []error {
	var unknown []string
	for k := range config {
		if !mcpKnownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	if len(unknown) == 1 {
		return []error{fmt.Errorf("unknown config key %q", unknown[0])}
	}
	quoted := make([]string, len(unknown))
	for i, k := range unknown {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	return []error{fmt.Errorf("unknown config keys: %s", strings.Join(quoted, ", "))}
}

// requireString extracts a required non-empty string from the config map.
func requireString(config map[string]any, key string) (string, []error) {
	val, ok := config[key]
	if !ok {
		return "", []error{fmt.Errorf("%s is required", key)}
	}
	s, ok := val.(string)
	if !ok {
		return "", []error{fmt.Errorf("%s must be a string", key)}
	}
	if s == "" {
		return "", []error{fmt.Errorf("%s must not be empty", key)}
	}
	return s, nil
}

// requireStringForProvider extracts a required non-empty string for a specific provider.
func requireStringForProvider(config map[string]any, key, provider string) (string, []error) {
	val, ok := config[key]
	if !ok {
		return "", []error{fmt.Errorf("%s is required when llm_provider is %q", key, provider)}
	}
	s, ok := val.(string)
	if !ok {
		return "", []error{fmt.Errorf("%s must be a string", key)}
	}
	if s == "" {
		return "", []error{fmt.Errorf("%s must not be empty", key)}
	}
	return s, nil
}

// optionalString extracts an optional string from the config map.
func optionalString(config map[string]any, key string) (*string, []error) {
	val, ok := config[key]
	if !ok {
		return nil, nil
	}
	s, ok := val.(string)
	if !ok {
		return nil, []error{fmt.Errorf("%s must be a string", key)}
	}
	if s == "" {
		return nil, []error{fmt.Errorf("%s must not be empty", key)}
	}
	return &s, nil
}

// optionalBool extracts an optional boolean from the config map.
func optionalBool(config map[string]any, key string) (*bool, []error) {
	val, ok := config[key]
	if !ok {
		return nil, nil
	}
	b, ok := val.(bool)
	if !ok {
		return nil, []error{fmt.Errorf("%s must be a boolean", key)}
	}
	return &b, nil
}

// optionalFloat64 extracts an optional float64 from the config map.
// JSON numbers may arrive as float64 or json.Number.
func optionalFloat64(config map[string]any, key string) (*float64, []error) {
	val, ok := config[key]
	if !ok {
		return nil, nil
	}
	switch v := val.(type) {
	case float64:
		return &v, nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil, []error{fmt.Errorf("%s must be a number", key)}
		}
		return &f, nil
	default:
		return nil, []error{fmt.Errorf("%s must be a number", key)}
	}
}

// optionalInt extracts an optional integer from the config map.
// JSON numbers arrive as float64; we reject non-integer values.
func optionalInt(config map[string]any, key string) (*int, []error) {
	val, ok := config[key]
	if !ok {
		return nil, nil
	}
	switch v := val.(type) {
	case float64:
		i := int(v)
		if float64(i) != v {
			return nil, []error{fmt.Errorf("%s must be an integer", key)}
		}
		return &i, nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return nil, []error{fmt.Errorf("%s must be an integer", key)}
		}
		intVal := int(i)
		return &intVal, nil
	default:
		return nil, []error{fmt.Errorf("%s must be an integer", key)}
	}
}

// parseInitUsers extracts and validates the init_users field.
func parseInitUsers(config map[string]any) ([]MCPServiceUser, []error) {
	val, ok := config["init_users"]
	if !ok {
		return nil, nil
	}

	arr, ok := val.([]any)
	if !ok {
		return nil, []error{fmt.Errorf("init_users must be an array")}
	}
	if len(arr) == 0 {
		return nil, []error{fmt.Errorf("init_users must contain at least one entry")}
	}

	var errs []error
	users := make([]MCPServiceUser, 0, len(arr))
	seenUsernames := make(map[string]bool, len(arr))

	for i, entry := range arr {
		m, ok := entry.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("init_users[%d] must be an object", i))
			continue
		}

		username, uOk := m["username"]
		password, pOk := m["password"]

		if !uOk {
			errs = append(errs, fmt.Errorf("init_users[%d].username is required", i))
		}
		if !pOk {
			errs = append(errs, fmt.Errorf("init_users[%d].password is required", i))
		}

		var usernameStr, passwordStr string
		if uOk {
			usernameStr, ok = username.(string)
			if !ok {
				errs = append(errs, fmt.Errorf("init_users[%d].username must be a string", i))
			} else if usernameStr == "" {
				errs = append(errs, fmt.Errorf("init_users[%d].username must not be empty", i))
			}
		}
		if pOk {
			passwordStr, ok = password.(string)
			if !ok {
				errs = append(errs, fmt.Errorf("init_users[%d].password must be a string", i))
			} else if passwordStr == "" {
				errs = append(errs, fmt.Errorf("init_users[%d].password must not be empty", i))
			}
		}

		if usernameStr != "" {
			if seenUsernames[usernameStr] {
				errs = append(errs, fmt.Errorf("init_users contains duplicate username %q", usernameStr))
			}
			seenUsernames[usernameStr] = true
		}

		if usernameStr != "" && passwordStr != "" {
			users = append(users, MCPServiceUser{
				Username: usernameStr,
				Password: passwordStr,
			})
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}
	return users, nil
}
