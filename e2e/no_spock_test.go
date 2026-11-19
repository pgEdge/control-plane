//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// TestCreateDBWithoutSpock verifies that a database created on hosts without
// Spock doesn't use it, and that its replicas still work.
func TestCreateDBWithoutSpock(t *testing.T) {
	t.Parallel()

	fixture.SkipIfSpockInstalled(t)

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tLog(t, "creating single-node database with a replica")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_without_spock",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1",
					HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
					},
				},
			},
		},
	})

	assert.Nil(t, db.Spec.SpockVersion, "spec should not have a spock version")
	for inst := range db.GetInstances(WithNode("n1")) {
		assert.Nil(t, inst.Spock, "instance %s should not have a spock status", inst.ID)
	}

	opts := ConnectionOptions{
		Matcher:  WithRole("primary"),
		Username: "admin",
		Password: "password",
	}

	tLog(t, "verifying that spock is not used on the primary")
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var hasSpock bool
		err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'spock')").Scan(&hasSpock)
		require.NoError(t, err)
		assert.False(t, hasSpock, "spock extension should not exist")

		var libraries string
		err = conn.QueryRow(ctx, "SELECT current_setting('shared_preload_libraries')").Scan(&libraries)
		require.NoError(t, err)
		assert.NotContains(t, libraries, "spock", "spock should not be preloaded")

		_, err = conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES (1, 'bar')")
		require.NoError(t, err)
	})

	tLog(t, "verifying that the replica receives writes")
	opts.Matcher = WithRole("replica")
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			var val string
			err := conn.QueryRow(ctx, "SELECT val FROM foo WHERE id = 1").Scan(&val)
			if !assert.NoError(c, err) {
				return
			}
			assert.Equal(c, "bar", val)
		}, time.Minute, time.Second)
	})
}

// TestCreateMultiNodeDBWithoutSpock verifies that a multi-node database is
// rejected on hosts without Spock, since there's nothing to replicate between
// nodes.
func TestCreateMultiNodeDBWithoutSpock(t *testing.T) {
	t.Parallel()

	fixture.SkipIfSpockInstalled(t)

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	createResp, err := fixture.Client.CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_multi_node_without_spock",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
				{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(host2)}},
			},
		},
	})
	require.Error(t, err, "CreateDatabase should reject a multi-node database without spock")
	assert.ErrorIs(t, err, client.ErrInvalidInput, "Error should be invalid input")
	assert.Nil(t, createResp, "CreateDatabase should not return a response")
	assert.Contains(t, err.Error(), "databases without spock are limited to a single node")
}

// TestCreateDBWithUninstalledPreloadLibrary verifies that systemd hosts reject
// a shared_preload_libraries entry that isn't installed, rather than failing
// to start Postgres.
func TestCreateDBWithUninstalledPreloadLibrary(t *testing.T) {
	t.Parallel()

	if fixture.Orchestrator() != "systemd" {
		t.Skip("installed libraries are only validated on systemd hosts")
	}

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	libraries := "pg_stat_statements,definitely_not_installed"
	if fixture.hasSpock {
		libraries += ",spock"
	}

	createResp, err := fixture.Client.CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_uninstalled_library",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
			},
			PostgresqlConf: map[string]any{
				"shared_preload_libraries": libraries,
			},
		},
	})
	require.Error(t, err, "CreateDatabase should reject an uninstalled library")
	assert.ErrorIs(t, err, client.ErrInvalidInput, "Error should be invalid input")
	assert.Nil(t, createResp, "CreateDatabase should not return a response")
	assert.Contains(t, err.Error(), "'definitely_not_installed' is not installed on host")
}
