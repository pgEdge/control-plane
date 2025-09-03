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

func TestUpdateAddNode23(t *testing.T) {
	testUpdateAddNode(t, 2, false)
}

func TestUpdateAddNode12(t *testing.T) {
	testUpdateAddNode(t, 1, false)
}

func testUpdateAddNode(t *testing.T, nodeCount int, deployReplicas bool) {
	username := "admin"
	password := "password"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	logDeploymentConfig(t, nodeCount, deployReplicas)

	// Step 1: Create DB
	nodes := buildNodeSpecs(nodeCount)
	db := createDatabaseFixture(ctx, t, username, password, nodes)

	// Step 2: Verify existing nodes
	for _, node := range db.Spec.Nodes {
		t.Logf("Verifying state of node %s", node.Name)
		verifyPrimaryNode(ctx, t, db, node, username, password, deployReplicas)
		if deployReplicas {
			t.Log("Verifying configuration & test data on replicas")
			verifyReplicaNodes(ctx, t, db, node, username, password)
		}
	}

	// Step 3: Add new node
	t.Log("Adding new node with zero down time enabled")
	newNodeName := addNewNode(ctx, t, db)
	t.Logf("nodes updated to total number %d", len(db.Spec.Nodes))

	// Step 4: Verify new node primary
	verifyNewNodePrimary(ctx, t, db, newNodeName, username, password)

	// Step 5: Verify replication
	opts := ConnectionOptions{Username: username, Password: password}
	db.VerifySpockReplication(ctx, t, db.Spec.Nodes, opts)

	// Step 6: Verify cross-node data
	verifyCrossNodeReplication(ctx, t, db, username, password)
}

// --- helpers ---

func logDeploymentConfig(t *testing.T, nodeCount int, deployReplicas bool) {
	t.Helper()
	if deployReplicas {
		t.Logf("Creating %d node database with 1 primary and 2 replicas per node", nodeCount)
	} else {
		t.Logf("Creating %d node database with 1 primary per node and no replicas", nodeCount)
	}
}

func buildNodeSpecs(nodeCount int) []*controlplane.DatabaseNodeSpec {
	var nodes []*controlplane.DatabaseNodeSpec
	for i := 1; i <= nodeCount; i++ {
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", i),
			HostIds: []controlplane.Identifier{controlplane.Identifier(fixture.HostIDs()[i-1])},
		})
	}
	return nodes
}

func createDatabaseFixture(ctx context.Context, t *testing.T, username, password string, nodes []*controlplane.DatabaseNodeSpec) *DatabaseFixture {
	t.Helper()
	return fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_db_create",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{{
				Username:   username,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			}},
			Port:  pointerTo(0),
			Nodes: nodes,
		},
	})
}

func verifyPrimaryNode(ctx context.Context, t *testing.T, db *DatabaseFixture, node *controlplane.DatabaseNodeSpec, username, password string, deployReplicas bool) {
	t.Helper()
	primaryOpts := ConnectionOptions{
		Matcher:  And(WithNode(node.Name), WithRole("primary")),
		Username: username,
		Password: password,
	}

	t.Log("Verifying state of primary")
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		var isInRecovery bool
		require.NoError(t, conn.QueryRow(ctx, "SELECT pg_is_in_recovery();").Scan(&isInRecovery))
		assert.False(t, isInRecovery)

		if deployReplicas {
			verifyHealthyReplicas(ctx, t, conn)
		}
	})

	t.Logf("Inserting test data on node %s primary", node.Name)
	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		createAndInsertTestData(ctx, t, conn, node.Name)
		if deployReplicas {
			verifyWALReplay(ctx, t, conn)
		}
	})
}

func verifyHealthyReplicas(ctx context.Context, t *testing.T, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, "SELECT state FROM pg_stat_replication where usename = 'patroni_replicator';")
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var state string
		require.NoError(t, rows.Scan(&state))
		if state == "streaming" {
			count++
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 2, count, "expected two healthy replicas")
}

func createAndInsertTestData(ctx context.Context, t *testing.T, conn *pgx.Conn, nodeName string) {
	t.Helper()
	table := pgx.Identifier{fmt.Sprintf("foo%s", nodeName)}
	sql := fmt.Sprintf("CREATE TABLE %s (id INT PRIMARY KEY, val TEXT)", table.Sanitize())
	_, err := conn.Exec(ctx, sql)
	require.NoError(t, err)

	for id, val := range []string{"foo", "bar"} {
		table := pgx.Identifier{fmt.Sprintf("foo%s", nodeName)}
		query := fmt.Sprintf("INSERT INTO %s (id, val) VALUES ($1, $2)", table.Sanitize())
		_, err = conn.Exec(ctx, query, id+1, val)
		require.NoError(t, err)
	}
}

