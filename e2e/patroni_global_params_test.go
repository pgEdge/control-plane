//go:build e2e_test

package e2e

import (
	"testing"

	"github.com/jackc/pgx/v5"
	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

func TestUpdatePatroniGlobalParams(t *testing.T) {
	t.Parallel()

	// I'm intentionally not adding a helper function for this check because
	// this condition will be very temporary.
	if fixture.orchestrator != "systemd" {
		t.Skip("patroni global parameter update is currently unsupported in non-systemd clusters")
	}

	host1 := fixture.HostIDs()[0]

	username := "admin"
	password := "password"

	tLog(t, "creating database")

	ctx := t.Context()
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_global_params",
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

	tLog(t, "querying original max connections")

	opts := ConnectionOptions{
		Username: username,
		Password: password,
	}
	var originalMaxConnections int
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		require.NoError(t, conn.QueryRow(ctx, `SELECT setting::integer FROM pg_settings WHERE name = 'max_connections';`).Scan(&originalMaxConnections))
	})

	tLogf(t, "got max_connections=%d", originalMaxConnections)
	tLog(t, "updating database to change max connections")

	err := db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_global_params",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			PostgresqlConf: map[string]any{
				"max_connections": originalMaxConnections + 1,
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
				},
			},
		},
	})
	require.NoError(t, err)

	tLog(t, "querying new max connections")

	var newMaxConnections int
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		require.NoError(t, conn.QueryRow(ctx, `SELECT setting::integer FROM pg_settings WHERE name = 'max_connections';`).Scan(&newMaxConnections))
	})

	tLogf(t, "got max_connections=%d", newMaxConnections)

	require.Equal(t, originalMaxConnections+1, newMaxConnections)
}
