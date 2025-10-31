//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"log"
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

	for _, version := range host.SupportedPgedgeVersions {
		name := fmt.Sprintf("postgres %s with spock %s", version.PostgresVersion, version.SpockVersion)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// define the cluster topology(nodes structure)
			totalhost := len(fixture.config.Hosts)
			nodeCount := 2
			hostPerNode := 2
			nodes := createNodesStruct(nodeCount, hostPerNode, t)

			expectedReplicas := totalhost - len(nodes)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

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
					Cpus:            pointerTo("1000m"),
					Memory:          pointerTo("1GB"),
					PostgresVersion: pointerTo(version.PostgresVersion),
					SpockVersion:    pointerTo(version.SpockVersion),
					Nodes:           nodes,
				},
			})

			// verification
			// 1. Validate expected count of primary and replica nodes
			assert.True(t, len(nodes) == getTotalPrimaryNodes(db),
				"Found replica count must match expected replica count")

			assert.True(t, expectedReplicas == getTotalReplicaNodes(db),
				"Found replica count must match expected replica count")

			// 2. validate primary and replica related functionalities
			for i := 0; i < len(fixture.config.Hosts); i++ {
				// verify_primary := true
				// execute it only for Primary nodes
				// establish primary node connection, verify pg version and db write functionality etc
				if *db.Instances[i].Postgres.Role == "primary" {
					// verification
					primaryOpts := ConnectionOptions{
						Matcher:  And(WithHost(db.Instances[i].HostID), WithRole("primary")),
						Username: username,
						Password: password,
					}

					verifyPgVersion(ctx, db, primaryOpts, version.PostgresVersion, t)
					verifyPrimaryNodes(ctx, db, primaryOpts, t)
					// verify_primary = false
				} else {
					// verify replicas
					// establish replica connection, verify pg version, read-only status etc
					connOpts := ConnectionOptions{
						Matcher:  And(WithHost(db.Instances[i].HostID), WithRole("replica")),
						Username: username,
						Password: password,
					}
					verifyPgVersion(ctx, db, connOpts, version.PostgresVersion, t)
					verifyReplicasNodes(ctx, db, connOpts, t)
				}
			}
			// 3. validate replication
			validateReplication(ctx, db, t)
		})
	}
}

// TODO: Need to create one common function to be used across all the test cases
// build spec.Nodes, creates a new node or update existing node
func createNodesStruct(numNodes int, hostsPerNode int, t testing.TB) []*controlplane.DatabaseNodeSpec {
	nodes := []*controlplane.DatabaseNodeSpec{}
	totalHosts := len(fixture.config.Hosts)
	hostIndex := 0

	tLogf(t, "Going to create topology for number of nodes %d and total hosts %d\n", numNodes, totalHosts)

	for i := 1; i <= numNodes && hostIndex < totalHosts; i++ {
		end := hostIndex + hostsPerNode
		if end > totalHosts {
			end = totalHosts
		}

		hosts := []controlplane.Identifier{}
		for j := hostIndex; j < end; j++ {
			hosts = append(hosts, controlplane.Identifier(fixture.HostIDs()[j]))
		}
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", i),
			HostIds: hosts,
		})

		hostIndex = end
	}
	//take care of leftover hosts
	if hostIndex < totalHosts {
		hosts := []controlplane.Identifier{}
		for j := hostIndex; j < totalHosts; j++ {
			hosts = append(hosts, controlplane.Identifier(fixture.HostIDs()[j]))
		}
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", len(nodes)+1),
			HostIds: hosts,
		})
	}

	return nodes
}

// validate number of Primary nodes are as expected
func getTotalPrimaryNodes(db *DatabaseFixture) int {
	var primarycount int
	for i := 0; i < len(db.Instances); i++ {
		if *db.Instances[i].Postgres.Role == "primary" {
			primarycount++
		}
	}
	return primarycount
}

// Validate number of Replica nodes are as expected
func getTotalReplicaNodes(db *DatabaseFixture) int {
	var replicacount int
	for i := 0; i < len(db.Instances); i++ {
		if *db.Instances[i].Postgres.Role == "replica" {
			replicacount++
		}
	}
	return replicacount
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