func verifyWALReplay(ctx context.Context, t *testing.T, conn *pgx.Conn) {
	t.Helper()
	t.Log("Verifying WAL replay to all replicas")
	var commitLSN string
	require.NoError(t, conn.QueryRow(ctx, "SELECT pg_current_wal_lsn()").Scan(&commitLSN))

	for i := 0; i < 10; i++ {
		rows, err := conn.Query(ctx, "SELECT (replay_lsn >= $1::pg_lsn) AS has_replayed FROM pg_stat_replication", commitLSN)
		require.NoError(t, err)
		defer rows.Close()

		allReplayed := true
		for rows.Next() {
			var hasReplayed bool
			require.NoError(t, rows.Scan(&hasReplayed))
			if !hasReplayed {
				allReplayed = false
				break
			}
		}
		if allReplayed {
			return
		}
		time.Sleep(time.Second)
	}
}

func verifyReplicaNodes(ctx context.Context, t *testing.T, db *DatabaseFixture, node *controlplane.DatabaseNodeSpec, username, password string) {
	t.Helper()
	for _, instance := range db.Instances {
		if instance.NodeName != node.Name || instance.Postgres.Role == nil || *instance.Postgres.Role != "replica" {
			continue
		}
		opts := ConnectionOptions{InstanceID: instance.ID, Username: username, Password: password}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			verifyReplicaRecoveryAndData(ctx, t, conn, instance.ID, node.Name)
		})
	}
}

func verifyReplicaRecoveryAndData(ctx context.Context, t *testing.T, conn *pgx.Conn, instanceID, nodeName string) {
	t.Helper()
	t.Logf("Verifying replica is in recovery: %s", instanceID)
	var isInRecovery bool
	require.NoError(t, conn.QueryRow(ctx, "SELECT pg_is_in_recovery();").Scan(&isInRecovery))
	assert.True(t, isInRecovery)

	t.Logf("Verifying replica has correct data: %s", instanceID)
	var count int
	table := pgx.Identifier{fmt.Sprintf("foo%s", nodeName)}
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", table.Sanitize())
	require.NoError(t, conn.QueryRow(ctx, sql).Scan(&count))
	assert.Equal(t, 2, count, "expected row data on replica %s", instanceID)

	t.Logf("Verifying replica will not accept writes: %s", instanceID)
	table = pgx.Identifier{fmt.Sprintf("foo%s", nodeName)}
	query := fmt.Sprintf("INSERT INTO %s (id, val) VALUES ($1, $2)", table.Sanitize())
	_, err := conn.Exec(ctx, query, 3, "baz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only transaction")
}

func addNewNode(ctx context.Context, t *testing.T, db *DatabaseFixture) string {
	t.Helper()
	newNodeIndex := len(db.Spec.Nodes) + 1
	newNodeName := fmt.Sprintf("n%d", newNodeIndex)

	db.Spec.Nodes = append(db.Spec.Nodes, &controlplane.DatabaseNodeSpec{
		Name:       newNodeName,
		HostIds:    []controlplane.Identifier{controlplane.Identifier(fixture.HostIDs()[newNodeIndex-1])},
		SourceNode: pointerTo(db.Spec.Nodes[0].Name),
	})

	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: db.Spec}))
	return newNodeName
}

func verifyNewNodePrimary(ctx context.Context, t *testing.T, db *DatabaseFixture, nodeName, username, password string) {
	t.Helper()
	t.Logf("Inserting test data on node %s primary", nodeName)
	opts := ConnectionOptions{
		Matcher:  And(WithNode(nodeName), WithRole("primary")),
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		createAndInsertTestData(ctx, t, conn, nodeName)
	})
}

func verifyCrossNodeReplication(ctx context.Context, t *testing.T, db *DatabaseFixture, username, password string) {
	t.Helper()
	for _, node := range db.Spec.Nodes {
		opts := ConnectionOptions{
			Matcher:  And(WithNode(node.Name), WithRole("primary")),
			Username: username,
			Password: password,
		}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			for _, peerNode := range db.Spec.Nodes {
				if peerNode.Name == node.Name {
					continue
				}
				t.Logf("Verifying data created on peer node %s is present on %s", peerNode.Name, node.Name)
				var count int
				table := pgx.Identifier{fmt.Sprintf("foo%s", peerNode.Name)}
				sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", table.Sanitize())
				require.NoError(t, conn.QueryRow(ctx, sql).Scan(&count))
				assert.Equal(t, 2, count, "expected data from peer %s on %s", peerNode.Name, node.Name)
			}
		})
	}
}
