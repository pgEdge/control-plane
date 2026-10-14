//go:build e2e_test

package e2e

import (
	"testing"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

// breakSharedPreloadLibraries updates db with a shared_preload_libraries value
// that references a library that isn't installed, which causes Postgres to
// fail to restart and leaves the node without a primary. It returns the
// node's last-known-good spec so the caller can submit it again to recover.
func breakSharedPreloadLibraries(t *testing.T, db *DatabaseFixture, host1 string, dbName string) *api.DatabaseSpec {
	t.Helper()

	goodSpec := &api.DatabaseSpec{
		DatabaseName: dbName,
		DatabaseUsers: []*api.DatabaseUserSpec{
			{Username: "admin", DbOwner: pointerTo(true), Attributes: []string{"LOGIN", "SUPERUSER"}},
		},
		Port: pointerTo(0),
		Nodes: []*api.DatabaseNodeSpec{
			{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
		},
		PostgresqlConf: map[string]any{
			"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
		},
	}

	badSpec := &api.DatabaseSpec{}
	*badSpec = *goodSpec
	badSpec.PostgresqlConf = map[string]any{
		"shared_preload_libraries": "pg_stat_statements,snowflake,spock,definitely_not_installed",
	}

	err := db.Update(t.Context(), UpdateOptions{Spec: badSpec})
	require.Error(t, err, "update with an uninstalled library should fail")

	return goodSpec
}

func TestUpdateDatabaseRecoversNodeWithNoPrimary(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	dbName := "no_primary_recovery"

	ctx := t.Context()

	tLog(t, "creating database")
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			DatabaseUsers: []*api.DatabaseUserSpec{
				{Username: "admin", Password: pointerTo("password"), DbOwner: pointerTo(true), Attributes: []string{"LOGIN", "SUPERUSER"}},
			},
			Port: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
			},
			PostgresqlConf: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
			},
		},
	})

	tLog(t, "breaking the node with an uninstalled shared_preload_libraries entry")
	goodSpec := breakSharedPreloadLibraries(t, db, host1, dbName)

	require.NoError(t, db.Refresh(ctx))
	require.Equal(t, "failed", db.Database.State, "database should be in a failed state")

	tLog(t, "resubmitting the last-known-good spec")
	err := db.Update(ctx, UpdateOptions{Spec: goodSpec})
	require.NoError(t, err, "update should repair the node even though it had no primary")

	require.Equal(t, "available", db.Database.State)
	inst := db.GetInstance(WithNode("n1"))
	require.NotNil(t, inst.Postgres)
	require.Equal(t, "primary", *inst.Postgres.Role)
}

func TestDeleteDatabaseWithNoPrimaryNode(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	dbName := "no_primary_delete"

	ctx := t.Context()

	tLog(t, "creating database")
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			DatabaseUsers: []*api.DatabaseUserSpec{
				{Username: "admin", Password: pointerTo("password"), DbOwner: pointerTo(true), Attributes: []string{"LOGIN", "SUPERUSER"}},
			},
			Port: pointerTo(0),
			Nodes: []*api.DatabaseNodeSpec{
				{Name: "n1", HostIds: []api.Identifier{api.Identifier(host1)}},
			},
			PostgresqlConf: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
			},
		},
	})

	tLog(t, "breaking the node with an uninstalled shared_preload_libraries entry")
	breakSharedPreloadLibraries(t, db, host1, dbName)

	tLog(t, "deleting the database while its only node has no primary")
	require.NoError(t, db.Delete(ctx, true))
}
