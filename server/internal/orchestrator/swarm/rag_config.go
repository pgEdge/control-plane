package swarm

import (
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type ragConfigOptions struct {
	ServiceSpec  *database.ServiceSpec
	DatabaseHost string
	DatabasePort int
	DatabaseName string
	Username     string
	Password     string
}

// generateRAGConfig renders the pgedge-rag-server.yaml content from a ServiceSpec.
// API keys are NOT included here — they are passed as environment variables by
// buildServiceEnvVars so they are never written to disk in the Swarm config.
func generateRAGConfig(opts *ragConfigOptions) (string, error) {
	cfg := opts.ServiceSpec.Config

	pipelineName := stringConfigField(cfg, "pipeline_name", "default")
	pipelineDesc := stringConfigField(cfg, "pipeline_description", "")
	embeddingProvider := stringConfigField(cfg, "embedding_provider", "")
	embeddingModel := stringConfigField(cfg, "embedding_model", "")
	llmProvider := stringConfigField(cfg, "llm_provider", "")
	llmModel := stringConfigField(cfg, "llm_model", "")
	tokenBudget := intConfigField(cfg, "token_budget", 4000)
	topN := intConfigField(cfg, "top_n", 10)
	ollamaURL := stringConfigField(cfg, "ollama_url", "")

	// Build the tables section
	tables, err := buildRAGTablesYAML(cfg)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("server:\n")
	sb.WriteString("  listen_address: \"0.0.0.0\"\n")
	sb.WriteString("  port: 8080\n")
	sb.WriteString("\n")
	sb.WriteString("defaults:\n")
	sb.WriteString(fmt.Sprintf("  token_budget: %d\n", tokenBudget))
	sb.WriteString(fmt.Sprintf("  top_n: %d\n", topN))
	sb.WriteString("\n")
	sb.WriteString("pipelines:\n")
	sb.WriteString(fmt.Sprintf("  - name: %q\n", pipelineName))
	if pipelineDesc != "" {
		sb.WriteString(fmt.Sprintf("    description: %q\n", pipelineDesc))
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
	sb.WriteString("    rag_llm:\n")
	sb.WriteString(fmt.Sprintf("      provider: %q\n", llmProvider))
	sb.WriteString(fmt.Sprintf("      model: %q\n", llmModel))

	// Include ollama_url in config if provided (Ollama server is internal, not a secret)
	if ollamaURL != "" {
		sb.WriteString(fmt.Sprintf("    ollama_url: %q\n", ollamaURL))
	}

	return sb.String(), nil
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
