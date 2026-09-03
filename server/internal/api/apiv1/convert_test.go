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
