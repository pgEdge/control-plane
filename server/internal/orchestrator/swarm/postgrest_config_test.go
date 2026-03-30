package swarm

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// parseConf parses the key=value lines from a postgrest.conf into a map.
// String values are returned unquoted; numeric values are returned as-is.
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
		// Strip surrounding quotes from string values.
		if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
		}
		m[key] = val
	}
	return m
}

func TestGeneratePostgRESTConfig_Defaults(t *testing.T) {
	params := &PostgRESTConfigParams{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:  "public",
			DBAnonRole: "pgedge_application_read_only",
			DBPool:     10,
			MaxRows:    1000,
		},
	}

	data, err := GeneratePostgRESTConfig(params)
	if err != nil {
		t.Fatalf("GeneratePostgRESTConfig() error = %v", err)
	}

	m := parseConf(t, data)

	if m["db-schemas"] != "public" {
		t.Errorf("db-schemas = %q, want %q", m["db-schemas"], "public")
	}
	if m["db-anon-role"] != "pgedge_application_read_only" {
		t.Errorf("db-anon-role = %q, want %q", m["db-anon-role"], "pgedge_application_read_only")
	}
	if m["db-pool"] != "10" {
		t.Errorf("db-pool = %q, want %q", m["db-pool"], "10")
	}
	if m["db-max-rows"] != "1000" {
		t.Errorf("db-max-rows = %q, want %q", m["db-max-rows"], "1000")
	}
}

func TestGeneratePostgRESTConfig_CustomCoreFields(t *testing.T) {
	params := &PostgRESTConfigParams{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:  "api,private",
			DBAnonRole: "web_anon",
			DBPool:     5,
			MaxRows:    500,
		},
	}

	data, err := GeneratePostgRESTConfig(params)
	if err != nil {
		t.Fatalf("GeneratePostgRESTConfig() error = %v", err)
	}

	m := parseConf(t, data)

	if m["db-schemas"] != "api,private" {
		t.Errorf("db-schemas = %q, want %q", m["db-schemas"], "api,private")
	}
	if m["db-anon-role"] != "web_anon" {
		t.Errorf("db-anon-role = %q, want %q", m["db-anon-role"], "web_anon")
	}
	if m["db-pool"] != "5" {
		t.Errorf("db-pool = %q, want %q", m["db-pool"], "5")
	}
	if m["db-max-rows"] != "500" {
		t.Errorf("db-max-rows = %q, want %q", m["db-max-rows"], "500")
	}
}

func TestGeneratePostgRESTConfig_JWTFieldsAbsent(t *testing.T) {
	// No JWT fields set — none should appear in the config file.
	params := &PostgRESTConfigParams{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:  "public",
			DBAnonRole: "web_anon",
			DBPool:     10,
			MaxRows:    1000,
		},
	}

	data, err := GeneratePostgRESTConfig(params)
	if err != nil {
		t.Fatalf("GeneratePostgRESTConfig() error = %v", err)
	}

	m := parseConf(t, data)

	for _, key := range []string{"jwt-secret", "jwt-aud", "jwt-role-claim-key", "server-cors-allowed-origins"} {
		if _, ok := m[key]; ok {
			t.Errorf("%s should be absent when not configured, but it was present", key)
		}
	}
}

func TestGeneratePostgRESTConfig_AllJWTFields(t *testing.T) {
	secret := "a-very-long-jwt-secret-that-is-at-least-32-chars"
	aud := "my-api-audience"
	roleClaimKey := ".role"
	corsOrigins := "https://example.com"

	params := &PostgRESTConfigParams{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:                "public",
			DBAnonRole:               "web_anon",
			DBPool:                   10,
			MaxRows:                  1000,
			JWTSecret:                &secret,
			JWTAud:                   &aud,
			JWTRoleClaimKey:          &roleClaimKey,
			ServerCORSAllowedOrigins: &corsOrigins,
		},
	}

	data, err := GeneratePostgRESTConfig(params)
	if err != nil {
		t.Fatalf("GeneratePostgRESTConfig() error = %v", err)
	}

	m := parseConf(t, data)

	if m["jwt-secret"] != secret {
		t.Errorf("jwt-secret = %q, want %q", m["jwt-secret"], secret)
	}
	if m["jwt-aud"] != aud {
		t.Errorf("jwt-aud = %q, want %q", m["jwt-aud"], aud)
	}
	if m["jwt-role-claim-key"] != roleClaimKey {
		t.Errorf("jwt-role-claim-key = %q, want %q", m["jwt-role-claim-key"], roleClaimKey)
	}
	if m["server-cors-allowed-origins"] != corsOrigins {
		t.Errorf("server-cors-allowed-origins = %q, want %q", m["server-cors-allowed-origins"], corsOrigins)
	}
}

func TestGeneratePostgRESTConfig_CredentialsNotInFile(t *testing.T) {
	// Verify that no credential-like keys ever appear in the config file.
	secret := "a-very-long-jwt-secret-that-is-at-least-32-chars"
	params := &PostgRESTConfigParams{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:  "public",
			DBAnonRole: "web_anon",
			DBPool:     10,
			MaxRows:    1000,
			JWTSecret:  &secret,
		},
	}

	data, err := GeneratePostgRESTConfig(params)
	if err != nil {
		t.Fatalf("GeneratePostgRESTConfig() error = %v", err)
	}

	// None of the libpq / db-uri credential keys should appear.
	for _, forbidden := range []string{"db-uri", "PGUSER", "PGPASSWORD", "PGHOST", "PGPORT", "PGDATABASE"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("config file must not contain %q (credentials are env vars)", forbidden)
		}
	}
}
