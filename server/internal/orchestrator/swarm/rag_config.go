package swarm

import (
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type ragConfigOptions struct {
	ServiceSpec     *database.ServiceSpec
	DatabaseHost    string
	DatabasePort    int
	DatabaseName    string
	Username        string
	Password        string
	// KeysDirPath is the container-internal path where API key files are mounted.
	// When set, an api_keys section is written into the YAML referencing files
	// under that directory. Populated from RAGAPIKeysResource.
	KeysDirPath string
}

// generateRAGConfig renders the pgedge-rag-server.yaml content from a ServiceSpec.
// API keys are NOT included here — they are passed as environment variables by
// buildServiceEnvVars so they are never written to disk in the Swarm config.
//
// cfg must contain a "pipelines" array with at least one pipeline object.
func generateRAGConfig(opts *ragConfigOptions) (string, error) {
	cfg := opts.ServiceSpec.Config

	rawPipelines, ok := cfg["pipelines"].([]any)
	if !ok || len(rawPipelines) == 0 {
		return "", fmt.Errorf("RAG config must contain a non-empty \"pipelines\" array")
	}

	tokenBudget := intConfigField(cfg, "token_budget", 4000)
	topN := intConfigField(cfg, "top_n", 10)

	var sb strings.Builder
	sb.WriteString("server:\n")
	sb.WriteString("  listen_address: \"0.0.0.0\"\n")
	sb.WriteString("  port: 8080\n")
	sb.WriteString("\n")
	sb.WriteString("defaults:\n")
	sb.WriteString(fmt.Sprintf("  token_budget: %d\n", tokenBudget))
	sb.WriteString(fmt.Sprintf("  top_n: %d\n", topN))
	sb.WriteString("\n")

	// Write api_keys section pointing to bind-mounted key files.
	if opts.KeysDirPath != "" {
		keys := collectRAGAPIKeys(opts.ServiceSpec.Config)
		if len(keys) > 0 {
			sb.WriteString("api_keys:\n")
			for _, def := range []struct{ filename, yamlKey string }{
				{"openai", "openai"},
				{"anthropic", "anthropic"},
				{"voyage", "voyage"},
			} {
				if _, ok := keys[def.filename]; ok {
					sb.WriteString(fmt.Sprintf("  %s: %q\n", def.yamlKey, opts.KeysDirPath+"/"+def.filename))
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("pipelines:\n")

	for _, raw := range rawPipelines {
		p, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid pipeline entry in RAG config")
		}
		if err := writePipelineYAML(&sb, p, opts); err != nil {
			return "", err
		}
	}

	return sb.String(), nil
}

// writePipelineYAML appends one pipeline block to sb, reading fields from p.
// Database connection is always taken from opts (shared across all pipelines).
func writePipelineYAML(sb *strings.Builder, p map[string]any, opts *ragConfigOptions) error {
	name := stringConfigField(p, "pipeline_name", "default")
	desc := stringConfigField(p, "pipeline_description", "")
	embeddingProvider := stringConfigField(p, "embedding_provider", "")
	embeddingModel := stringConfigField(p, "embedding_model", "")
	embeddingBaseURL := stringConfigField(p, "embedding_base_url", "")
	llmProvider := stringConfigField(p, "llm_provider", "")
	llmModel := stringConfigField(p, "llm_model", "")
	llmBaseURL := stringConfigField(p, "llm_base_url", "")
	ollamaURL := stringConfigField(p, "ollama_url", "")

	tables, err := buildRAGTablesYAML(p)
	if err != nil {
		return err
	}

	sb.WriteString(fmt.Sprintf("  - name: %q\n", name))
	if desc != "" {
		sb.WriteString(fmt.Sprintf("    description: %q\n", desc))
	}
	sb.WriteString("    database:\n")
	sb.WriteString(fmt.Sprintf("      host: %q\n", opts.DatabaseHost))
	sb.WriteString(fmt.Sprintf("      port: %d\n", opts.DatabasePort))
	sb.WriteString(fmt.Sprintf("      database: %q\n", opts.DatabaseName))
	sb.WriteString(fmt.Sprintf("      username: %q\n", opts.Username))
	sb.WriteString(fmt.Sprintf("      password: %q\n", opts.Password))
	sb.WriteString("      ssl_mode: \"prefer\"\n")
	sb.WriteString("    tables:\n")
	sb.WriteString(tables)
	sb.WriteString("    embedding_llm:\n")
	sb.WriteString(fmt.Sprintf("      provider: %q\n", embeddingProvider))
	sb.WriteString(fmt.Sprintf("      model: %q\n", embeddingModel))
	if embeddingBaseURL != "" {
		sb.WriteString(fmt.Sprintf("      base_url: %q\n", embeddingBaseURL))
	}
	sb.WriteString("    rag_llm:\n")
	sb.WriteString(fmt.Sprintf("      provider: %q\n", llmProvider))
	sb.WriteString(fmt.Sprintf("      model: %q\n", llmModel))
	if llmBaseURL != "" {
		sb.WriteString(fmt.Sprintf("      base_url: %q\n", llmBaseURL))
	}
	if ollamaURL != "" {
		sb.WriteString(fmt.Sprintf("    ollama_url: %q\n", ollamaURL))
	}
	return nil
}

func buildRAGTablesYAML(cfg map[string]any) (string, error) {
	rawTables, _ := cfg["tables"].([]any)
	if len(rawTables) == 0 {
		return "", fmt.Errorf("no tables configured for RAG service")
	}

	var sb strings.Builder
	for _, t := range rawTables {
		tableMap, ok := t.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid table entry in RAG config")
		}
		tableName, _ := tableMap["table"].(string)
		textCol, _ := tableMap["text_column"].(string)
		vectorCol, _ := tableMap["vector_column"].(string)
		idCol, _ := tableMap["id_column"].(string)

		sb.WriteString(fmt.Sprintf("      - table: %q\n", tableName))
		sb.WriteString(fmt.Sprintf("        text_column: %q\n", textCol))
		sb.WriteString(fmt.Sprintf("        vector_column: %q\n", vectorCol))
		if idCol != "" {
			sb.WriteString(fmt.Sprintf("        id_column: %q\n", idCol))
		}
	}
	return sb.String(), nil
}

func stringConfigField(cfg map[string]any, key, defaultVal string) string {
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func intConfigField(cfg map[string]any, key string, defaultVal int) int {
	switch v := cfg[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return defaultVal
}
