package database_test

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseConf parses key=value lines from a postgrest.conf into a map.
// Surrounding quotes are stripped from string values.
func parseConf(t *testing.T, data []byte) map[string]string {
	t.Helper()
	m := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " = ", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected line in postgrest.conf: %q", line)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
		}
		m[key] = val
	}
	return m
}

func makeTestConn() database.PostgRESTConnParams {
	return database.PostgRESTConnParams{
		Username:      "svc_pgrest",
		Password:      "testpass",
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "pg-host1", Port: 5432}},
	}
}

func TestGenerateConf_CoreFields(t *testing.T) {
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "pgedge_application_read_only",
		DBPool:     10,
		MaxRows:    1000,
	}
	data, err := cfg.GenerateConf(makeTestConn())
	require.NoError(t, err)
	m := parseConf(t, data)
	assert.Equal(t, "public", m["db-schemas"])
	assert.Equal(t, "pgedge_application_read_only", m["db-anon-role"])
	assert.Equal(t, "10", m["db-pool"])
	assert.Equal(t, "1000", m["db-max-rows"])
}

func TestGenerateConf_CustomCoreFields(t *testing.T) {
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "api,private",
		DBAnonRole: "web_anon",
		DBPool:     5,
		MaxRows:    500,
	}
	data, err := cfg.GenerateConf(makeTestConn())
	require.NoError(t, err)
	m := parseConf(t, data)
	assert.Equal(t, "api,private", m["db-schemas"])
	assert.Equal(t, "web_anon", m["db-anon-role"])
	assert.Equal(t, "5", m["db-pool"])
	assert.Equal(t, "500", m["db-max-rows"])
}

func TestGenerateConf_JWTFieldsAbsent(t *testing.T) {
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     10,
		MaxRows:    1000,
	}
	data, err := cfg.GenerateConf(makeTestConn())
	require.NoError(t, err)
	m := parseConf(t, data)
	for _, key := range []string{"jwt-secret", "jwt-aud", "jwt-role-claim-key", "server-cors-allowed-origins"} {
		assert.NotContains(t, m, key, "%s should be absent when not configured", key)
	}
}

func TestGenerateConf_AllJWTFields(t *testing.T) {
	secret := "a-very-long-jwt-secret-that-is-at-least-32-chars"
	aud := "my-api-audience"
	roleClaimKey := ".role"
	corsOrigins := "https://example.com"
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:                "public",
		DBAnonRole:               "web_anon",
		DBPool:                   10,
		MaxRows:                  1000,
		JWTSecret:                &secret,
		JWTAud:                   &aud,
		JWTRoleClaimKey:          &roleClaimKey,
		ServerCORSAllowedOrigins: &corsOrigins,
	}
	data, err := cfg.GenerateConf(makeTestConn())
	require.NoError(t, err)
	m := parseConf(t, data)
	assert.Equal(t, secret, m["jwt-secret"])
	assert.Equal(t, aud, m["jwt-aud"])
	assert.Equal(t, roleClaimKey, m["jwt-role-claim-key"])
	assert.Equal(t, corsOrigins, m["server-cors-allowed-origins"])
}

func TestGenerateConf_DBURIContainsCredentials(t *testing.T) {
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     10,
		MaxRows:    1000,
	}
	conn := database.PostgRESTConnParams{
		Username:      "svc_pgrest",
		Password:      "s3cr3t",
		DatabaseName:  "mydb",
		DatabaseHosts: []database.ServiceHostEntry{{Host: "pg-host1", Port: 5432}},
	}
	data, err := cfg.GenerateConf(conn)
	require.NoError(t, err)
	m := parseConf(t, data)
	uri, ok := m["db-uri"]
	require.True(t, ok, "db-uri must be present in postgrest.conf")
	assert.Contains(t, uri, "svc_pgrest")
	assert.Contains(t, uri, "s3cr3t")
	assert.Contains(t, uri, "pg-host1")
	assert.Contains(t, uri, "mydb")
}

func TestGenerateConf_DBURIMultiHost(t *testing.T) {
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     10,
		MaxRows:    1000,
	}
	conn := database.PostgRESTConnParams{
		Username:     "svc_pgrest",
		Password:     "pass",
		DatabaseName: "mydb",
		DatabaseHosts: []database.ServiceHostEntry{
			{Host: "pg-host1", Port: 5432},
			{Host: "pg-host2", Port: 5432},
		},
		TargetSessionAttrs: "read-write",
	}
	data, err := cfg.GenerateConf(conn)
	require.NoError(t, err)
	m := parseConf(t, data)
	uri := m["db-uri"]
	assert.Contains(t, uri, "pg-host1")
	assert.Contains(t, uri, "pg-host2")
	assert.Contains(t, uri, "target_session_attrs")
}

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
