//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSingleNodeDB(t *testing.T) {
	testCreateDB(t, 1, false)
}

func TestCreateSingleNodeDBWithReplicas(t *testing.T) {
	testCreateDB(t, 1, true)
}

func TestCreateMultiNodeDB(t *testing.T) {
	testCreateDB(t, 2, false)
}

func TestCreateMultiNodeDBWithReplicas(t *testing.T) {
	testCreateDB(t, 2, true)
}

func testCreateDB(t *testing.T, nodeCount int, deployReplicas bool) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]

	username := "admin"
	password := "password"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if deployReplicas {
		t.Logf("Creating %d node database with 1 primary and 2 replicas per node", nodeCount)
	} else {
		t.Logf("Creating %d node database with 1 primary per node and no replicas", nodeCount)
	}

	nodes := []*controlplane.DatabaseNodeSpec{}
	for i := 1; i <= nodeCount; i++ {
		hosts := []controlplane.Identifier{controlplane.Identifier(host1)}
		if deployReplicas {
			hosts = append(hosts, controlplane.Identifier(host2))
		}
		if deployReplicas {
			hosts = append(hosts, controlplane.Identifier(host3))
		}
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", i),
			HostIds: hosts,
		})
	}

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
			Port:  pointerTo(0),
			Nodes: nodes,
		},
	})

	for _, node := range db.Spec.Nodes {
		t.Logf("Verifying state of node %s", node.Name)

		primaryOpts := ConnectionOptions{
			Matcher:  And(WithNode(node.Name), WithRole("primary")),
			Username: username,
			Password: password,
		}

		t.Log("Verifying state of primary")

		db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
			var isInRecovery bool

			row := conn.QueryRow(ctx, "SELECT pg_is_in_recovery();")
			require.NoError(t, row.Scan(&isInRecovery))

			assert.False(t, isInRecovery)

			if deployReplicas {
				rows, err := conn.Query(ctx, "SELECT state FROM pg_stat_replication where usename = 'patroni_replicator';")
				require.NoError(t, err)
				defer rows.Close()

				healthyReplicaCount := 0
				for rows.Next() {
					var state string
					err := rows.Scan(&state)
					require.NoError(t, err)

					if state == "streaming" {
						healthyReplicaCount++
					}
				}
				replicaCount := healthyReplicaCount
				require.NoError(t, rows.Err())
				assert.Equal(t, 2, replicaCount, "expected two healthy replicas")
			}

		})

		db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
			t.Logf("Inserting test data on node %s primary", node.Name)

			_, err := conn.Exec(ctx, fmt.Sprintf("CREATE TABLE foo%s (id INT PRIMARY KEY, val TEXT)", node.Name))
			require.NoError(t, err)

			_, err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO foo%s (id, val) VALUES ($1, $2)", node.Name), 1, "foo")
			require.NoError(t, err)

			_, err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO foo%s (id, val) VALUES ($1, $2)", node.Name), 2, "bar")
			require.NoError(t, err)

			if deployReplicas {
				t.Log("Verifying WAL replay to all replicas")

				// Get commit_lsn to verify replay
				var commitLSN string
				row := conn.QueryRow(ctx, "SELECT pg_current_wal_lsn() AS commit_lsn")
				require.NoError(t, row.Scan(&commitLSN))

				// Wait for up to 10 seconds and verify commit_lsn has replayed
				// This should prevent flaky tests
				var replayLSN string
				var hasReplayed bool

				for i := 0; i < 10; i++ {
					rows, err := conn.Query(ctx, "SELECT replay_lsn, (replay_lsn >= $1::pg_lsn) AS has_replayed FROM pg_stat_replication", commitLSN)
					require.NoError(t, err)
					defer rows.Close()

					hasReplayed = true
					for rows.Next() {
						err := rows.Scan(&replayLSN, &hasReplayed)
						require.NoError(t, err)
						if !hasReplayed {
							hasReplayed = false
							break
						}
					}

					if hasReplayed {
						break
					}

					time.Sleep(1 * time.Second)
				}
			}
		})

		if deployReplicas {
			t.Log("Verifying configuration & test data on replicas")

			for _, instance := range db.Instances {

				if instance.NodeName != node.Name {
					continue
				}
				role := instance.Postgres.Role
				if role == nil || *role != "replica" {
					continue
				}
				replicaOpts := ConnectionOptions{
					InstanceID: instance.ID,
					Username:   username,
					Password:   password,
				}

				db.WithConnection(ctx, replicaOpts, t, func(conn *pgx.Conn) {
					t.Logf("Verifying replica is in recovery: %s", instance.ID)

					var isInRecovery bool
					row := conn.QueryRow(ctx, "SELECT pg_is_in_recovery();")
					require.NoError(t, row.Scan(&isInRecovery))

					assert.True(t, isInRecovery)

					t.Logf("Verifying replica has correct data: %s", instance.ID)
					var count int
					row = conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM foo%s", node.Name))
					require.NoError(t, row.Scan(&count))

					assert.Equal(t, 2, count, "expected row data on replica instance %s", instance.ID)

					t.Logf("Verifying replica will not accept writes: %s", instance.ID)
					query := fmt.Sprintf("INSERT INTO %s (id, val) VALUES ($1, $2)", pgx.Identifier{fmt.Sprintf("foo%s", node.Name)}.Sanitize())
					_, err := conn.Exec(ctx, query, 3, "baz")
					require.Error(t, err)
					assert.Contains(t, err.Error(), "cannot execute INSERT in a read-only transaction")
				})
			}
		}
	}

	if nodeCount > 1 {
		db.WaitForReplication(ctx, t, username, password)

		for _, node := range db.Spec.Nodes {

			primaryOpts := ConnectionOptions{
				Matcher:  And(WithNode(node.Name), WithRole("primary")),
				Username: username,
				Password: password,
			}
			db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
				for _, peerNode := range db.Spec.Nodes {
					// Skip check of data created on current node
					if peerNode.Name == node.Name {
						continue
					}

					t.Logf("Verifying data created on peer node %s is present on %s", peerNode.Name, node.Name)
					var count int
					row := conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM foo%s", peerNode.Name))
					require.NoError(t, row.Scan(&count))

					assert.Equal(t, 2, count, "expected row data from peer node %s", peerNode.Name)

				}
			})
		}
	}

}
