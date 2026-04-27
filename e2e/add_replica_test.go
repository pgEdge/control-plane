//go:build e2e_test

package e2e

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

func TestAddReplica(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]

	username := "admin"
	password := "password"

	tLog(t, "creating database")

	ctx := t.Context()
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "add_replica",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
				{Name: "n2", HostIds: []api.Identifier{api.Identifier(host2)}},
			},
		},
	})

	tLog(t, "creating test data")

	opts := ConnectionOptions{
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id int primary key, val text)")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES (1, 'foo')")
		require.NoError(t, err)
	})

	tLog(t, "adding a replica")

	err := db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName: "add_replica",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name: "n1",
					HostIds: []api.Identifier{
						api.Identifier(host1),
						api.Identifier(host3),
					},
				},
				{Name: "n2", HostIds: []api.Identifier{api.Identifier(host2)}},
			},
		},
	})
	require.NoError(t, err)

	tLog(t, "validating that replica exists and is populated")

	opts = ConnectionOptions{
		Username: username,
		Password: password,
		Matcher:  And(WithNode("n1"), WithRole("replica")),
	}
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		const isInRecoveryQuery = "SELECT pg_is_in_recovery()"
		const valQuery = "SELECT val FROM foo WHERE id = 1"

		var isInRecovery bool
		require.NoError(t, conn.QueryRow(ctx, isInRecoveryQuery).Scan(&isInRecovery))
		require.True(t, isInRecovery)

		var val string
		require.NoError(t, conn.QueryRow(ctx, valQuery).Scan(&val))
		require.Equal(t, "foo", val)
	})
}
