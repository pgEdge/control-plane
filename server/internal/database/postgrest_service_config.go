package database

import (
	"bytes"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// PostgRESTServiceConfig is the typed internal representation of PostgREST service
// configuration. Parsed from ServiceSpec.Config map[string]any. All fields are
// optional; defaults are applied when absent.
type PostgRESTServiceConfig struct {
	DBSchemas    string `json:"db_schemas"`    // default: "public"
	DBAnonRole   string `json:"db_anon_role"`  // default: "pgedge_application_read_only"
	DBPool       int    `json:"db_pool"`       // default: 10, range: 1-30
	MaxRows      int    `json:"max_rows"`      // default: 1000, range: 1-10000
	JWTSecret    *string `json:"jwt_secret,omitempty"`
	JWTAud       *string `json:"jwt_aud,omitempty"`
	JWTRoleClaimKey          *string `json:"jwt_role_claim_key,omitempty"`
	ServerCORSAllowedOrigins *string `json:"server_cors_allowed_origins,omitempty"`
}

var postgrestKnownKeys = map[string]bool{
	"db_schemas":                  true,
	"db_anon_role":                true,
	"db_pool":                     true,
	"max_rows":                    true,
	"jwt_secret":                  true,
	"jwt_aud":                     true,
	"jwt_role_claim_key":          true,
	"server_cors_allowed_origins": true,
}

// ParsePostgRESTServiceConfig parses and validates a config map into a typed
// PostgRESTServiceConfig. All fields are optional with sensible defaults.
func ParsePostgRESTServiceConfig(config map[string]any) (*PostgRESTServiceConfig, []error) {
	var errs []error

	errs = append(errs, validatePostgRESTUnknownKeys(config)...)

	// db_schemas — optional string, default "public"
	dbSchemas := "public"
	if v, ok := config["db_schemas"]; ok {
		s, sOk := v.(string)
		if !sOk {
			errs = append(errs, fmt.Errorf("db_schemas must be a string"))
		} else if s == "" {
			errs = append(errs, fmt.Errorf("db_schemas must not be empty"))
		} else {
			dbSchemas = s
		}
	}

	// db_anon_role — optional string, default "pgedge_application_read_only"
	dbAnonRole := "pgedge_application_read_only"
	if v, ok := config["db_anon_role"]; ok {
		s, sOk := v.(string)
		if !sOk {
			errs = append(errs, fmt.Errorf("db_anon_role must be a string"))
		} else if s == "" {
			errs = append(errs, fmt.Errorf("db_anon_role must not be empty"))
		} else {
			dbAnonRole = s
		}
	}

	// db_pool — optional int, default 10, range 1-30
	dbPool := 10
	if _, ok := config["db_pool"]; ok {
		i, iErrs := optionalInt(config, "db_pool")
		errs = append(errs, iErrs...)
		if len(iErrs) == 0 && i != nil {
			if *i < 1 || *i > 30 {
				errs = append(errs, fmt.Errorf("db_pool must be between 1 and 30"))
			} else {
				dbPool = *i
			}
		}
	}

	// max_rows — optional int, default 1000, range 1-10000
	maxRows := 1000
	if _, ok := config["max_rows"]; ok {
		i, iErrs := optionalInt(config, "max_rows")
		errs = append(errs, iErrs...)
		if len(iErrs) == 0 && i != nil {
			if *i < 1 || *i > 10000 {
				errs = append(errs, fmt.Errorf("max_rows must be between 1 and 10000"))
			} else {
				maxRows = *i
			}
		}
	}

	// jwt_secret — optional string, min 32 chars
	jwtSecret, jsErrs := optionalString(config, "jwt_secret")
	errs = append(errs, jsErrs...)
	if jwtSecret != nil && len(*jwtSecret) < 32 {
		errs = append(errs, fmt.Errorf("jwt_secret must be at least 32 characters"))
	}

	jwtAud, jaErrs := optionalString(config, "jwt_aud")
	errs = append(errs, jaErrs...)

	jwtRoleClaimKey, jrErrs := optionalString(config, "jwt_role_claim_key")
	errs = append(errs, jrErrs...)

	corsOrigins, coErrs := optionalString(config, "server_cors_allowed_origins")
	errs = append(errs, coErrs...)

	if len(errs) > 0 {
		return nil, errs
	}

	return &PostgRESTServiceConfig{
		DBSchemas:                dbSchemas,
		DBAnonRole:               dbAnonRole,
		DBPool:                   dbPool,
		MaxRows:                  maxRows,
		JWTSecret:                jwtSecret,
		JWTAud:                   jwtAud,
		JWTRoleClaimKey:          jwtRoleClaimKey,
		ServerCORSAllowedOrigins: corsOrigins,
	}, nil
}

// validatePostgRESTUnknownKeys rejects config keys not in the PostgREST known set.
func validatePostgRESTUnknownKeys(config map[string]any) []error {
	var unknown []string
	for k := range config {
		if !postgrestKnownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	if len(unknown) == 1 {
		return []error{fmt.Errorf("unknown config key %q", unknown[0])}
	}
	quoted := make([]string, len(unknown))
	for i, k := range unknown {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	return []error{fmt.Errorf("unknown config keys: %s", strings.Join(quoted, ", "))}
}

// PostgRESTConnParams holds the connection and credential details needed to
// generate a complete postgrest.conf. These are kept separate from
// PostgRESTServiceConfig because they are runtime-provisioned values (not
// user-supplied configuration).
type PostgRESTConnParams struct {
	Username           string
	Password           string
	DatabaseName       string
	DatabaseHosts      []ServiceHostEntry
	TargetSessionAttrs string
}

// GenerateConf renders a postgrest.conf file from the service config and the
// runtime connection parameters. The db-uri (including credentials) is written
// into the file; no credentials are exposed as environment variables.
func (c *PostgRESTServiceConfig) GenerateConf(conn PostgRESTConnParams) ([]byte, error) {
	uri, err := buildPostgRESTDBURI(conn)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "db-uri = %q\n", uri)
	fmt.Fprintf(&buf, "db-schemas = %q\n", c.DBSchemas)
	fmt.Fprintf(&buf, "db-anon-role = %q\n", c.DBAnonRole)
	fmt.Fprintf(&buf, "db-pool = %d\n", c.DBPool)
	fmt.Fprintf(&buf, "db-max-rows = %d\n", c.MaxRows)

	if c.JWTSecret != nil {
		fmt.Fprintf(&buf, "jwt-secret = %q\n", *c.JWTSecret)
	}
	if c.JWTAud != nil {
		fmt.Fprintf(&buf, "jwt-aud = %q\n", *c.JWTAud)
	}
	if c.JWTRoleClaimKey != nil {
		fmt.Fprintf(&buf, "jwt-role-claim-key = %q\n", *c.JWTRoleClaimKey)
	}
	if c.ServerCORSAllowedOrigins != nil {
		fmt.Fprintf(&buf, "server-cors-allowed-origins = %q\n", *c.ServerCORSAllowedOrigins)
	}

	return buf.Bytes(), nil
}

// buildPostgRESTDBURI constructs a libpq URI with multi-host support.
// Format: postgresql://user:pass@host1:port1,host2:port2/dbname[?target_session_attrs=...]
func buildPostgRESTDBURI(conn PostgRESTConnParams) (string, error) {
	if len(conn.DatabaseHosts) == 0 {
		return "", fmt.Errorf("PostgRESTConnParams.DatabaseHosts is empty")
	}

	userInfo := url.UserPassword(conn.Username, conn.Password)

	hostParts := make([]string, len(conn.DatabaseHosts))
	for i, h := range conn.DatabaseHosts {
		hostParts[i] = fmt.Sprintf("%s:%d", h.Host, h.Port)
	}

	uri := fmt.Sprintf("postgresql://%s@%s/%s",
		userInfo.String(),
		strings.Join(hostParts, ","),
		url.PathEscape(conn.DatabaseName),
	)

	if conn.TargetSessionAttrs != "" {
		uri += "?target_session_attrs=" + url.QueryEscape(conn.TargetSessionAttrs)
	}

	return uri, nil
}
