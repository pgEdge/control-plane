package postgres

import (
	"math"
	"strings"
)

// coldFrontDuckDBExtensionDir is where the pgedge-coldfront-duckdb-extensions
// package installs the patched DuckDB iceberg extensions. CP points pg_duckdb at
// it via a GUC rather than a $PGDATA copy. Hardcoding the package path mirrors
// how CP hardcodes the /usr/bin path for the tiering binaries (finding #15).
const coldFrontDuckDBExtensionDir = "/usr/lib/pgedge/coldfront/duckdb-extensions"

// ColdFrontDuckDBGUCs returns the static duckdb.* GUCs a ColdFront data node
// needs (finding #6a). They are boot-time settings: extension_directory is read
// at server start. autoinstall_known_extensions MUST be false — if true, DuckDB
// silently downloads the UNPATCHED upstream iceberg extension, which 409s under
// concurrent cold writes. These values are correctness-critical and set by CP so
// no consumer or image can get them wrong.
func ColdFrontDuckDBGUCs() map[string]any {
	return map[string]any{
		"duckdb.extension_directory":          coldFrontDuckDBExtensionDir,
		"duckdb.allow_unsigned_extensions":    "true",
		"duckdb.autoinstall_known_extensions": "false",
	}
}

// AppendSharedPreloadLibraries appends libraries to a comma-separated
// shared_preload_libraries value, preserving the existing entries and order and
// skipping any that are already present. Dedup keeps reconciles stable: the same
// desired set yields the same string, so an idempotent re-apply does not look
// like a change and does not trigger a restart. Entry whitespace is trimmed.
func AppendSharedPreloadLibraries(current string, add ...string) string {
	var libs []string
	seen := map[string]bool{}
	appendLib := func(lib string) {
		lib = strings.TrimSpace(lib)
		if lib == "" || seen[lib] {
			return
		}
		seen[lib] = true
		libs = append(libs, lib)
	}
	for lib := range strings.SplitSeq(current, ",") {
		appendLib(lib)
	}
	for _, lib := range add {
		appendLib(lib)
	}
	return strings.Join(libs, ",")
}

func DefaultGUCs() map[string]any {
	return map[string]any{
		"archive_command":              "/bin/true",
		"archive_mode":                 "on",
		"checkpoint_completion_target": "0.9",
		"checkpoint_timeout":           "15min",
		"dynamic_shared_memory_type":   "posix",
		"hot_standby_feedback":         "on",
		"log_destination":              "stderr",
		"log_directory":                "log",
		"log_filename":                 "postgresql-%a.log",
		"log_line_prefix":              "%m [%p] ",
		"log_rotation_age":             "1d",
		"log_rotation_size":            "0",
		"log_truncate_on_rotation":     "on",
		"logging_collector":            "on",
		"password_encryption":          "scram-sha-256",
		"shared_preload_libraries":     "pg_stat_statements,snowflake,spock",
		"track_commit_timestamp":       "on",
		"track_io_timing":              "on",
		"wal_level":                    "logical",
		"wal_log_hints":                "on",
		"wal_sender_timeout":           "5s",
	}
}

func SpockDefaultGUCs() map[string]any {
	return map[string]any{
		"spock.enable_ddl_replication":   "on",
		"spock.include_ddl_repset":       "on",
		"spock.allow_ddl_from_functions": "on",
		"spock.conflict_resolution":      "last_update_wins",
		"spock.save_resolutions":         "on",
		"spock.conflict_log_level":       "DEBUG",
	}
}

func SnowflakeLolorGUCs(nodeOrdinal int) map[string]any {
	return map[string]any{
		"snowflake.node": nodeOrdinal,
		"lolor.node":     nodeOrdinal,
	}
}

func DefaultTunableGUCs(memBytes uint64, cpus float64, clusterSize int) map[string]any {
	// Do our calculations in float64 to avoid integer division
	memBytesF := float64(memBytes)
	clusterSizeF := float64(clusterSize)

	// Most of these are based on RDS defaults with some tweaks to incorporate
	// defaults from the pgedge CLI and recommendations from the spock readme.
	return map[string]any{
		"autovacuum_max_workers":       int(max(memBytesF/64371566592, 3)),
		"autovacuum_vacuum_cost_limit": int(max(math.Log2(memBytesF/21474836480)*600, 200)),
		"autovacuum_work_mem":          int64(max(memBytesF/32768, 65536)),         // Units are KB
		"effective_cache_size":         int64(memBytesF / 16384),                   // Units are BLKSZ (default 8KB)
		"maintenance_work_mem":         int64(max(memBytesF*1024/63963136, 65536)), // Units are KB
		"max_connections":              int(min(memBytesF/9531392, 5000)),
		"max_worker_processes":         int(max(cpus*2, clusterSizeF, 12)),
		"max_parallel_workers":         int(max(cpus/2, 8)),
		"shared_buffers":               int64(memBytesF / 32768),    // Units are KB
		"max_wal_senders":              int(max(clusterSize+2, 16)), // +2 to leave room for read replicas
		"max_replication_slots":        int(max(clusterSize+2, 16)),
	}
}
