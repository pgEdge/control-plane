package database_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/require"
)

func TestSanitizeRolesDump(t *testing.T) {
	const pgDumpOutput = `
--
-- PostgreSQL database cluster dump
--

\restrict pkBoMZjYobwxOGivq5KHJz8w62YJgpw1o50PPBHMK3ZjRHr8KgMfZHq44xbPnSM

SET default_transaction_read_only = off;

SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;

--
-- Roles
--

CREATE ROLE admin;
ALTER ROLE admin WITH SUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS PASSWORD 'md580a19f669b02edfbc208a5386ab5036b';
CREATE ROLE app;
ALTER ROLE app WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS PASSWORD 'md598b0eddb1d41e30a28e098217145c424';
CREATE ROLE foo_role;
ALTER ROLE foo_role WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;
CREATE ROLE "foo ""role";
ALTER ROLE "foo ""role" WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;
CREATE ROLE patroni_replicator;
ALTER ROLE patroni_replicator WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN REPLICATION NOBYPASSRLS;
CREATE ROLE pgedge;
ALTER ROLE pgedge WITH SUPERUSER INHERIT CREATEROLE CREATEDB LOGIN REPLICATION BYPASSRLS;
CREATE ROLE pgedge_application;
ALTER ROLE pgedge_application WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;
CREATE ROLE pgedge_application_read_only;
ALTER ROLE pgedge_application_read_only WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;
CREATE ROLE pgedge_superuser;
ALTER ROLE pgedge_superuser WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;

--
-- User Configurations
--


--
-- Role memberships
--

GRANT foo_role TO app WITH INHERIT TRUE GRANTED BY pgedge;
GRANT pg_checkpoint TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_create_subscription TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_maintain TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_monitor TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_read_all_data TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_read_all_settings TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_read_all_stats TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_signal_autovacuum_worker TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_signal_backend TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_stat_scan_tables TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_use_reserved_connections TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;
GRANT pg_write_all_data TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;


--
-- Role privileges on configuration parameters
--

GRANT SET ON PARAMETER commit_delay TO pgedge_superuser;
GRANT SET ON PARAMETER deadlock_timeout TO pgedge_superuser;
GRANT SET ON PARAMETER lc_messages TO pgedge_superuser;
GRANT SET ON PARAMETER log_duration TO pgedge_superuser;
GRANT SET ON PARAMETER log_error_verbosity TO pgedge_superuser;
GRANT SET ON PARAMETER log_executor_stats TO pgedge_superuser;
GRANT SET ON PARAMETER log_lock_waits TO pgedge_superuser;
GRANT SET ON PARAMETER log_min_duration_sample TO pgedge_superuser;
GRANT SET ON PARAMETER log_min_duration_statement TO pgedge_superuser;
GRANT SET ON PARAMETER log_min_error_statement TO pgedge_superuser;
GRANT SET ON PARAMETER log_min_messages TO pgedge_superuser;
GRANT SET ON PARAMETER log_parser_stats TO pgedge_superuser;
GRANT SET ON PARAMETER log_planner_stats TO pgedge_superuser;
GRANT SET ON PARAMETER log_replication_commands TO pgedge_superuser;
GRANT SET ON PARAMETER log_statement TO pgedge_superuser;
GRANT SET ON PARAMETER log_statement_sample_rate TO pgedge_superuser;
GRANT SET ON PARAMETER log_statement_stats TO pgedge_superuser;
GRANT SET ON PARAMETER log_temp_files TO pgedge_superuser;
GRANT SET ON PARAMETER log_transaction_sample_rate TO pgedge_superuser;
GRANT SET ON PARAMETER "pg_stat_statements.track" TO pgedge_superuser;
GRANT SET ON PARAMETER "pg_stat_statements.track_planning" TO pgedge_superuser;
GRANT SET ON PARAMETER "pg_stat_statements.track_utility" TO pgedge_superuser;
GRANT SET ON PARAMETER session_replication_role TO pgedge_superuser;
GRANT SET ON PARAMETER temp_file_limit TO pgedge_superuser;
GRANT SET ON PARAMETER track_activities TO pgedge_superuser;
GRANT SET ON PARAMETER track_counts TO pgedge_superuser;
GRANT SET ON PARAMETER track_functions TO pgedge_superuser;
GRANT SET ON PARAMETER track_io_timing TO pgedge_superuser;


\unrestrict pkBoMZjYobwxOGivq5KHJz8w62YJgpw1o50PPBHMK3ZjRHr8KgMfZHq44xbPnSM

--
-- PostgreSQL database cluster dump complete
--
`

	expectedRoles := []string{
		"admin",
		"app",
		"foo_role",
		`foo "role`,
		"patroni_replicator",
		"pgedge",
		"pgedge_application",
		"pgedge_application_read_only",
		"pgedge_superuser",
	}
	expectedStatements := []string{
		`SET default_transaction_read_only = off;`,
		`SET client_encoding = 'UTF8';`,
		`SET standard_conforming_strings = on;`,
		`ALTER ROLE admin WITH SUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS PASSWORD 'md580a19f669b02edfbc208a5386ab5036b';`,
		`ALTER ROLE app WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS PASSWORD 'md598b0eddb1d41e30a28e098217145c424';`,
		`ALTER ROLE foo_role WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;`,
		`ALTER ROLE "foo ""role" WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;`,
		`ALTER ROLE patroni_replicator WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN REPLICATION NOBYPASSRLS;`,
		`ALTER ROLE pgedge WITH SUPERUSER INHERIT CREATEROLE CREATEDB LOGIN REPLICATION BYPASSRLS;`,
		`ALTER ROLE pgedge_application WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;`,
		`ALTER ROLE pgedge_application_read_only WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;`,
		`ALTER ROLE pgedge_superuser WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOLOGIN NOREPLICATION NOBYPASSRLS;`,
		`GRANT foo_role TO app WITH INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_checkpoint TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_create_subscription TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_maintain TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_monitor TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_read_all_data TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_read_all_settings TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_read_all_stats TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_signal_autovacuum_worker TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_signal_backend TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_stat_scan_tables TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_use_reserved_connections TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT pg_write_all_data TO pgedge_superuser WITH ADMIN OPTION, INHERIT TRUE GRANTED BY pgedge;`,
		`GRANT SET ON PARAMETER commit_delay TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER deadlock_timeout TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER lc_messages TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_duration TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_error_verbosity TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_executor_stats TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_lock_waits TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_min_duration_sample TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_min_duration_statement TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_min_error_statement TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_min_messages TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_parser_stats TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_planner_stats TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_replication_commands TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_statement TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_statement_sample_rate TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_statement_stats TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_temp_files TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER log_transaction_sample_rate TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER "pg_stat_statements.track" TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER "pg_stat_statements.track_planning" TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER "pg_stat_statements.track_utility" TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER session_replication_role TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER temp_file_limit TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER track_activities TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER track_counts TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER track_functions TO pgedge_superuser;`,
		`GRANT SET ON PARAMETER track_io_timing TO pgedge_superuser;`,
	}

	roles, statements := database.SanitizeRolesDump(pgDumpOutput)
	require.Equal(t, expectedRoles, roles)
	require.Equal(t, expectedStatements, statements)
}
