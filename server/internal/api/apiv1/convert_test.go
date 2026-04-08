package apiv1

import "testing"

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
