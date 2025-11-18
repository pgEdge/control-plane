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
)

func TestMinorVersionUpgrade(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	username := "admin"
	password := "password"

	fromVersion := "18.0"
	toVersion := "18.1"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Logf("creating database with initial version %s", fromVersion)

	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "test",
			PostgresVersion: &fromVersion,
			Port:            pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
					},
				},
			},
		},
	})

	t.Log("asserting that the primary is running on the first host")

	// Assert that the primary is running on the first host in the host_ids
	// list.
	primary := db.GetInstance(WithRole("primary"))
	require.NotNil(t, primary)
	assert.Equal(t, host1, primary.HostID)

	opts := ConnectionOptions{
		InstanceID: primary.ID,
		Username:   username,
		Password:   password,
	}

	t.Logf("validating that the initial version is %s", fromVersion)

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var current string

		rows := conn.QueryRow(ctx, "SHOW server_version;")
		require.NoError(t, rows.Scan(&current))

		assert.Equal(t, fromVersion, current)
	})

	t.Logf("updating version to %s", toVersion)

	// Bump the minor version
	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "test",
			PostgresVersion: &toVersion,
			Port:            pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
					},
				},
			},
		},
	})
	require.NoError(t, err)

	t.Log("asserting that the primary hasn't changed")

	// Assert that the primary hasn't changed
	updated := db.GetInstance(WithRole("primary"))
	require.NotNil(t, updated)
	assert.Equal(t, primary.ID, updated.ID)
	assert.Equal(t, primary.HostID, updated.HostID)

	t.Logf("validating that the version is updated to %s", toVersion)

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var current string

		rows := conn.QueryRow(ctx, "SHOW server_version;")
		require.NoError(t, rows.Scan(&current))

		assert.Equal(t, toVersion, current)
	})
}
