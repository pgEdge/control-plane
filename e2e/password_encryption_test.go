//go:build e2e_test

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// TestPasswordEncryptionDefaultsToScram checks that a newly created database,
// which does not override password_encryption, hashes database_users
// passwords with scram-sha-256 rather than the legacy md5 default.
func TestPasswordEncryptionDefaultsToScram(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	username := "admin"
	password := "password"

	ctx := t.Context()

	tLog(t, "creating a database without a password_encryption override")
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_scram_default",
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
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
				},
			},
		},
	})

	opts := ConnectionOptions{Username: username, Password: password}

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var encryption string
		require.NoError(t, conn.QueryRow(ctx,
			`SELECT setting FROM pg_settings WHERE name = 'password_encryption'`).Scan(&encryption))
		require.Equal(t, "scram-sha-256", encryption)

		require.True(t, strings.HasPrefix(rolePassword(ctx, t, conn, username), "SCRAM-SHA-256$"),
			"database_users password should be hashed with scram-sha-256 by default")
	})
}

// TestPasswordEncryptionAutoUpdatesDatabaseUsers checks the mechanism the
// scram-sha-256 default relies on for already-provisioned databases: every
// database_users entry is re-applied via ALTER ROLE ... WITH PASSWORD each
// time the instance resource is updated, regardless of whether the password
// itself changed. So a database that was provisioned under the old md5
// default gets every database_users password automatically rehashed the next
// time it goes through an update, without any per-user action.
//
// This is simulated here with an explicit password_encryption override
// (rather than the historical md5 default, which no longer exists in this
// codebase) so the "before" state is deterministic.
func TestPasswordEncryptionAutoUpdatesDatabaseUsers(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	adminUsername := "admin"
	appUsername := "myapp_user"
	password := "password"

	ctx := t.Context()

	users := func() []*api.DatabaseUserSpec {
		return []*api.DatabaseUserSpec{
			{
				Username:   adminUsername,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			},
			{
				Username:   appUsername,
				Password:   pointerTo(password),
				Attributes: []string{"LOGIN"},
			},
		}
	}

	tLog(t, "creating a database that simulates the legacy md5 default")
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName:  "test_scram_rehash",
			DatabaseUsers: users(),
			Port:          pointerTo(0),
			PatroniPort:   pointerTo(0),
			PostgresqlConf: map[string]any{
				"password_encryption": "md5",
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
				},
			},
		},
	})

	opts := ConnectionOptions{Username: adminUsername, Password: password}

	tLog(t, "verifying both database_users start out with md5 password hashes")
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		require.True(t, strings.HasPrefix(rolePassword(ctx, t, conn, adminUsername), "md5"))
		require.True(t, strings.HasPrefix(rolePassword(ctx, t, conn, appUsername), "md5"))
	})

	tLog(t, "updating the database to switch password_encryption to scram-sha-256")
	require.NoError(t, db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName:  "test_scram_rehash",
			DatabaseUsers: users(),
			Port:          pointerTo(0),
			PatroniPort:   pointerTo(0),
			PostgresqlConf: map[string]any{
				"password_encryption": "scram-sha-256",
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
				},
			},
		},
	}))

	tLog(t, "verifying every database_users entry was automatically rehashed to scram-sha-256")
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		require.True(t, strings.HasPrefix(rolePassword(ctx, t, conn, adminUsername), "SCRAM-SHA-256$"),
			"admin password should have been rehashed without any per-user action")
		require.True(t, strings.HasPrefix(rolePassword(ctx, t, conn, appUsername), "SCRAM-SHA-256$"),
			"myapp_user password should have been rehashed without any per-user action")
	})
}

func rolePassword(ctx context.Context, t testing.TB, conn *pgx.Conn, roleName string) string {
	var rolPassword string
	require.NoError(t, conn.QueryRow(ctx,
		"SELECT rolpassword FROM pg_authid WHERE rolname = $1", roleName).Scan(&rolPassword))
	return rolPassword
}
