package database_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePostgRESTServiceConfig(t *testing.T) {
	t.Run("defaults applied for empty config", func(t *testing.T) {
		cfg, errs := database.ParsePostgRESTServiceConfig(map[string]any{})
		require.Empty(t, errs)
		assert.Equal(t, "public", cfg.DBSchemas)
		assert.Equal(t, "pgedge_application_read_only", cfg.DBAnonRole)
		assert.Equal(t, 10, cfg.DBPool)
		assert.Equal(t, 1000, cfg.MaxRows)
		assert.Nil(t, cfg.JWTSecret)
		assert.Nil(t, cfg.JWTAud)
		assert.Nil(t, cfg.JWTRoleClaimKey)
		assert.Nil(t, cfg.ServerCORSAllowedOrigins)
	})

	t.Run("all fields overridden", func(t *testing.T) {
		cfg, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_schemas":                  "api,public",
			"db_anon_role":                "web_anon",
			"db_pool":                     float64(20),
			"max_rows":                    float64(5000),
			"jwt_secret":                  "a-very-long-secret-that-is-at-least-32-characters",
			"jwt_aud":                     "pgedge-cloud",
			"jwt_role_claim_key":          ".role",
			"server_cors_allowed_origins": "https://app.example.com",
		})
		require.Empty(t, errs)
		assert.Equal(t, "api,public", cfg.DBSchemas)
		assert.Equal(t, "web_anon", cfg.DBAnonRole)
		assert.Equal(t, 20, cfg.DBPool)
		assert.Equal(t, 5000, cfg.MaxRows)
		require.NotNil(t, cfg.JWTSecret)
		assert.Equal(t, "a-very-long-secret-that-is-at-least-32-characters", *cfg.JWTSecret)
		require.NotNil(t, cfg.JWTAud)
		assert.Equal(t, "pgedge-cloud", *cfg.JWTAud)
		require.NotNil(t, cfg.JWTRoleClaimKey)
		assert.Equal(t, ".role", *cfg.JWTRoleClaimKey)
		require.NotNil(t, cfg.ServerCORSAllowedOrigins)
		assert.Equal(t, "https://app.example.com", *cfg.ServerCORSAllowedOrigins)
	})

	t.Run("db_schemas wrong type", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_schemas": 123,
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_schemas must be a string")
	})

	t.Run("db_schemas empty string", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_schemas": "",
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_schemas must not be empty")
	})

	t.Run("db_anon_role wrong type", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_anon_role": true,
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_anon_role must be a string")
	})

	t.Run("db_anon_role empty string", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_anon_role": "",
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_anon_role must not be empty")
	})

	t.Run("db_pool below range", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": float64(0),
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_pool must be between 1 and 30")
	})

	t.Run("db_pool above range", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": float64(31),
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_pool must be between 1 and 30")
	})

	t.Run("db_pool wrong type", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": "ten",
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_pool must be an integer")
	})

	t.Run("db_pool non-integer float", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": float64(5.5),
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "db_pool must be an integer")
	})

	t.Run("db_pool boundary values", func(t *testing.T) {
		cfg, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": float64(1),
		})
		require.Empty(t, errs)
		assert.Equal(t, 1, cfg.DBPool)

		cfg, errs = database.ParsePostgRESTServiceConfig(map[string]any{
			"db_pool": float64(30),
		})
		require.Empty(t, errs)
		assert.Equal(t, 30, cfg.DBPool)
	})

	t.Run("max_rows below range", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"max_rows": float64(0),
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "max_rows must be between 1 and 10000")
	})

	t.Run("max_rows above range", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"max_rows": float64(10001),
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "max_rows must be between 1 and 10000")
	})

	t.Run("max_rows boundary values", func(t *testing.T) {
		cfg, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"max_rows": float64(1),
		})
		require.Empty(t, errs)
		assert.Equal(t, 1, cfg.MaxRows)

		cfg, errs = database.ParsePostgRESTServiceConfig(map[string]any{
			"max_rows": float64(10000),
		})
		require.Empty(t, errs)
		assert.Equal(t, 10000, cfg.MaxRows)
	})

	t.Run("jwt_secret too short", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"jwt_secret": "short",
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "jwt_secret must be at least 32 characters")
	})

	t.Run("jwt_secret exactly 32 chars", func(t *testing.T) {
		cfg, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"jwt_secret": "12345678901234567890123456789012",
		})
		require.Empty(t, errs)
		require.NotNil(t, cfg.JWTSecret)
		assert.Equal(t, "12345678901234567890123456789012", *cfg.JWTSecret)
	})

	t.Run("jwt_secret wrong type", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"jwt_secret": 12345,
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "jwt_secret must be a string")
	})

	t.Run("unknown config key", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"unknown_key": "value",
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), `unknown config key "unknown_key"`)
	})

	t.Run("multiple unknown config keys", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"foo": "bar",
			"baz": 123,
		})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "unknown config keys:")
		assert.Contains(t, errs[0].Error(), `"baz"`)
		assert.Contains(t, errs[0].Error(), `"foo"`)
	})

	t.Run("multiple errors collected", func(t *testing.T) {
		_, errs := database.ParsePostgRESTServiceConfig(map[string]any{
			"db_schemas": "",
			"db_pool":    float64(0),
			"max_rows":   "not-a-number",
		})
		require.Len(t, errs, 3)
	})
}
