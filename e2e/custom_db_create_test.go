//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var username string = "admin"
var password string = "password"

// Creates databases with every supported Postgres and Spock version.
// Performs various validations, such as checking primary and replica node functionality.
// Uses DatabaseFixture to create the database.
func TestCreateDbWithVersions(t *testing.T) {
	// The supported versions will be the same across all hosts.
	host, err := fixture.Client.GetHost(t.Context(), &controlplane.GetHostPayload{
		HostID: controlplane.Identifier(fixture.HostIDs()[0]),
	})
	require.NoError(t, err)

	allHostIDs := fixture.HostIDs()

	for _, version := range host.SupportedPgedgeVersions {
		name := fmt.Sprintf("postgres %s with spock %s", version.PostgresVersion, version.SpockVersion)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

			tLog(t, "creating the database")

			// Create a new database fixture that provisions a database based on the given
			// specs and also provides access to useful objects and methods through the
			// fixture struct
			db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
				Spec: &controlplane.DatabaseSpec{
					DatabaseName: "test_db_create",
					DatabaseUsers: []*controlplane.DatabaseUserSpec{
						{
							Username:   username,
							Password:   pointerTo(password),
							DbOwner:    pointerTo(true),
							Attributes: []string{"LOGIN", "SUPERUSER"},
						},
					},
					Port:            pointerTo(0),
					PostgresVersion: pointerTo(version.PostgresVersion),
					SpockVersion:    pointerTo(version.SpockVersion),
					Nodes: []*controlplane.DatabaseNodeSpec{
						{
							Name: "n1",
							HostIds: []controlplane.Identifier{
								controlplane.Identifier(allHostIDs[0]),
								controlplane.Identifier(allHostIDs[1]),
							},
						},
						{
							Name: "n2",
							HostIds: []controlplane.Identifier{
								controlplane.Identifier(allHostIDs[2]),
							},
						},
					},
				},
			})

			tLog(t, "database created successfully")

			primaries := slices.Collect(db.GetInstances(WithRole("primary")))
			replicas := slices.Collect(db.GetInstances(WithRole("replica")))

			// verification
			// 1. Validate expected count of primary and replica nodes
			assert.Len(t, primaries, 2)
			assert.Len(t, replicas, 1)

			// 2. validate primary and replica related functionalities
			for _, instance := range primaries {
				// verification
				primaryOpts := ConnectionOptions{
					Instance: instance,
					Username: username,
					Password: password,
				}

				verifyPgVersion(ctx, db, primaryOpts, version.PostgresVersion, t)
				verifyPrimaryNodes(ctx, db, primaryOpts, t)
			}

			for _, instance := range replicas {
				// verify replicas
				// establish replica connection, verify pg version, read-only status etc
				connOpts := ConnectionOptions{
					Instance: instance,
					Username: username,
					Password: password,
				}
				verifyPgVersion(ctx, db, connOpts, version.PostgresVersion, t)
				verifyReplicasNodes(ctx, db, connOpts, t)
			}

			// 3. validate replication
			validateReplication(ctx, db, t)
		})
	}
}

// Verify primary nodes functionality e.g. not in recovery mode,
// wal_lsn, if write-able etc
func verifyPrimaryNodes(ctx context.Context, db *DatabaseFixture,
	primaryOpts ConnectionOptions, t testing.TB) {
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		// 1. Check recovery mode (must be false for primary)
		var isInRecovery bool
		err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isInRecovery)
		if err != nil {
			require.NoError(t, err)
		}
		if isInRecovery {
			require.NoError(t, err)
		}
		tLog(t, "Node is PRIMARY (pg_is_in_recovery = false)")

		// 2. Check WAL generation
		var walLSN string
		err = conn.QueryRow(ctx, "SELECT pg_current_wal_lsn()").Scan(&walLSN)
		if err != nil {
			require.NoError(t, err)
		}
		tLogf(t, "WAL is being generated, current LSN: %s\n", walLSN)

		// 3. Insert/Update test (write check)
		_, err = conn.Exec(ctx, "CREATE TEMP TABLE IF NOT EXISTS test_table(id SERIAL, val TEXT)")
		if err != nil {
			require.NoError(t, err)
		}
		_, err = conn.Exec(ctx, "INSERT INTO test_table(val) VALUES('test-value')")
		if err != nil {
			require.NoError(t, err)
		}
		tLog(t, "Insert succeeded, primary allows writes")

		// 4. Check replication slots and connected hosts rows info
		rows, err := conn.Query(ctx, "SELECT slot_name, active FROM pg_replication_slots")
		if err != nil {
			require.NoError(t, err)
		}
		defer rows.Close()

		for rows.Next() {
			var slotName string
			var active bool
			if err := rows.Scan(&slotName, &active); err != nil {
				require.NoError(t, err)
			}
			tLogf(t, "   - Slot: %s, Active: %t\n", slotName, active)
		}
	})
}

