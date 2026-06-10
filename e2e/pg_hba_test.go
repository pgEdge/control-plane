//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// TestPgHbaPgIdentUserConfig checks that user-supplied pg_hba_conf and
// pg_ident_conf entries reach the running Postgres. It uses a single-node
// database (the cheapest topology for this check) and asks Postgres itself,
// via pg_hba_file_rules and pg_ident_file_mappings, whether it loaded the
// entries without error. It then updates an entry and confirms the change is
// applied with a reload rather than a restart.
//
// The exact position of the user zone within pg_hba.conf is covered by the
// generator golden tests; connection allow/deny behavior and replication are
// covered elsewhere, so this test stays intentionally small.
func TestPgHbaPgIdentUserConfig(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	username := "admin"
	password := "password"

	ctx := t.Context()

	tLog(t, "creating a single-node database with user pg_hba and pg_ident entries")
	db := fixture.NewDatabaseFixture(ctx, t, &api.CreateDatabaseRequest{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_pg_hba",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "myapp_user",
					Password:   pointerTo(password),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			// A database-level rule that applies to every node, plus an ident
			// mapping.
			PgHbaConf: []string{
				"host all myapp_user 203.0.113.0/24 scram-sha-256",
			},
			PgIdentConf: []string{
				"ssl_users cert_admin myapp_user",
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
					// A node-level rule, which is prepended ahead of the
					// database-level rule.
					PgHbaConf: []string{
						"host all myapp_user 10.0.0.0/8 scram-sha-256",
					},
				},
			},
		},
	})

	opts := ConnectionOptions{Username: username, Password: password}

	// postmasterStartTime is captured before the update so we can confirm the
	// update reloads rather than restarts Postgres.
	var postmasterStartTime time.Time

	tLog(t, "verifying Postgres loaded the user entries")
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		// Every rule in the file parsed without error.
		require.Zero(t, hbaRuleErrors(ctx, t, conn),
			"pg_hba.conf contains rules that failed to parse")
		require.Zero(t, identMappingErrors(ctx, t, conn),
			"pg_ident.conf contains mappings that failed to parse")

		// The user entries are present, with the node-level rule ahead of the
		// database-level rule (the prepend ordering).
		require.Equal(t, []string{"10.0.0.0", "203.0.113.0"},
			userRuleAddresses(ctx, t, conn),
			"node-level entry should be prepended ahead of the database-level entry")

		// The pg_ident mapping is loaded.
		var pgUsername string
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT pg_username FROM pg_ident_file_mappings WHERE map_name = 'ssl_users'").Scan(&pgUsername))
		require.Equal(t, "myapp_user", pgUsername)

		require.NoError(t, conn.QueryRow(ctx,
			"SELECT pg_postmaster_start_time()").Scan(&postmasterStartTime))
	})

	tLog(t, "updating the entries and confirming a reload, not a restart")
	require.NoError(t, db.Update(ctx, UpdateOptions{
		Spec: &api.DatabaseSpec{
			DatabaseName: "test_pg_hba",
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "myapp_user",
					Password:   pointerTo(password),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			PgHbaConf: []string{
				"host all myapp_user 198.51.100.0/24 scram-sha-256",
			},
			PgIdentConf: []string{
				"ssl_users cert_admin myapp_user",
			},
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(host1)},
					PgHbaConf: []string{
						"host all myapp_user 172.16.0.0/12 scram-sha-256",
					},
				},
			},
		},
	}))

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		// The updated entries are now loaded.
		require.Equal(t, []string{"172.16.0.0", "198.51.100.0"},
			userRuleAddresses(ctx, t, conn))

		// Postgres reloaded (SIGHUP) rather than restarting, so the postmaster
		// start time is unchanged.
		var after time.Time
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT pg_postmaster_start_time()").Scan(&after))
		require.True(t, postmasterStartTime.Equal(after),
			"Postgres should reload, not restart (start time changed)")
	})
}

// userRuleAddresses returns the addresses of the active pg_hba rules for
// myapp_user, ordered by their position in the file.
func userRuleAddresses(ctx context.Context, t testing.TB, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT address
		FROM pg_hba_file_rules
		WHERE 'myapp_user' = ANY(user_name)
		ORDER BY rule_number`)
	require.NoError(t, err)
	addresses, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return addresses
}

func hbaRuleErrors(ctx context.Context, t testing.TB, conn *pgx.Conn) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRow(ctx,
		"SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL").Scan(&count))
	return count
}

func identMappingErrors(ctx context.Context, t testing.TB, conn *pgx.Conn) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRow(ctx,
		"SELECT count(*) FROM pg_ident_file_mappings WHERE error IS NOT NULL").Scan(&count))
	return count
}
