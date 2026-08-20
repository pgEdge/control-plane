package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestSyncEvent(t *testing.T) {
	for _, tc := range []struct {
		name          string
		transactional bool
		expectedSQL   string
	}{
		{
			name:          "transactional",
			transactional: true,
			expectedSQL:   "SELECT spock.sync_event(true);",
		},
		{
			name:          "not transactional",
			transactional: false,
			expectedSQL:   "SELECT spock.sync_event();",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := postgres.SyncEvent(tc.transactional)
			assert.Equal(t, tc.expectedSQL, query.SQL)
		})
	}
}

func TestSpockProgressReachedLSN(t *testing.T) {
	for _, tc := range []struct {
		name           string
		spockMajor     uint64
		expectedColumn string
	}{
		{
			name:           "spock 5",
			spockMajor:     5,
			expectedColumn: "p.remote_lsn",
		},
		{
			name:           "spock 6",
			spockMajor:     6,
			expectedColumn: "p.remote_commit_lsn",
		},
		{
			name:           "spock 7 (future major, treated like 6)",
			spockMajor:     7,
			expectedColumn: "p.remote_commit_lsn",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := postgres.SpockProgressReachedLSN(tc.spockMajor, "n1", "0/0")
			assert.Contains(t, query.SQL, tc.expectedColumn)
			assert.Equal(t, pgx.NamedArgs{
				"peer_node_name": "n1",
				"target_lsn":     "0/0",
			}, query.Args)
		})
	}
}

func TestWaitForSyncEvent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		waitIfDisabled bool
		expectedSQL    string
	}{
		{
			name:           "wait if disabled",
			waitIfDisabled: true,
			expectedSQL:    "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout, true);",
		},
		{
			name:           "do not wait if disabled",
			waitIfDisabled: false,
			expectedSQL:    "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout);",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := postgres.WaitForSyncEvent("n1", "0/0", 30, tc.waitIfDisabled)
			assert.Equal(t, tc.expectedSQL, query.SQL)
			assert.Equal(t, pgx.NamedArgs{
				"origin_node": "n1",
				"lsn":         "0/0",
				"timeout":     30,
			}, query.Args)
		})
	}
}

func TestCreateReplicationSlot(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failover    bool
		expectedSQL string
	}{
		{
			name:     "failover",
			failover: true,
			expectedSQL: "SELECT pg_create_logical_replication_slot(" +
				"spock.spock_gen_slot_name(@slot_dbname, @slot_provider_node, @slot_sub_name), " +
				"'spock_output', false, false, true);",
		},
		{
			name:     "no failover",
			failover: false,
			expectedSQL: "SELECT pg_create_logical_replication_slot(" +
				"spock.spock_gen_slot_name(@slot_dbname, @slot_provider_node, @slot_sub_name), " +
				"'spock_output');",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slot := postgres.CreateReplicationSlot("db", "n1", "n2", tc.failover)
			then, ok := slot.Then.(postgres.Statement)
			if !ok {
				t.Fatalf("expected slot.Then to be a postgres.Statement, got %T", slot.Then)
			}
			assert.Equal(t, tc.expectedSQL, then.SQL)
		})
	}
}

func TestPhysicalReplicationSlotNames(t *testing.T) {
	query := postgres.PhysicalReplicationSlotNames()
	assert.Contains(t, query.SQL, "slot_type = 'physical'")
	assert.Contains(t, query.SQL, "NOT temporary",
		"must exclude temporary slots -- they vanish with their creating session and shouldn't be depended on")
}
