package swarm

import (
	"bytes"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// PostgRESTConfigParams holds all inputs needed to generate a postgrest.conf file.
type PostgRESTConfigParams struct {
	Config *database.PostgRESTServiceConfig
}

// GeneratePostgRESTConfig generates the postgrest.conf file content.
// Credentials are not written here; they are injected as libpq env vars at the container level.
func GeneratePostgRESTConfig(params *PostgRESTConfigParams) ([]byte, error) {
	if params == nil {
		return nil, fmt.Errorf("GeneratePostgRESTConfig: params must not be nil")
	}
	if params.Config == nil {
		return nil, fmt.Errorf("GeneratePostgRESTConfig: params.Config must not be nil")
	}
	cfg := params.Config

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "db-schemas = %q\n", cfg.DBSchemas)
	fmt.Fprintf(&buf, "db-anon-role = %q\n", cfg.DBAnonRole)
	fmt.Fprintf(&buf, "db-pool = %d\n", cfg.DBPool)
	fmt.Fprintf(&buf, "db-max-rows = %d\n", cfg.MaxRows)

	if cfg.JWTSecret != nil {
		fmt.Fprintf(&buf, "jwt-secret = %q\n", *cfg.JWTSecret)
	}
	if cfg.JWTAud != nil {
		fmt.Fprintf(&buf, "jwt-aud = %q\n", *cfg.JWTAud)
	}
	if cfg.JWTRoleClaimKey != nil {
		fmt.Fprintf(&buf, "jwt-role-claim-key = %q\n", *cfg.JWTRoleClaimKey)
	}
	if cfg.ServerCORSAllowedOrigins != nil {
		fmt.Fprintf(&buf, "server-cors-allowed-origins = %q\n", *cfg.ServerCORSAllowedOrigins)
	}

	return buf.Bytes(), nil
}
