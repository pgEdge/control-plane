package database_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
)

func TestIsSensitiveConfigKey(t *testing.T) {
	sensitive := []string{
		"password", "ro_password", "rw_password",
		"secret", "client_secret",
		"token", "init_token", "auth_token",
		"api_key", "openai_api_key", "anthropic_api_key", "embedding_api_key",
		"apikey", "api-key",
		"credential", "credentials",
		"private_key", "private-key",
		"access_key", "access-key",
		"init_users",
	}
	for _, key := range sensitive {
		assert.True(t, database.IsSensitiveConfigKey(key), "IsSensitiveConfigKey(%q) should be true", key)
	}

	notSensitive := []string{
		"token_budget", "top_n", "llm_model", "llm_provider",
		"database_name", "host", "port", "table", "vector_column",
		"text_column", "description", "pipeline_name",
	}
	for _, key := range notSensitive {
		assert.False(t, database.IsSensitiveConfigKey(key), "IsSensitiveConfigKey(%q) should be false", key)
	}
}

func TestServiceSpec_DefaultOptionalFieldsFrom(t *testing.T) {
	t.Run("fills api_key omitted from an existing RAG pipeline", func(t *testing.T) {
		current := &database.ServiceSpec{
			ServiceID: "rag",
			Config: map[string]any{
				"pipelines": []any{
					map[string]any{
						"name": "docs",
						"embedding_llm": map[string]any{
							"provider": "voyage",
							"model":    "voyage-3",
							"api_key":  "voyage-secret",
						},
					},
				},
			},
		}
		newSvc := &database.ServiceSpec{
			ServiceID: "rag",
			Config: map[string]any{
				"pipelines": []any{
					map[string]any{
						"name": "docs",
						"embedding_llm": map[string]any{
							"provider": "voyage",
							"model":    "voyage-3",
						},
					},
				},
			},
		}

		newSvc.DefaultOptionalFieldsFrom(current)

		pipelines := newSvc.Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		assert.Equal(t, "voyage-secret", embeddingLLM["api_key"])
	})

	t.Run("a newly submitted value is not overwritten", func(t *testing.T) {
		current := &database.ServiceSpec{ServiceID: "rag", Config: map[string]any{"api_key": "sk-old"}}
		newSvc := &database.ServiceSpec{ServiceID: "rag", Config: map[string]any{"api_key": "sk-new"}}

		newSvc.DefaultOptionalFieldsFrom(current)

		assert.Equal(t, "sk-new", newSvc.Config["api_key"])
	})

	t.Run("a pipeline with no matching old name is left without a key", func(t *testing.T) {
		current := &database.ServiceSpec{
			ServiceID: "rag",
			Config: map[string]any{
				"pipelines": []any{
					map[string]any{"name": "docs", "embedding_llm": map[string]any{"provider": "voyage", "api_key": "voyage-secret"}},
				},
			},
		}
		newSvc := &database.ServiceSpec{
			ServiceID: "rag",
			Config: map[string]any{
				"pipelines": []any{
					map[string]any{"name": "new-pipeline", "embedding_llm": map[string]any{"provider": "voyage"}},
				},
			},
		}

		newSvc.DefaultOptionalFieldsFrom(current)

		pipelines := newSvc.Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		_, present := embeddingLLM["api_key"]
		assert.False(t, present)
	})

	t.Run("nil other is a no-op", func(t *testing.T) {
		newSvc := &database.ServiceSpec{ServiceID: "rag", Config: map[string]any{"api_key": ""}}
		assert.NotPanics(t, func() {
			newSvc.DefaultOptionalFieldsFrom(nil)
		})
		assert.Equal(t, "", newSvc.Config["api_key"])
	})
}

func TestSpec_DefaultOptionalFieldsFrom_Services(t *testing.T) {
	t.Run("fills api_key omitted from a RAG service that already exists", func(t *testing.T) {
		current := &database.Spec{
			Services: []*database.ServiceSpec{
				{
					ServiceID:   "rag1",
					ServiceType: "rag",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "docs",
								"embedding_llm": map[string]any{
									"provider": "voyage",
									"model":    "voyage-3",
									"api_key":  "voyage-secret",
								},
							},
						},
					},
				},
			},
		}
		newSpec := &database.Spec{
			Services: []*database.ServiceSpec{
				{
					ServiceID:   "rag1",
					ServiceType: "rag",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "docs",
								"embedding_llm": map[string]any{
									"provider": "voyage",
									"model":    "voyage-3",
								},
							},
						},
					},
				},
			},
		}

		newSpec.DefaultOptionalFieldsFrom(current)

		pipelines := newSpec.Services[0].Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		assert.Equal(t, "voyage-secret", embeddingLLM["api_key"])
	})

	t.Run("a newly added service (no match in current) is left untouched", func(t *testing.T) {
		current := &database.Spec{Services: []*database.ServiceSpec{}}
		newSpec := &database.Spec{
			Services: []*database.ServiceSpec{
				{
					ServiceID:   "rag2",
					ServiceType: "rag",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{"name": "docs", "embedding_llm": map[string]any{"provider": "voyage"}},
						},
					},
				},
			},
		}

		newSpec.DefaultOptionalFieldsFrom(current)

		pipelines := newSpec.Services[0].Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		_, present := embeddingLLM["api_key"]
		assert.False(t, present)
	})
}
