//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateUnderLoad(t *testing.T) {
	hostIDs := fixture.HostIDs()
	host1 := controlplane.Identifier(hostIDs[0])
	host2 := controlplane.Identifier(hostIDs[1])
	host3 := controlplane.Identifier(hostIDs[2])

	for _, tc := range []*LoadTest{
		{
			Name: "add node",
			StartingNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			UpdatedNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
				{
					Name:       "n3",
					HostIds:    []controlplane.Identifier{host3},
					SourceNode: pointerTo("n1"),
				},
			},
			LoadNodes: []string{"n1"},
		},
		{
			Name: "add replica",
			StartingNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			UpdatedNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1, host3}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			LoadNodes: []string{"n1"},
		},
		{
			Name: "add node load all",
			StartingNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			UpdatedNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
				{
					Name:       "n3",
					HostIds:    []controlplane.Identifier{host3},
					SourceNode: pointerTo("n1"),
				},
			},
			LoadNodes: []string{"n1", "n2"},
		},
		{
			Name: "add replica load all",
			StartingNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			UpdatedNodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{host1, host3}},
				{Name: "n2", HostIds: []controlplane.Identifier{host2}},
			},
			LoadNodes: []string{"n1", "n2"},
		},
	} {
		tc.Run(t)
	}
}

type LoadTest struct {
	Name          string
	StartingNodes []*controlplane.DatabaseNodeSpec
	UpdatedNodes  []*controlplane.DatabaseNodeSpec
	LoadNodes     []string
}

func (l *LoadTest) Run(t *testing.T) {
	t.Run(l.Name, func(t *testing.T) {
		t.Parallel()

		username := "admin"
		password := "password"
		users := []*controlplane.DatabaseUserSpec{
			{
				Username:   username,
				Password:   &password,
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			},
		}

		ctx, cancel := context.WithTimeout(t.Context(), 7*time.Minute)
		defer cancel()

		tLog(t, "creating database for load test")

		// Create the database
		db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
			Spec: &controlplane.DatabaseSpec{
				DatabaseName:  "load_test",
				Port:          pointerTo(0),
				Nodes:         l.StartingNodes,
				DatabaseUsers: users,
			},
		})

		tLog(t, "starting loaders")

		// Start the loaders
		var wg sync.WaitGroup
		loaders := make([]*Loader, len(l.LoadNodes))
		for i, n := range l.LoadNodes {
			loaders[i] = &Loader{
				DB:        db,
				NodeName:  n,
				Username:  username,
				Password:  password,
				TableName: n + "_data",
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaders[i].Run(ctx, t)
			}()
		}

		tLog(t, "updating the database")

		// Update the database while the loaders are running
		err := db.Update(ctx, UpdateOptions{
			Spec: &controlplane.DatabaseSpec{
				DatabaseName:  "load_test",
				Port:          pointerTo(0),
				Nodes:         l.UpdatedNodes,
				DatabaseUsers: users,
			},
		})
		require.NoError(t, err)

		tLog(t, "stopping the loaders")

		for _, loader := range loaders {
			loader.Stop()
		}
		wg.Wait()

		tLog(t, "validating each loader's table across all instances")

		for _, loader := range loaders {
			expected := loader.GetRows(ctx, t)

			tLogf(t, "expecting %d rows for table '%s'", expected, loader.TableName)

			for _, instance := range db.Instances {
				tLogf(t, "validating table '%s' on instance '%s'", loader.TableName, instance.ID)

				opts := ConnectionOptions{
					InstanceID: instance.ID,
					Username:   username,
					Password:   password,
				}
				db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
					var actual int

					sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, loader.TableName)
					err := conn.QueryRow(ctx, sql).Scan(&actual)

					assert.NoError(t, err)
					assert.Equal(t, expected, actual)
				})
			}
		}
	})
}

type Loader struct {
	DB        *DatabaseFixture
	NodeName  string
	Username  string
	Password  string
	TableName string
	done      chan struct{}
}

func (l *Loader) Run(ctx context.Context, t testing.TB) {
	t.Helper()

	l.done = make(chan struct{})
	l.DB.WithConnection(ctx, l.connOpts(), t, func(conn *pgx.Conn) {
		// Create table if not exists
		l.createTable(ctx, t, conn)

		// Persist workload until Stop() is called
		l.workload(ctx, t, conn)

		// Wait for replication to finish
		l.waitForReplication(ctx, t, conn)
	})
}

func (l *Loader) Stop() {
	close(l.done)
}

func (l *Loader) GetRows(ctx context.Context, t testing.TB) int {
	var totalRows int

	l.DB.WithConnection(ctx, l.connOpts(), t, func(conn *pgx.Conn) {
		// Fetch row count
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, l.TableName)
		err := conn.QueryRow(ctx, countSQL).Scan(&totalRows)
		require.NoError(t, err)
	})

	return totalRows
}

func (l *Loader) connOpts() ConnectionOptions {
	return ConnectionOptions{
		Matcher: And(
			WithNode(l.NodeName),
			WithRole("primary"),
		),
		Username: l.Username,
		Password: l.Password,
	}
}

func (l *Loader) createTable(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	sql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			data TEXT
		)`, l.TableName)

	_, err := conn.Exec(ctx, sql)
	require.NoError(t, err)
}

func (l *Loader) workload(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	tLogf(t, "%s loader: initiating load against the primary instance of node %s", l.TableName, l.NodeName)

	sql := fmt.Sprintf(`
			-- Insert some rows
			INSERT INTO %[1]s (data)
			SELECT md5(random()::text)
			FROM generate_series(1, 100);

			-- Delete some rows (simulate churn)
			DELETE FROM %[1]s
			WHERE id IN (SELECT id FROM %[1]s ORDER BY random() LIMIT 50);
		`, l.TableName)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.done:
			return
		case <-ticker.C:
			_, err := conn.Exec(ctx, sql)
			require.NoError(t, err)
		}
	}
}

func (l *Loader) waitForReplication(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	tLogf(t, "%s loader: got stop signal. waiting for replication catch up with writes.", l.TableName)

	lagSQL := `
		SELECT NOT EXISTS (
			SELECT 1
			FROM spock.lag_tracker
			WHERE replication_lag_bytes > 0
		);`

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var finished bool

			err := conn.QueryRow(ctx, lagSQL).Scan(&finished)
			require.NoError(t, err)

			if finished {
				return
			}
		}
	}
}
