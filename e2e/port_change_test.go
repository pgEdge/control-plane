//go:build e2e_test

package e2e

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

func TestPortChange(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	username := "admin"
	password := "password"

	tLog(t, "creating database")

	ctx := t.Context()
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_port_change",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			// We want to use uncommon ports that are below the ephemeral range
			// and our default random range to minimize the chance of conflicts.
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:        "n1",
					HostIds:     []api.Identifier{api.Identifier(host1)},
					Port:        pointerTo(1024),
					PatroniPort: pointerTo(1124),
				},
				{
					Name:        "n2",
					HostIds:     []api.Identifier{api.Identifier(host2)},
					Port:        pointerTo(1026),
					PatroniPort: pointerTo(1126),
				},
			},
		},
	})

	tLog(t, "updating database to change ports")

	require.NoError(t, db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_port_change",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:        "n1",
					HostIds:     []api.Identifier{api.Identifier(host1)},
					Port:        pointerTo(1025),
					PatroniPort: pointerTo(1125),
				},
				{
					Name:        "n2",
					HostIds:     []api.Identifier{api.Identifier(host2)},
					Port:        pointerTo(1027),
					PatroniPort: pointerTo(1127),
				},
			},
		},
	}))

	tLog(t, "validating that the database is usable")

	n1Opts := ConnectionOptions{
		Username: username,
		Password: password,
		Matcher:  WithNode("n1"),
	}
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		if fixture.Orchestrator() == "systemd" {
			// This assertion won't work for swarm because we only change the
			// port binding. The postgres port always stays the same.
			var port string
			require.NoError(t, conn.QueryRow(ctx, "SHOW port").Scan(&port))
			require.Equal(t, "1025", port)
		}

		_, err := conn.Exec(ctx, `CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)`)
		require.NoError(t, err)

		_, err = conn.Exec(ctx, `INSERT INTO foo (id, val) VALUES (1, 'foo')`)
		require.NoError(t, err)
	})

	tLog(t, "waiting for replication")

	db.WaitForReplication(ctx, t, username, password)

	tLog(t, "validating that replication is functioning")

	n2Opts := ConnectionOptions{
		Username: username,
		Password: password,
		Matcher:  WithNode("n2"),
	}
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		if fixture.Orchestrator() == "systemd" {
			var port string
			require.NoError(t, conn.QueryRow(ctx, "SHOW port").Scan(&port))
			require.Equal(t, "1027", port)
		}

		var foo string
		require.NoError(t, conn.QueryRow(ctx, "SELECT val FROM foo WHERE id = 1").Scan(&foo))
		require.Equal(t, "foo", foo)
	})
}
