//go:build e2e_test

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spock6PostgresVersions are the Postgres versions with a Spock 6.0.0-beta2
// entry in the version manifest.
var spock6PostgresVersions = []string{"16.16", "17.12", "18.7"}

// TestSpock6AddNode validates the add-node workflow end-to-end against a
// real Spock 6 cluster, adds a 3rd node, and confirms the full mesh reaches
// "replicating" — exercising the Spock-major-gated spock.progress query
// (PeerCatchupResource).
//
// On Docker Swarm, it runs on each Postgres version that has a Spock 6 entry
// in the version manifest, using the manifest's image. On systemd there's no
// manifest — the Spock 6 packages just need to be installed on the host,
// discovered by version, and requested via spock_version; postgres_version
// is left for the server to default so the test doesn't need to track exact
// installed package versions.
func TestSpock6AddNode(t *testing.T) {
	t.Parallel()
	fixture.SkipIfSpock6Unsupported(t)
	fixture.SkipIfSpockNotInstalled(t)

	if fixture.Orchestrator() == "systemd" {
		testSpock6AddNode(t, nil, "spock6_add_node_db")
		return
	}
	for _, version := range spock6PostgresVersions {
		t.Run("postgres "+version, func(t *testing.T) {
			t.Parallel()
			major, _, _ := strings.Cut(version, ".")
			testSpock6AddNode(t, pointerTo(version), "spock6_add_node_pg"+major)
		})
	}
}

func testSpock6AddNode(t *testing.T, postgresVersion *string, dbName string) {
	const (
		username = "admin"
		password = "password"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()

	nodeSpec := func(name, hostID string) *controlplane.DatabaseNodeSpec {
		return &controlplane.DatabaseNodeSpec{
			Name:    name,
			HostIds: []controlplane.Identifier{controlplane.Identifier(hostID)},
		}
	}

	t.Log("Step 1: Creating 2-node Spock 6 database fixture")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    dbName,
			PostgresVersion: postgresVersion,
			SpockVersion:    pointerTo("6"),
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{{
				Username:   username,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			}},
			Nodes: []*controlplane.DatabaseNodeSpec{
				nodeSpec("n1", hostIDs[0]),
				nodeSpec("n2", hostIDs[1]),
			},
		},
	})
	t.Logf("Database created: %s", db.ID)

	t.Log("Step 2: Adding n3 node with n1 as source")
	db.Spec.Nodes = append(db.Spec.Nodes, func() *controlplane.DatabaseNodeSpec {
		n := nodeSpec("n3", hostIDs[2])
		n.SourceNode = pointerTo("n1")
		return n
	}())
	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: db.Spec}))
	t.Log("Add-node completed successfully against Spock 6")

	t.Log("Step 3: Waiting for full mesh replication")
	db.WaitForReplication(ctx, t, username, password)
	t.Log("Replication complete")

	t.Log("Step 4: Verifying spock.spock_version() reports major 6 on the new node")
	n3Opts := ConnectionOptions{
		Matcher:  And(WithNode("n3"), WithRole("primary")),
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, n3Opts, t, func(conn *pgx.Conn) {
		var version string
		err := conn.QueryRow(ctx, "SELECT spock.spock_version();").Scan(&version)
		require.NoError(t, err)
		assert.Regexp(t, `^6\.`, version, "expected node n3 to be running Spock 6, got %q", version)
	})
}
