package postgres

import (
	"fmt"
	"slices"
	"strings"
)

var defaultSchemas = []string{"public", "spock", "pg_catalog", "information_schema"}
var builtinRoles = []string{"pgedge_application", "pgedge_application_read_only", "pgedge_superuser"}

type UserRoleOptions struct {
	Name       string
	Password   string
	DBName     string
	DBOwner    bool
	Attributes []string
	Roles      []string
}

func CreateUserRole(opts UserRoleOptions) (Statements, error) {
	if slices.Contains(builtinRoles, opts.Name) {
		return nil, fmt.Errorf("role name %q conflicts with a builtin role", opts.Name)
	}

	statements := Statements{
		Statement{
			SQL: fmt.Sprintf("CREATE ROLE %q", opts.Name),
		},
	}
	if opts.Password != "" {
		statements = append(statements, Statement{
			// Passwords don't work with pgx.NamedArgs, so we have to escape
			// them manually
			SQL: fmt.Sprintf("ALTER ROLE %q WITH PASSWORD '%s';", opts.Name, strings.ReplaceAll(opts.Password, "'", "''")),
		})
	}
	for _, attr := range opts.Attributes {
		statements = append(statements, Statement{
			// Attributes can't be quoted, so we're using %s instead of %q
			SQL: fmt.Sprintf("ALTER ROLE %q WITH %s;", opts.Name, attr),
		})
	}
	if opts.DBOwner {
		statements = append(statements, Statement{
			SQL: fmt.Sprintf("ALTER DATABASE %q OWNER TO %q;", opts.DBName, opts.Name),
		})
	}
	for _, role := range opts.Roles {
		statements = append(statements, Statement{
			SQL: fmt.Sprintf("GRANT %q TO %q WITH INHERIT TRUE;", role, opts.Name),
		})
	}

	return statements, nil
}

type BuiltinRoleOptions struct {
	PGVersion    int
	DBName       string
	ExtraSchemas []string
}

func (o BuiltinRoleOptions) Schemas() []string {
	var schemas []string
	schemas = append(schemas, defaultSchemas...)
	schemas = append(schemas, o.ExtraSchemas...)
	return schemas
}

func CreateBuiltInRoles(opts BuiltinRoleOptions) (Statements, error) {
	statements, err := CreatePgEdgeSuperuserRole(opts)
	if err != nil {
		return nil, err
	}
	statements = append(statements, CreateApplicationReadOnlyRole(opts)...)
	statements = append(statements, CreateApplicationRole(opts)...)
	return statements, nil
}

func CreateApplicationRole(opts BuiltinRoleOptions) Statements {
	statements := Statements{
		Statement{
			SQL: "CREATE ROLE pgedge_application WITH NOLOGIN;",
		},
		dbConnect(opts.DBName, "pgedge_application"),
	}

	for _, schema := range opts.Schemas() {
		statements = append(statements, schemaAdmin(schema, "pgedge_application")...)
	}

	return statements
}

func CreateApplicationReadOnlyRole(opts BuiltinRoleOptions) Statements {
	statements := Statements{
		Statement{
			SQL: "CREATE ROLE pgedge_application_read_only WITH NOLOGIN;",
		},
		dbConnect(opts.DBName, "pgedge_application_read_only"),
	}

	for _, schema := range opts.Schemas() {
		statements = append(statements, schemaReadOnly(schema, "pgedge_application_read_only")...)
	}

	return statements
}

func CreatePgEdgeSuperuserRole(opts BuiltinRoleOptions) (Statements, error) {
	var roles string
	switch opts.PGVersion {
	case 15:
		roles = pg15PgedgeSuperuserRoles()
	case 16:
		roles = pg16PgedgeSuperuserRoles()
	case 17:
		roles = pg17PgedgeSuperuserRoles()
	default:
		return nil, fmt.Errorf("no superuser template for PostgreSQL version: %d", opts.PGVersion)
	}

	statements := Statements{
		Statement{
			SQL: "CREATE ROLE pgedge_superuser WITH NOLOGIN;",
		},
		Statement{
			SQL: "GRANT SET ON PARAMETER " + superuserParameters() + " TO pgedge_superuser;",
		},
		Statement{
			SQL: "GRANT " + roles + " TO pgedge_superuser WITH ADMIN true;",
		},
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %q TO pgedge_superuser;", opts.DBName),
		},
	}

	for _, schema := range opts.Schemas() {
		statements = append(statements, schemaAdmin(schema, "pgedge_superuser")...)
	}

	return statements, nil
}

func dbConnect(dbName, role string) Statement {
	return Statement{
		SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %q TO %q;", dbName, role),
	}
}

func schemaAdmin(schema, role string) Statements {
	return Statements{
		Statement{
			SQL: fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA %q TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA %q TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %q TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %q GRANT ALL PRIVILEGES ON TABLES TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %q GRANT ALL PRIVILEGES ON SEQUENCES TO %q;", schema, role),
		},
	}
}

func schemaReadOnly(schema, role string) Statements {
	return Statements{
		Statement{
			SQL: fmt.Sprintf("GRANT USAGE ON SCHEMA %q TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %q TO %q;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %q GRANT SELECT ON TABLES TO %q;", schema, role),
		},
	}
}

func superuserParameters() string {
	return strings.Join([]string{
		"commit_delay",
		"deadlock_timeout",
		"lc_messages",
		"log_duration",
		"log_error_verbosity",
		"log_executor_stats",
		"log_lock_waits",
		"log_min_duration_sample",
		"log_min_duration_statement",
		"log_min_error_statement",
		"log_min_messages",
		"log_parser_stats",
		"log_planner_stats",
		"log_replication_commands",
		"log_statement",
		"log_statement_sample_rate",
		"log_statement_stats",
		"log_temp_files",
		"log_transaction_sample_rate",
		"pg_stat_statements.track",
		"pg_stat_statements.track_planning",
		"pg_stat_statements.track_utility",
		"session_replication_role",
		"temp_file_limit",
		"track_activities",
		"track_counts",
		"track_functions",
		"track_io_timing",
	}, ", ")
}

func pg15PgedgeSuperuserRoles() string {
	return strings.Join([]string{
		"pg_read_all_data",
		"pg_write_all_data",
		"pg_read_all_settings",
		"pg_read_all_stats",
		"pg_stat_scan_tables",
		"pg_monitor",
		"pg_signal_backend",
		"pg_checkpoint",
	}, ", ")
}

func pg16PgedgeSuperuserRoles() string {
	return strings.Join([]string{
		"pg_read_all_data",
		"pg_write_all_data",
		"pg_read_all_settings",
		"pg_read_all_stats",
		"pg_stat_scan_tables",
		"pg_monitor",
		"pg_signal_backend",
		"pg_checkpoint",
		"pg_use_reserved_connections",
		"pg_create_subscription",
	}, ", ")
}

func pg17PgedgeSuperuserRoles() string {
	return strings.Join([]string{
		"pg_read_all_data",
		"pg_write_all_data",
		"pg_read_all_settings",
		"pg_read_all_stats",
		"pg_stat_scan_tables",
		"pg_monitor",
		"pg_signal_backend",
		"pg_checkpoint",
		"pg_use_reserved_connections",
		"pg_create_subscription",
		"pg_maintain",
	}, ", ")
}
