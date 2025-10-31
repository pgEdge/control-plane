//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

// TestWholeCluster deploys one instance to each host in the cluster.
func TestWholeCluster(t *testing.T) {
	username := "admin"
	password := "password"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Logf("creating database")

	hostIDs := fixture.HostIDs()
	nodes := []*controlplane.DatabaseNodeSpec{
		{Name: "n1"},
		{Name: "n2"},
		{Name: "n3"},
	}
	for i, hostID := range hostIDs {
		nodeIdx := i % len(nodes)
		nodes[nodeIdx].HostIds = append(nodes[nodeIdx].HostIds, controlplane.Identifier(hostID))
	}
	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test",
			Port:         pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: nodes,
		},
	})

	t.Logf("validating replication")

	// Validate replication by writing data to one node and check for that data
	// on all other nodes.
	for _, write := range nodes {
		t.Logf("creating table on node %s", write.Name)

		writeOpts := ConnectionOptions{
			Matcher:  And(WithNode(write.Name), WithRole("primary")),
			Username: username,
			Password: password,
		}

		var syncLSN string

		db.WithConnection(ctx, writeOpts, t, func(conn *pgx.Conn) {
			_, err := conn.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (id INT PRIMARY KEY, data TEXT);`, write.Name))
			require.NoError(t, err)

			_, err = conn.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, data) VALUES (1, 'test');`, write.Name))
			require.NoError(t, err)

			row := conn.QueryRow(ctx, "SELECT spock.sync_event();")
			require.NoError(t, row.Scan(&syncLSN))
		})

		for _, read := range nodes {
			if read.Name == write.Name {
				continue
			}

			for instance := range db.GetInstances(WithNode(read.Name)) {
				t.Logf("validating table on node %s, instance %s with role %s", read.Name, instance.ID, *instance.Postgres.Role)

				readOpts := ConnectionOptions{
					Instance: instance,
					Username: username,
					Password: password,
				}
				db.WithConnection(ctx, readOpts, t, func(conn *pgx.Conn) {
					t.Log("waiting for replication to finish")

					var synced bool
					row := conn.QueryRow(ctx, "CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 30);", write.Name, syncLSN)

					require.NoError(t, row.Scan(&synced))
					require.True(t, synced)

					t.Log("selecting test data")

					var actual string
					row = conn.QueryRow(ctx, fmt.Sprintf(`SELECT data FROM %s WHERE id = 1;`, write.Name))

					require.NoError(t, row.Scan(&actual))
					require.Equal(t, "test", actual)
				})
			}
		}
	}
}
