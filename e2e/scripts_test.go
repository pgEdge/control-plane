//go:build e2e_test

package e2e

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

func TestScripts(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	username := "admin"
	password := "password"

	tLog(t, "creating database")

	ctx := t.Context()
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_scripts",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username: "app",
					// This role assignment will fail if the post-init script
					// does not run before the users are created.
					Roles: []string{"test_role"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
			},
			Scripts: &api.DatabaseScripts{
				PostInit: api.SQLScript{
					"CREATE ROLE test_role NOLOGIN",
				},
				PostDatabaseCreate: api.SQLScript{
					"CREATE TABLE foo (id int primary key, val text)",
					"INSERT INTO foo (id, val) VALUES (1, 'foo')",
				},
			},
		},
	})

	tLog(t, "validating that scripts ran")

	opts := ConnectionOptions{
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		const roleExistsQuery = "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'test_role')"
		const valQuery = "SELECT val FROM foo WHERE id = 1"

		var roleExists bool
		require.NoError(t, conn.QueryRow(ctx, roleExistsQuery).Scan(&roleExists))
		require.True(t, roleExists)

		var val string
		require.NoError(t, conn.QueryRow(ctx, valQuery).Scan(&val))
		require.Equal(t, "foo", val)
	})

	tLog(t, "updating database scripts and adding a node")

	db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_scripts",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				// TODO(PLAT-544): This will work after the 'populate nodes'
				//        enhancement to propagate roles from the source node.
				// {
				// 	Username: "app",
				// 	Roles:    []string{"test_role"},
				// },
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
				{Name: "n2", HostIds: []api.Identifier{api.Identifier(host2)}},
			},
			Scripts: &api.DatabaseScripts{
				PostInit: api.SQLScript{
					"CREATE ROLE test_role_2 NOLOGIN",
				},
				PostDatabaseCreate: api.SQLScript{
					"CREATE TABLE bar (id int primary key, val text)",
					"INSERT INTO bar (id, val) VALUES (1, 'foo')",
				},
			},
		},
	})

	tLog(t, "validating that the scripts did not run")

	opts = ConnectionOptions{
		Username: username,
		Password: password,
		Matcher:  WithNode("n2"),
	}
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		// const roleExistsQuery = "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'test_role')"
		const roleNotExistsQuery = "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'test_role_2')"
		const valQuery = "SELECT val FROM foo WHERE id = 1"
		const tableNotExistsQuery = "SELECT NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'bar');"

		// TODO(PLAT-544): This will work after the 'populate nodes' enhancement
		//       to propagate roles from the source node.
		// var roleExists bool
		// assert.NoError(t, conn.QueryRow(ctx, roleExistsQuery).Scan(&roleExists))
		// assert.True(t, roleExists)

		var roleNotExists bool
		require.NoError(t, conn.QueryRow(ctx, roleNotExistsQuery).Scan(&roleNotExists))
		require.True(t, roleNotExists)

		var val string
		require.NoError(t, conn.QueryRow(ctx, valQuery).Scan(&val))
		require.Equal(t, "foo", val)

		var tableNotExists bool
		require.NoError(t, conn.QueryRow(ctx, tableNotExistsQuery).Scan(&tableNotExists))
		require.True(t, tableNotExists)
	})
}
