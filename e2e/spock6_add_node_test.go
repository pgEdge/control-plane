//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spock6DevImage is a floating/mutable tag tracking the latest Spock 6
// development build. Not pinned to a specific build number: nightly CI
// re-running this test picks up whatever the tag currently resolves to,
// with no extra plumbing needed to point CI at "latest."
const spock6DevImage = "ghcr.io/pgedge/pgedge-postgres:18-spock6-standard"

// TestSpock6AddNode validates the add-node workflow end-to-end against a
// real Spock 6 cluster: creates a 2-node database pinned to a Spock 6 dev
// image via orchestrator_opts.swarm.image (bypassing manifest version
// constraints, since spock6 manifest entries are deliberately "dev"
// stability and never auto-selected), adds a 3rd node, and confirms the
// full mesh reaches "replicating" — exercising the Spock-major-gated
// spock.progress query (PeerCatchupResource) and the verify-replicating
// step (VerifySubscriptionReplicatingResource) added in this same ticket.
func TestSpock6AddNode(t *testing.T) {
	t.Parallel()

	const (
		username = "admin"
		password = "password"
		dbName   = "spock6_add_node_db"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()

	nodeSpec := func(name, hostID string) *controlplane.DatabaseNodeSpec {
		return &controlplane.DatabaseNodeSpec{
			Name:    name,
			HostIds: []controlplane.Identifier{controlplane.Identifier(hostID)},
			OrchestratorOpts: &controlplane.OrchestratorOpts{
				Swarm: &controlplane.SwarmOpts{Image: pointerTo(spock6DevImage)},
			},
		}
	}

	t.Log("Step 1: Creating 2-node Spock 6 database fixture")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    dbName,
			PostgresVersion: pointerTo("18.6"),
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
