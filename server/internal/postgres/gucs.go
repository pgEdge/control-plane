package postgres

import (
	"math"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

// minOutputPluginLibrariesVersions are the earliest Postgres minor version per
// major version that gates output plugins behind the output_plugin_libraries
// allowlist GUC. Any major version newer than the last entry here is assumed
// to require it as well. See SPOC-651 / PLAT-721.
var minOutputPluginLibrariesVersions = map[uint64]*ds.Version{
	16: ds.MustParseVersion("16.15"),
	17: ds.MustParseVersion("17.11"),
	18: ds.MustParseVersion("18.6"),
}

// needsOutputPluginLibraries reports whether the given Postgres version
// requires output_plugin_libraries to be set in order to allow spock_output
// to create replication slots.
//
// Spock 6 manifest entries deliberately point at a floating/mutable image
// tag (see version-manifest.json) rather than a pinned build number, so its
// declared postgres_version can drift out of sync with whatever Postgres
// minor the tag actually resolves to at any given moment. That drift caused
// a real, live-reproduced failure ("library spock_output may not be used as
// an output plugin") when the floating tag moved past the gate threshold for
// a minor this function had no way to know about ahead of time, since it
// only ever sees the declared version, not what's actually running.
//
// Rather than track the floating tag's exact current resolution, Spock
// major >= 6 is treated as an unconditional yes here. This is a deliberate
// trade-off, not a fully general fix: every Spock 6 build seen in practice
// so far ships on a Postgres minor at or past the relevant gate, so this
// resolves the real failure above; it has not been verified whether setting
// this GUC against a hypothetical Postgres minor that predates the gate
// (and so may not recognize the parameter at all) is itself harmless or
// would fail Postgres startup. If Spock 6 is ever built against such a
// minor, this assumption needs revisiting. Spock 5.x behavior is unchanged
// — it still depends solely on the exact Postgres minor check below.
func needsOutputPluginLibraries(version *ds.PgEdgeVersion) bool {
	if version == nil || version.PostgresVersion == nil {
		return false
	}
	if version.SpockVersion != nil {
		if spockMajor, ok := version.SpockVersion.Major(); ok && spockMajor >= 6 {
			return true
		}
	}
	pgVersion := version.PostgresVersion.MajorMinorVersion()
	major, ok := pgVersion.Major()
	if !ok {
		return false
	}
	minVersion, ok := minOutputPluginLibrariesVersions[major]
	if !ok {
		// Newer majors than we know about are assumed to need it; older
		// majors than we know about never had the gate.
		return major > 18
	}
	return pgVersion.Compare(minVersion) >= 0
}

func DefaultGUCs(version *ds.PgEdgeVersion) map[string]any {
	gucs := map[string]any{
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
	if needsOutputPluginLibraries(version) {
		gucs["output_plugin_libraries"] = "pgoutput, test_decoding, spock_output"
	}
	return gucs
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
