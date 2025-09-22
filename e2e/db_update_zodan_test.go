//go:build e2e_test

package e2e

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type NewInstanceInfo struct {
	Hostname string
	Nodename string
	Role     string
}

type tablerow struct {
	tablename string
	rowcount  int
}

type tablesrowcount []tablerow

// create cluster with three hosts and later generate load on the existing cluster and in parallel
// add a new node, or an instance in existing node. Verify all the instances after 'add node' are in
// sync and data is correctly replicated.
func TestZodanFunctionalityAddPrimary(t *testing.T) {
	t.Parallel()

	testZodanFunctionality(true, "n3", "host-1", false, "db1", t)

}

func TestZodanFunctionalityAddReplica(t *testing.T) {
	t.Parallel()

	testZodanFunctionality(false, "n1", "host-3", false, "db2", t)

}

func TestZodanFunctionalityAddPrimaryWithLoadOnAll(t *testing.T) {

	testZodanFunctionality(true, "n3", "host-1", true, "db3", t)
}
func TestZodanFunctionalityAddReplicaWithLoadOnAll(t *testing.T) {

	testZodanFunctionality(false, "n1", "host-3", true, "db4", t)
}

func testZodanFunctionality(createNewNode bool, nodeName string, hostName string, loadAllPrimaries bool, databasename string, t *testing.T) {

	type primariesOpts struct {
		PrimaryOptions []ConnectionOptions
	}

	var tablerowcounts tablesrowcount

	// define the cluster topology(nodes structure)
	totalhost := len(fixture.config.Hosts)
	nodeCount := 2
	hostPerNode := 2
	nodes := createNodesStruct(nodeCount, hostPerNode, t)

	expectedReplicas := totalhost - len(nodes)

	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Minute)
	defer cancel()
	// TODO: need to parameterize it
	// Create a new database fixture that provisions a database based on the given
	// specs and also provides access to useful objects and methods through the
	// fixture struct
	id := controlplane.Identifier(databasename)
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		ID: &id,
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: databasename,
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:  pointerTo(0),
			Nodes: nodes,
		},
	})

	// verification
	// 1. Validate expected count of primary and replica nodes
	assert.True(t, len(nodes) == getTotalPrimaryNodes(db),
		"Found replica count must match expected replica count")

	assert.True(t, expectedReplicas == getTotalReplicaNodes(db),
		"Found replica count must match expected replica count")

	// validate replication
	validateReplication(ctx, db, t)

	// get the information regarding hosts
	infoTobeVerified := updateSpecNodesObject(db,
		createNewNode, nodeName, hostName, t)

	primaryOpts := ConnectionOptions{
		Matcher:  WithRole("primary"),
		Username: username,
		Password: password,
	}

	t.Logf("Number of instances before appending cluster are: %d", len(db.Instances))

	tablename := "testtable"

	// parallel operations
	var wg sync.WaitGroup
	wg.Add(2)
	var expectedrows int
	var expectedrowsM int
	var err error
	connections := getAllConnectionOptsForPrimaries(db)

	// Run load test and add-node in parallel
	go func() {
		defer wg.Done()
		if !loadAllPrimaries {
			expectedrows, err = runLoad(ctx, db, primaryOpts, tablename, t)
			if err != nil {
				t.Log(err)
			}
		} else {
			tablerowcounts, err = runLoadAllPrimaries(ctx, db, connections, tablename, t)
			t.Logf("The total number of rows returned are1: %d", expectedrowsM)
			if err != nil {
				t.Log(err)
			}
		}

	}()

	go func() {
		defer wg.Done()
		updateClusterWithNewNode(ctx, db, t)
	}()
	wg.Wait()

	instance := db.GetInstance(And(
		WithHost(infoTobeVerified.Hostname),
		WithNode(infoTobeVerified.Nodename),
		WithRole(infoTobeVerified.Role),
	))
	assert.NotNil(t, instance)

	t.Logf("Number of instances after appending cluster are: %d", len(db.Instances))

	// verify after the add node, the data replicated is in sync on all the nodes/instances
	if loadAllPrimaries {
		t.Logf("The table counts are: %d ", len(tablerowcounts))
		for i := range tablerowcounts {
			verifyDataAllInstances(
				ctx,
				db,
				tablerowcounts[i].tablename,
				tablerowcounts[i].rowcount,
				t,
			)
		}
	} else {
		verifyDataAllInstances(ctx, db, tablename, expectedrows, t)

	}

}

// calls the update database api to update the cluster
func updateClusterWithNewNode(ctx context.Context,
	db *DatabaseFixture, t testing.TB) {
	time.Sleep(2 * time.Second)

	t.Log("Starting add node ...")

	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: db.Spec}))

	t.Log("Ends add node ...")
}

func runLoad(ctx context.Context, db *DatabaseFixture,
	primaryOpts ConnectionOptions, tableName string, t testing.TB) (int, error) {

	t.Logf("Starting write workload on table %s...", tableName)

	var totalRows int
	var runErr error

	db.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
		// Create table dynamically
		createTableSQL := fmt.Sprintf(`
            CREATE TABLE IF NOT EXISTS %s (
                id SERIAL PRIMARY KEY,
                data TEXT
            )`, tableName)
		_, _ = conn.Exec(ctx, createTableSQL)

		// Workload SQL
		workloadSQL := fmt.Sprintf(`
        DO $$
        DECLARE
            start_time TIMESTAMP := clock_timestamp();
        BEGIN
            WHILE clock_timestamp() < start_time + interval '1 minute' LOOP
                -- Insert some rows
                INSERT INTO %[1]s (data)
                SELECT md5(random()::text)
                FROM generate_series(1, 100);

                -- Delete some rows (simulate churn)
                DELETE FROM %[1]s
                WHERE id IN (SELECT id FROM %[1]s ORDER BY random() LIMIT 50);

                -- Small pause
                PERFORM pg_sleep(0.1);
            END LOOP;
        END$$;
        `, tableName)
		_, err := conn.Exec(ctx, workloadSQL)
		if err != nil {
			runErr = fmt.Errorf("failed to run load test on %s: %w", tableName, err)
			return
		}

		// Fetch row count
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableName)
		err = conn.QueryRow(ctx, countSQL).Scan(&totalRows)
		if err != nil {
			runErr = fmt.Errorf("failed to fetch row count from %s: %w", tableName, err)
		}
	})

	t.Logf("Write workload on %s finished. Total rows %d inserted ", tableName, totalRows)

	return totalRows, runErr
}

