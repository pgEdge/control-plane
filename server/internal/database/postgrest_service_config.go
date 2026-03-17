package database

import (
	"fmt"
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
