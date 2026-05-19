package postgres

import (
	"math"
)

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
		"password_encryption":          "md5", // TODO: Can we default to scram-sha-256?
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
