package postgres

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

func DefaultGUCs() map[string]any {
	return map[string]any{
		"password_encryption":          "md5", // TODO: Can we default to scram-sha-256?
		"dynamic_shared_memory_type":   "posix",
		"wal_level":                    "logical",
		"checkpoint_timeout":           "15min",
		"checkpoint_completion_target": "0.9",
		"archive_mode":                 "on",
		"archive_command":              "/bin/true",
		"wal_sender_timeout":           "5s",
		"track_commit_timestamp":       "on",
		"hot_standby_feedback":         "on",
		"track_io_timing":              "on",
		"shared_preload_libraries":     "pg_stat_statements,snowflake,spock",
	}
}

func Spock4DefaultGUCs() map[string]any {
	return map[string]any{
		"spock.enable_ddl_replication":   "on",
		"spock.include_ddl_repset":       "on",
		"spock.allow_ddl_from_functions": "on",
		"spock.conflict_resolution":      "last_update_wins",
		"spock.save_resolutions":         "on",
		"spock.conflict_log_level":       "DEBUG",
	}
}

func SnowflakeLolorGUCs(nodeName string) (map[string]any, error) {
	re := regexp.MustCompile(`\d+`)
	matches := re.FindStringSubmatch(nodeName)
	if len(matches) == 0 {
		return nil, fmt.Errorf("node name %q does not contain a node number", nodeName)
	}
	suffix := matches[0]
	nodeNum, err := strconv.Atoi(suffix)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node number from %q: %w", suffix, err)
	}
	return map[string]any{
		"snowflake.node": nodeNum,
		"lolor.node":     nodeNum,
	}, nil
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
