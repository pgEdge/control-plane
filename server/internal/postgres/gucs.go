package postgres

import (
	"fmt"
	"math"
	"sort"
	"strings"

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
func needsOutputPluginLibraries(version *ds.PgEdgeVersion) bool {
	if version == nil || version.PostgresVersion == nil {
		return false
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

func DefaultGUCs(version *ds.PgEdgeVersion, peerInstanceIDs []string) map[string]any {
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
	if NeedsNativeFailoverSlotsForVersion(version) {
		gucs["sync_replication_slots"] = "on"
		if slots := synchronizedStandbySlots(peerInstanceIDs); slots != "" {
			gucs["synchronized_standby_slots"] = slots
		}
	}
	return gucs
}

// synchronizedStandbySlots computes this instance's synchronized_standby_slots
// value from its peer instances' IDs, sorted for a stable result.
func synchronizedStandbySlots(peerInstanceIDs []string) string {
	if len(peerInstanceIDs) == 0 {
		return ""
	}
	names := make([]string, len(peerInstanceIDs))
	for i, id := range peerInstanceIDs {
		names[i] = patroniSlotNameFromInstanceID(id)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// patroniSlotNameFromInstanceID reproduces Patroni's own
// slot_name_from_member_name (patroni/dcs/__init__.py): lowercase, "-"/"."
// become "_", anything else invalid becomes "uNNNN" (its ordinal, zero
// padded to 4 digits), truncated to 63 bytes. This has to match exactly,
// character for character, since Patroni names each member's physical
// replication slot from its own member name -- which Control Plane sets to
// the instance's InstanceID (see PatroniConfigGenerator.Generate's Name
// field) -- and this is how synchronized_standby_slots names that same slot
// from the Control Plane side.
func patroniSlotNameFromInstanceID(instanceID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(instanceID) {
		switch {
		case r == '-' || r == '.':
			b.WriteByte('_')
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "u%04d", r)
		}
	}
	s := b.String()
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

// NeedsNativeFailoverSlots reports whether the given Spock and Postgres
// major versions require PG17+'s native logical-slot-failover mechanism:
// replication slots created with failover => true, plus sync_replication_slots
// and synchronized_standby_slots kept in sync (see DefaultGUCs).
//
// The FAILOVER-flag history this needs to account for is more specific
// than "5.x doesn't have it, 6.0 does": 5.0.7 had it on unconditionally,
// 5.0.8-5.0.10 removed it entirely, 5.0.11 brought it back opt-in behind
// spock.use_native_failover_slots (default off), and 6.0.0 made it
// unconditional again with that GUC removed. Deliberately gated on Spock
// major >= 6 only, never on any 5.x minor (including 5.0.11's opt-in
// GUC) — existing 5.x deployments must see zero behavior change from
// this, and Control Plane doesn't manage that opt-in GUC either way.
func NeedsNativeFailoverSlots(spockMajor, pgMajor uint64) bool {
	return spockMajor >= 6 && pgMajor >= 17
}

// NeedsNativeFailoverSlotsForVersion is NeedsNativeFailoverSlots for
// callers that have a declared *ds.PgEdgeVersion on hand (e.g. an
// instance's spec) rather than already-extracted major versions. Returns
// false for a nil version or either major being unresolvable, matching
// how the rest of this file treats an unknown/unparseable version.
func NeedsNativeFailoverSlotsForVersion(version *ds.PgEdgeVersion) bool {
	spockMajor, pgMajor, ok := nativeFailoverSlotMajors(version)
	return ok && NeedsNativeFailoverSlots(spockMajor, pgMajor)
}

func nativeFailoverSlotMajors(version *ds.PgEdgeVersion) (spockMajor, pgMajor uint64, ok bool) {
	if version == nil || version.SpockVersion == nil || version.PostgresVersion == nil {
		return 0, 0, false
	}
	spockMajor, ok = version.SpockVersion.Major()
	if !ok {
		return 0, 0, false
	}
	pgMajor, ok = version.PostgresVersion.MajorMinorVersion().Major()
	if !ok {
		return 0, 0, false
	}
	return spockMajor, pgMajor, true
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
