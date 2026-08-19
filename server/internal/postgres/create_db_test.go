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
