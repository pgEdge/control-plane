package patroni

import "github.com/pgEdge/control-plane/server/internal/ds"

var dcsGUCs = ds.NewSet(
	"max_connections",
	"max_locks_per_transaction",
	"max_worker_processes",
	"max_prepared_transactions",
	"wal_level",
	"track_commit_timestamp",
)

var dynamicGUCs = ds.NewSet(
	"max_wal_senders",
	"max_replication_slots",
	"wal_keep_segments",
	"wal_keep_size",
	// Reload-safe, kept identical across every instance in the node via
	// DCS rather than each instance's own static config -- see
	// postgres.NativeFailoverSlotGUCs. Set once at config-generation time
	// for every instance regardless of current role, since Patroni can
	// promote any of them to primary later.
	"sync_replication_slots",
	// Never generated as a static default (see NativeFailoverSlotGUCs'
	// doc comment) -- its correct value depends on live replication
	// topology, so it's only ever pushed here directly via the Patroni
	// REST client's PatchDynamicConfig, by InstanceMonitor's runtime
	// reconciliation (see server/internal/monitor/instance_monitor.go).
	// Listed here purely so this file stays the one place documenting
	// every GUC this codebase manages through Patroni's DCS, reload-safe.
	"synchronized_standby_slots",
)

// ExtractPatroniControlledGUCs extracts the GUCs that Patroni controls into a
// separate map that can be used in the DCS parameters. See the important rules
// page for more information:
// https://patroni.readthedocs.io/en/latest/patroni_configuration.html#important-rules
// Something non-obvious in that page, but explained in the main configuration
// page, is that dynamic parameters can also be set in the DCS section.
func ExtractPatroniControlledGUCs(gucs map[string]any) map[string]any {
	dcs := map[string]any{}
	for guc, value := range gucs {
		if dcsGUCs.Has(guc) || dynamicGUCs.Has(guc) {
			dcs[guc] = value
			delete(gucs, guc)
		}
	}
	return dcs
}
