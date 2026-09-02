package apiv1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
		if !isSensitiveConfigKey(key) {
			t.Errorf("isSensitiveConfigKey(%q) = false, want true", key)
		}
	}

	notSensitive := []string{
		"token_budget", "top_n", "llm_model", "llm_provider",
		"database_name", "host", "port", "table", "vector_column",
		"text_column", "description", "pipeline_name",
	}
	for _, key := range notSensitive {
		if isSensitiveConfigKey(key) {
			t.Errorf("isSensitiveConfigKey(%q) = true, want false", key)
		}
	}
}

func TestRestoreSensitiveConfig(t *testing.T) {
	t.Run("restores a key entirely absent from newConfig (scrub deletes, not blanks)", func(t *testing.T) {
		newConfig := map[string]any{"provider": "voyage", "model": "voyage-3"}
		oldConfig := map[string]any{"provider": "voyage", "model": "voyage-3", "api_key": "sk-old"}
		out := restoreSensitiveConfig(newConfig, oldConfig)
		assert.Equal(t, "sk-old", out["api_key"])
	})

	t.Run("restores a key explicitly submitted as blank", func(t *testing.T) {
		newConfig := map[string]any{"api_key": ""}
		oldConfig := map[string]any{"api_key": "sk-old"}
		out := restoreSensitiveConfig(newConfig, oldConfig)
		assert.Equal(t, "sk-old", out["api_key"])
	})

	t.Run("a newly submitted value is not overwritten", func(t *testing.T) {
		newConfig := map[string]any{"api_key": "sk-new"}
		oldConfig := map[string]any{"api_key": "sk-old"}
		out := restoreSensitiveConfig(newConfig, oldConfig)
		assert.Equal(t, "sk-new", out["api_key"])
	})

	t.Run("no stored value leaves the field absent so validation still requires it", func(t *testing.T) {
		newConfig := map[string]any{"provider": "voyage"}
		oldConfig := map[string]any{"provider": "voyage"}
		out := restoreSensitiveConfig(newConfig, oldConfig)
		_, present := out["api_key"]
		assert.False(t, present)
	})

	t.Run("RAG pipelines nested in an array are matched by name and restored", func(t *testing.T) {
		newConfig := map[string]any{
			"pipelines": []any{
				map[string]any{
					"name": "docs",
					"embedding_llm": map[string]any{
						"provider": "voyage",
						"model":    "voyage-3",
					},
					"rag_llm": map[string]any{
						"provider": "anthropic",
						"model":    "claude-3",
					},
				},
			},
		}
		oldConfig := map[string]any{
			"pipelines": []any{
				map[string]any{
					"name": "docs",
					"embedding_llm": map[string]any{
						"provider": "voyage",
						"model":    "voyage-3",
						"api_key":  "voyage-secret",
					},
					"rag_llm": map[string]any{
						"provider": "anthropic",
						"model":    "claude-3",
						"api_key":  "anthropic-secret",
					},
				},
			},
		}

		out := restoreSensitiveConfig(newConfig, oldConfig)

		pipelines := out["pipelines"].([]any)
		pipeline := pipelines[0].(map[string]any)
		embeddingLLM := pipeline["embedding_llm"].(map[string]any)
		ragLLM := pipeline["rag_llm"].(map[string]any)
		assert.Equal(t, "voyage-secret", embeddingLLM["api_key"])
		assert.Equal(t, "anthropic-secret", ragLLM["api_key"])
	})

	t.Run("a pipeline with no matching old name is left without a key", func(t *testing.T) {
		newConfig := map[string]any{
			"pipelines": []any{
				map[string]any{
					"name": "new-pipeline",
					"embedding_llm": map[string]any{
						"provider": "voyage",
					},
				},
			},
		}
		oldConfig := map[string]any{
			"pipelines": []any{
				map[string]any{
					"name": "docs",
					"embedding_llm": map[string]any{
						"provider": "voyage",
						"api_key":  "voyage-secret",
					},
				},
			},
		}

		out := restoreSensitiveConfig(newConfig, oldConfig)

		pipelines := out["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		_, present := embeddingLLM["api_key"]
		assert.False(t, present)
	})
}

func TestRestoreOmittedServiceSecrets(t *testing.T) {
	t.Run("fills api_key omitted from a RAG service that already exists", func(t *testing.T) {
		newSpec := &api.DatabaseSpec{
			Services: []*api.ServiceSpec{
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
		oldSpec := &database.Spec{
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

		restoreOmittedServiceSecrets(newSpec, oldSpec)

		pipelines := newSpec.Services[0].Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		assert.Equal(t, "voyage-secret", embeddingLLM["api_key"])
	})

	t.Run("a newly added service (no match in oldSpec) is left untouched", func(t *testing.T) {
		newSpec := &api.DatabaseSpec{
			Services: []*api.ServiceSpec{
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
		oldSpec := &database.Spec{Services: []*database.ServiceSpec{}}

		restoreOmittedServiceSecrets(newSpec, oldSpec)

		pipelines := newSpec.Services[0].Config["pipelines"].([]any)
		embeddingLLM := pipelines[0].(map[string]any)["embedding_llm"].(map[string]any)
		_, present := embeddingLLM["api_key"]
		assert.False(t, present)
	})
}

func TestNormalizeConfig(t *testing.T) {
	t.Run("nil becomes empty map", func(t *testing.T) {
		result := normalizeConfig(nil)
		if result == nil {
			t.Fatal("normalizeConfig(nil) returned nil, want empty map")
		}
		if len(result) != 0 {
			t.Errorf("normalizeConfig(nil) returned map with %d entries, want 0", len(result))
		}
	})

	t.Run("non-nil map is returned as-is", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		result := normalizeConfig(input)
		if result["key"] != "value" {
			t.Errorf("normalizeConfig did not preserve existing entries")
		}
	})
}

func TestOrchestratorOptsConversion_Image(t *testing.T) {
	t.Run("Image is mapped from API to domain", func(t *testing.T) {
		apiOpts := &api.OrchestratorOpts{
			Swarm: &api.SwarmOpts{
				Image: utils.PointerTo("ghcr.io/pgedge/pgedge-postgres:custom-image"),
			},
		}
		domain := orchestratorOptsToDatabase(apiOpts)
		require.NotNil(t, domain)
		require.NotNil(t, domain.Swarm)
		assert.Equal(t, "ghcr.io/pgedge/pgedge-postgres:custom-image", domain.Swarm.Image)
	})

	t.Run("Image is mapped from domain to API", func(t *testing.T) {
		domainOpts := &database.OrchestratorOpts{
			Swarm: &database.SwarmOpts{
				Image: "ghcr.io/pgedge/pgedge-postgres:custom-image",
			},
		}
		apiOpts := orchestratorOptsToAPI(domainOpts)
		require.NotNil(t, apiOpts)
		require.NotNil(t, apiOpts.Swarm)
		require.NotNil(t, apiOpts.Swarm.Image)
		assert.Equal(t, "ghcr.io/pgedge/pgedge-postgres:custom-image", *apiOpts.Swarm.Image)
	})

	t.Run("nil Image pointer maps to empty string in domain", func(t *testing.T) {
		apiOpts := &api.OrchestratorOpts{
			Swarm: &api.SwarmOpts{Image: nil},
		}
		domain := orchestratorOptsToDatabase(apiOpts)
		require.NotNil(t, domain)
		assert.Empty(t, domain.Swarm.Image)
	})

	t.Run("empty Image in domain maps to non-nil pointer with empty string", func(t *testing.T) {
		domainOpts := &database.OrchestratorOpts{
			Swarm: &database.SwarmOpts{Image: ""},
		}
		apiOpts := orchestratorOptsToAPI(domainOpts)
		require.NotNil(t, apiOpts)
		require.NotNil(t, apiOpts.Swarm.Image)
		assert.Empty(t, *apiOpts.Swarm.Image)
	})
}