// Verify replica nodes functionality
func verifyReplicasNodes(ctx context.Context, db *DatabaseFixture,
	primaryOpts ConnectionOptions, t testing.TB) {
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		// 1. Check recovery mode (must be true)
		var isInRecovery bool
		err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isInRecovery)
		if err != nil {
			require.NoError(t, err)
		}
		if !isInRecovery {
			t.Fatalf("Expected a replica, but pg_is_in_recovery=false (this is a primary)")
		}
		tLog(t, "Node is REPLICA (pg_is_in_recovery = true)")

		// 2. Check replication source LSNs
		var receiveLSN, replayLSN string
		err = conn.QueryRow(ctx, "SELECT pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()").
			Scan(&receiveLSN, &replayLSN)
		if err != nil {
			require.NoError(t, err)
		}
		tLogf(t, "WAL received up to: %s, replayed up to: %s\n", receiveLSN, replayLSN)

		// 3. Verify read-only mode (writes should fail)
		_, err = conn.Exec(ctx, "CREATE TEMP TABLE test_table_replica(id SERIAL, val TEXT)")
		if err == nil {
			t.Fatalf("Replica accepted a write")
		}
		tLog(t, "Replica is in read-only mode (writes are not allowed)")
	})
}

// Validate postgresql version
func verifyPgVersion(ctx context.Context, db *DatabaseFixture,
	primaryOpts ConnectionOptions, expectedVersion string, t testing.TB) {
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		var versionStr string
		err := conn.QueryRow(ctx, "SELECT version()").Scan(&versionStr)
		if err != nil {
			log.Fatalf("Failed to fetch PostgreSQL version: %v", err)
		}
		if !strings.Contains(versionStr, expectedVersion) {
			log.Fatalf("Expected PostgreSQL version %s, but got: %s", expectedVersion, versionStr)
		}
		tLogf(t, "PostgreSQL version validation passed (found %s)\n", expectedVersion)
	})
}

// Validate replication scenarios e.g. updates from master is recevied by masters
// and replicas
func validateReplication(ctx context.Context, db *DatabaseFixture, t testing.TB) {

	// get any first available primary
	primaryOpts := ConnectionOptions{
		Matcher:  WithRole("primary"),
		Username: username,
		Password: password,
	}
	// insert table and data
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		// create table and insert values
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 1, "foo")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 2, "bar")
		require.NoError(t, err)
	})

	db.WaitForReplication(ctx, t, username, password)

	// verify replicated data on other hosts
	for i := 0; i < len(db.Instances); i++ {
		// establish conn info
		connOpts := ConnectionOptions{
			Matcher:  WithID(db.Instances[i].ID),
			Username: username,
			Password: password,
		}
		// verify data created at master is available on all hosts
		db.WithConnection(ctx, connOpts, t, func(conn *pgx.Conn) {
			var rowCount int
			err := conn.QueryRow(ctx, "SELECT COUNT(*) FROM foo").Scan(&rowCount)
			require.NoError(t, err)

			// Validate
			expectedCount := 2
			if rowCount != expectedCount {
				t.Fatalf("Expected %d rows in foo, but found %d", expectedCount, rowCount)
			}
			tLogf(t, "Data is successfully replicated on %s", db.Instances[i].HostID)
		})
	}
}
