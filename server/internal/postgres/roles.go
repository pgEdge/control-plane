package postgres

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

var defaultSchemas = []string{"public", "spock", "pg_catalog", "information_schema"}
var builtinRoles = []string{"pgedge_superuser"}

// UserRoleNeedsCreate returns a query that evaluates to true when the named
// role does not yet exist in pg_catalog.pg_roles.
func UserRoleNeedsCreate(name string) Query[bool] {
	return Query[bool]{
		SQL: "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = @name);",
		Args: pgx.NamedArgs{
			"name": name,
		},
	}
}

func CreateRoleIfNotExists(name string) ConditionalStatement {
	return ConditionalStatement{
		If: UserRoleNeedsCreate(name),
		Then: Statement{
			SQL: fmt.Sprintf("CREATE ROLE %s;", QuoteIdentifier(name)),
		},
	}
}

type UserRoleOptions struct {
	Name       string
	Password   string
	Attributes []string
	Roles      []string
}

func CreateUserRole(opts UserRoleOptions) (Statements, error) {
	if slices.Contains(builtinRoles, opts.Name) {
		return nil, fmt.Errorf("role name '%s' conflicts with a builtin role", opts.Name)
	}

	statements := Statements{
		CreateRoleIfNotExists(opts.Name),
	}
	if opts.Password != "" {
		statements = append(statements, Statement{
			// Passwords don't work with pgx.NamedArgs, so we have to escape
			// them manually
			SQL: fmt.Sprintf("ALTER ROLE %s WITH PASSWORD '%s';", QuoteIdentifier(opts.Name), strings.ReplaceAll(opts.Password, "'", "''")),
		})
	}
	for _, attr := range opts.Attributes {
		statements = append(statements, Statement{
			// Attributes can't be quoted, so we're using %s instead of %q
			SQL: fmt.Sprintf("ALTER ROLE %s WITH %s;", QuoteIdentifier(opts.Name), attr),
		})
	}
	for _, role := range opts.Roles {
		statements = append(statements, Statement{
			SQL: fmt.Sprintf("GRANT %s TO %s WITH INHERIT TRUE;", QuoteIdentifier(role), QuoteIdentifier(opts.Name)),
		})
	}

	return statements, nil
}

type BuiltinRoleOptions struct {
	PGVersion string
}

func CreateBuiltInRoles(opts BuiltinRoleOptions) (Statements, error) {
	return CreatePgEdgeSuperuserRole(opts)
}

func CreatePgEdgeSuperuserRole(opts BuiltinRoleOptions) (Statements, error) {
	var roles string
	switch {
	case strings.HasPrefix(opts.PGVersion, "16"):
		roles = pg16PgedgeSuperuserRoles()
	case strings.HasPrefix(opts.PGVersion, "17"):
		roles = pg17PgedgeSuperuserRoles()
	case strings.HasPrefix(opts.PGVersion, "18"):
		roles = pg18PgedgeSuperuserRoles()
	default:
		return nil, fmt.Errorf("no superuser template for PostgreSQL version: %s", opts.PGVersion)
	}

	statements := Statements{
		ConditionalStatement{
			If: Query[bool]{
				SQL: "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'pgedge_superuser');",
			},
			Then: Statement{
				SQL: "CREATE ROLE pgedge_superuser WITH NOLOGIN;",
			},
		},
		Statement{
			SQL: "GRANT SET ON PARAMETER " + superuserParameters() + " TO pgedge_superuser;",
		},
		Statement{
			SQL: "GRANT " + roles + " TO pgedge_superuser WITH ADMIN true;",
		},
	}

	return statements, nil
}

func AlterOwner(dbName, owner string) Statement {
	return Statement{
		SQL: fmt.Sprintf("ALTER DATABASE %s OWNER TO %s;", QuoteIdentifier(dbName), QuoteIdentifier(owner)),
	}
}

type BuiltinRolePrivilegeOptions struct {
	DBName       string
	ExtraSchemas []string
}

func (o BuiltinRolePrivilegeOptions) Schemas() []string {
	var schemas []string
	schemas = append(schemas, defaultSchemas...)
	schemas = append(schemas, o.ExtraSchemas...)
	return schemas
}

func GrantBuiltinRolePrivileges(opts BuiltinRolePrivilegeOptions) Statements {
	statements := Statements{
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO pgedge_superuser;", QuoteIdentifier(opts.DBName)),
		},
		dbConnect(opts.DBName, "pgedge_superuser"),
	}
	for _, schema := range opts.Schemas() {
		statements = append(statements, schemaAdmin(schema, "pgedge_superuser")...)
	}

	return statements
}

func dbConnect(dbName, role string) Statement {
	return Statement{
		SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", QuoteIdentifier(dbName), QuoteIdentifier(role)),
	}
}

func schemaAdmin(schema, role string) Statements {
	schema = QuoteIdentifier(schema)
	role = QuoteIdentifier(role)
	return Statements{
		Statement{
			SQL: fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA %s TO %s;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s TO %s;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %s TO %s;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT ALL PRIVILEGES ON TABLES TO %s;", schema, role),
		},
		Statement{
			SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT ALL PRIVILEGES ON SEQUENCES TO %s;", schema, role),
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

func pg18PgedgeSuperuserRoles() string {
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
		"pg_signal_autovacuum_worker",
	}, ", ")
}
