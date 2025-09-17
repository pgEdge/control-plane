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

	var infoTobeVerified NewInstanceInfo
	var tablerowcounts tablesrowcount
	targetPgVersion := "17.6"

	// define the cluster topology(nodes structure)
	totalhost := len(fixture.config.Hosts)
	nodeCount := 2
	hostPerNode := 2
	nodes := createNodesStruct2(nodeCount, hostPerNode, t)

	expectedReplicas := totalhost - len(nodes)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
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
			Port:            pointerTo(0),
			Cpus:            pointerTo("1000m"),
			Memory:          pointerTo("1GB"),
			PostgresVersion: pointerTo(targetPgVersion),
			SpockVersion:    pointerTo("5"),
			Nodes:           nodes,
		},
	})

	// verification
	// 1. Validate expected count of primary and replica nodes
	assert.True(t, len(nodes) == getTotalPrimaryNodes(db),
		"Found replica count must match expected replica count")

	assert.True(t, expectedReplicas == getTotalReplicaNodes(db),
		"Found replica count must match expected replica count")

	validateReplication(ctx, db, t)

	// build spec.Node object with the new node/instance info
	infoTobeVerified2 := updateSpecNodesObject(db,
		createNewNode, nodeName, hostName, infoTobeVerified, t)

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

	assert.True(t, verifyInstanceAddition(db, infoTobeVerified2),
		"The new instance info does not seem correct")

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
	rowsCh := make(chan int, len(primaries))
	errCh := make(chan error, len(primaries))
	var tablerowcounts tablesrowcount

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
			rowsCh <- rows
			tablerowcounts = append(tablerowcounts, tablerow{
				tablename: tableName,
				rowcount:  rows,
			})
		}(p, tableName)
	}

	wg.Wait()
	close(rowsCh)
	close(errCh)

	// Aggregate results
	totalRows := 0
	for r := range rowsCh {
		totalRows += r
	}

	// Combine errors (if any)
	var runErr error
	for e := range errCh {
		if runErr == nil {
			runErr = e
		} else {
			runErr = fmt.Errorf("%v; %w", runErr, e)
		}
	}

	return tablerowcounts, runErr
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
			if err != nil {
				t.Logf("Failed to count rows: %v\n", err)
			}
			if rowCount != expectedrows {
				t.Errorf("Instance %s: expected %d rows, got %d",
					db.Instances[i].ID, expectedrows, rowCount)
			} else {
				t.Logf("Data verified on instance %s, expected_rows: %d == current_rows: %d ",
					db.Instances[i].ID, expectedrows, rowCount)
			}
		})
	}

}

// extend existing cluster either by adding a new instance in existing node or
// by creating a new node
func updateSpecNodesObject(db *DatabaseFixture, addNewNode bool,
	nodeName string, hostname string, infoTobeVerified NewInstanceInfo, t testing.TB) NewInstanceInfo {
	if addNewNode {
		t.Logf("Updating Nodes specs, a new node: %s will be added with host: %s", hostname, nodeName)
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

// Helping function to get a slice of all connections for replicas
func getAllConnectionOptsForReplicas(db *DatabaseFixture) []ConnectionOptions {
	var replicas []ConnectionOptions

	for i := range db.Instances {
		if db.Instances[i].Postgres != nil && *db.Instances[i].Postgres.Role == "replica" {
			replicas = append(replicas, ConnectionOptions{
				Matcher:  And(WithHost(db.Instances[i].HostID)),
				Username: username,
				Password: password,
			})
		}
	}

	return replicas
}

// TODO: Need to create one common function to be used across all the test cases
// build spec.Nodes, creates a new node or update existing node
func createNodesStruct2(numNodes int, hostsPerNode int, t testing.TB) []*controlplane.DatabaseNodeSpec {
	nodes := []*controlplane.DatabaseNodeSpec{}
	totalHosts := len(fixture.config.Hosts)
	hostIndex := 0

	t.Logf("Going to create topology for number of nodes %d and total hosts %d\n", numNodes, totalHosts)

	for i := 1; i <= numNodes && hostIndex < totalHosts; i++ {
		end := hostIndex + hostsPerNode
		if end > totalHosts {
			end = totalHosts
		}

		hosts := []controlplane.Identifier{}
		for j := hostIndex; j < end; j++ {
			hosts = append(hosts, controlplane.Identifier(fixture.HostIDs()[j]))
		}
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", i),
			HostIds: hosts,
		})

		hostIndex = end
	}
	//take care of leftover hosts
	if hostIndex < totalHosts {
		hosts := []controlplane.Identifier{}
		for j := hostIndex; j < totalHosts; j++ {
			hosts = append(hosts, controlplane.Identifier(fixture.HostIDs()[j]))
		}
		nodes = append(nodes, &controlplane.DatabaseNodeSpec{
			Name:    fmt.Sprintf("n%d", len(nodes)+1),
			HostIds: hosts,
		})
	}

	return nodes
}