// runLoadAllPrimaries runs load test against all primaries in parallel
func runLoadAllPrimaries(ctx context.Context, db *DatabaseFixture, primaries []ConnectionOptions,
	tablename string, t testing.TB) (tablesrowcount, error) {
	var wg sync.WaitGroup
	errCh := make(chan error, len(primaries))
	resultsCh := make(chan tablerow, len(primaries))

	for i, p := range primaries {
		wg.Add(1)

		// Unique table name for each primary (e.g., load_test_0, load_test_1, ...)
		tableName := fmt.Sprintf("%s%d", tablename, i)

		go func(opts ConnectionOptions, tbl string) {
			defer wg.Done()
			rows, err := runLoad(ctx, db, opts, tbl, t)
			if err != nil {
				errCh <- fmt.Errorf("runLoad failed for %+v: %w", opts, err)
				return
			}
			resultsCh <- tablerow{tablename: tbl, rowcount: rows}
		}(p, tableName)
	}

	wg.Wait()
	close(resultsCh)
	close(errCh)

	// Aggregate results
	var tablerowcounts []tablerow
	for r := range resultsCh {
		tablerowcounts = append(tablerowcounts, r)
	}

	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}

	return tablerowcounts, errors.Join(errs...)
}

// This calculates number of records in a particular table for each instance
func verifyDataAllInstances(ctx context.Context, db *DatabaseFixture, tablename string, expectedrows int, t testing.TB) {

	for i := 0; i < len(db.Instances); i++ {
		connOpts := ConnectionOptions{
			Matcher:  WithID(db.Instances[i].ID),
			Username: username,
			Password: password,
		}
		t.Logf("Data verification on instance %s ", db.Instances[i].ID)
		sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tablename)
		db.WithConnection(ctx, connOpts, t, func(conn *pgx.Conn) {

			var rowCount int

			err := conn.QueryRow(ctx, sql).Scan(&rowCount)
			require.NoError(t, err)
			require.Equal(t, expectedrows, rowCount)

			t.Logf("Data verified on instance %s, expected_rows: %d == current_rows: %d ",
				db.Instances[i].ID, expectedrows, rowCount)
		})
	}

}

// extend existing cluster either by adding a new instance in existing node or
// by creating a new node
func updateSpecNodesObject(db *DatabaseFixture, addNewNode bool,
	nodeName string, hostname string, t testing.TB) NewInstanceInfo {
	var infoTobeVerified NewInstanceInfo
	if addNewNode {
		t.Logf("Updating Nodes specs, a new node: %s will be added with host: %s", nodeName, hostname)
		db.Spec.Nodes = append(db.Spec.Nodes, &controlplane.DatabaseNodeSpec{
			Name:       nodeName,
			HostIds:    []controlplane.Identifier{controlplane.Identifier(hostname)},
			SourceNode: pointerTo(db.Spec.Nodes[0].Name),
		})

		// for Instances verifyfication purpose
		infoTobeVerified.Hostname = hostname
		infoTobeVerified.Nodename = nodeName
		infoTobeVerified.Role = "primary"
	} else {
		t.Logf("Updating Nodes specs, a new instance: %s will be added in node: %s", hostname, nodeName)
		nodeIndex := getIndexOfNode(db, nodeName, t)
		db.Spec.Nodes[nodeIndex].HostIds = append(db.Spec.Nodes[nodeIndex].HostIds,
			controlplane.Identifier(hostname))

		// for Instances verifyfication purpose
		infoTobeVerified.Hostname = hostname
		infoTobeVerified.Nodename = nodeName
		infoTobeVerified.Role = "replica"

	}
	return infoTobeVerified
}

// Index of a particular node in the db.Spec.Nodes
func getIndexOfNode(db *DatabaseFixture, nodeName string, t testing.TB) int {

	for i, node := range db.Spec.Nodes {
		if node.Name == nodeName {
			t.Logf("The index for node name %s %d", nodeName, i)
			return i
		}
	}
	return -1
}

// verify the new instance is added in Instances with correct details
func verifyInstanceAddition(db *DatabaseFixture, infoTobeVerified NewInstanceInfo) bool {
	for _, inst := range db.Instances {

		if inst.HostID == infoTobeVerified.Hostname &&
			inst.NodeName == infoTobeVerified.Nodename &&
			*inst.Postgres.Role == infoTobeVerified.Role {
			return true
		}
	}
	return false
}

// Helping function to get a slice of all connections for primaries
func getAllConnectionOptsForPrimaries(db *DatabaseFixture) []ConnectionOptions {
	var primaries []ConnectionOptions

	for i := range db.Instances {
		if db.Instances[i].Postgres != nil && *db.Instances[i].Postgres.Role == "primary" {
			primaries = append(primaries, ConnectionOptions{
				Matcher:  And(WithHost(db.Instances[i].HostID)),
				Username: username,
				Password: password,
			})
		}
	}

	return primaries
}
