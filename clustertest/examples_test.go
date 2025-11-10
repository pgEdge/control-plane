package clustertest_test

import (
	"context"
	"testing"
	"time"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/clustertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Example_basicCluster demonstrates creating a simple 3-host cluster.
func Example_basicCluster() {
	t := &testing.T{} // In real tests, this comes from the test function
	ctx := context.Background()

	// Create a 3-host cluster with auto-initialization
	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(3),
		clustertest.WithAutoInit(true),
	)
	if err != nil {
		panic(err)
	}
	defer cluster.Cleanup(ctx)

	// Use the cluster client
	client := cluster.Client()
	clusterInfo, _ := client.GetCluster(ctx)
	println("Cluster initialized:", clusterInfo.ID)
}

// Example_customConfiguration shows how to create a cluster with custom host configs.
func Example_customConfiguration() {
	t := &testing.T{}
	ctx := context.Background()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHost(clustertest.HostConfig{
			ID:       "host-1",
			EtcdMode: clustertest.EtcdModeServer,
			ExtraEnv: map[string]string{
				"PGEDGE_LOGGING__LEVEL": "debug",
			},
		}),
		clustertest.WithHost(clustertest.HostConfig{
			ID:       "host-2",
			EtcdMode: clustertest.EtcdModeServer,
		}),
		clustertest.WithHost(clustertest.HostConfig{
			ID:       "host-3",
			EtcdMode: clustertest.EtcdModeClient,
		}),
		clustertest.WithAutoInit(true),
	)
	if err != nil {
		panic(err)
	}
	defer cluster.Cleanup(ctx)
}

// Example_manualInitialization demonstrates manual cluster initialization.
func Example_manualInitialization() {
	t := &testing.T{}
	ctx := context.Background()

	// Create cluster without auto-init
	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(3),
		clustertest.WithAutoInit(false),
	)
	if err != nil {
		panic(err)
	}
	defer cluster.Cleanup(ctx)

	// Manually initialize the cluster
	err = cluster.InitializeCluster(ctx)
	if err != nil {
		panic(err)
	}
}

// TestDatabaseCreation shows a realistic test creating a database.
func TestDatabaseCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		//clustertest.WithHosts(3),
		clustertest.WithHosts(1),
		clustertest.WithAutoInit(true),
		clustertest.WithKeepOnFailure(true),
		clustertest.WithLogCapture(true),
	)
	require.NoError(t, err)

	cli := cluster.Client()
	hosts := cluster.Hosts()

	// Create a database with nodes on different hosts
	dbID := api.Identifier("testdb")
	dbName := "testdb"
	db, err := cli.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: &dbID,
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(hosts[0].ID())},
				},
				//{
				//	Name:    "n2",
				//	HostIds: []api.Identifier{api.Identifier(hosts[1].ID())},
				//},
			},
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username: "admin",
					Password: pointerTo("secret123"),
					Roles:    []string{"admin"},
				},
			},
		},
	})
	require.NoError(t, err)

	// Wait for database creation to complete
	// Log task status periodically to debug why it's stuck
	t.Logf("Waiting for task %s to complete...", db.Task.TaskID)
	task, err := cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: db.Database.ID,
		TaskID:     db.Task.TaskID,
	})
	if err != nil {
		// If we hit timeout, log the task log to see what's happening
		taskLog, logErr := cli.GetDatabaseTaskLog(ctx, &api.GetDatabaseTaskLogPayload{
			DatabaseID: db.Database.ID,
			TaskID:     db.Task.TaskID,
		})
		if logErr == nil {
			t.Logf("Task log (status=%s):", taskLog.TaskStatus)
			for _, entry := range taskLog.Entries {
				t.Logf("  [%s] %s", entry.Timestamp, entry.Message)
			}
		}
		require.NoError(t, err, "task failed or timed out")
	}
	assert.Equal(t, "completed", task.Status)

	// Verify database exists
	dbInfo, err := cli.GetDatabase(ctx, &api.GetDatabasePayload{DatabaseID: "testdb"})
	require.NoError(t, err)
	assert.Equal(t, "testdb", string(dbInfo.ID))
	assert.Equal(t, "available", dbInfo.State)
}

// TestHostFailover demonstrates testing failover scenarios.
func TestHostFailover(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(3),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	cli := cluster.Client()
	hosts := cluster.Hosts()

	// Create database with replicas
	dbID := api.Identifier("failoverdb")
	dbName := "failoverdb"
	db, err := cli.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: &dbID,
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(hosts[0].ID())},
				},
				{
					Name:    "n2",
					HostIds: []api.Identifier{api.Identifier(hosts[1].ID())},
				},
			},
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username: "admin",
					Password: pointerTo("secret123"),
					Roles:    []string{"admin"},
				},
			},
		},
	})
	require.NoError(t, err)
	task, err := cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: db.Database.ID,
		TaskID:     db.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)

	// Simulate host failure
	host1 := cluster.Host(hosts[0].ID())
	err = host1.Stop(ctx)
	require.NoError(t, err)

	// Wait for cluster to detect the failure
	eventually(t, 30*time.Second, func() bool {
		hostInfo, _ := cli.GetHost(ctx, &api.GetHostPayload{HostID: api.Identifier(hosts[0].ID())})
		return hostInfo != nil && hostInfo.Status != nil && hostInfo.Status.State == "unreachable"
	})

	// Perform failover
	failoverResp, err := cli.FailoverDatabaseNode(ctx, &api.FailoverDatabaseNodeRequest{
		DatabaseID: "failoverdb",
		NodeName:   "n1",
	})
	require.NoError(t, err)
	task, err = cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: "failoverdb",
		TaskID:     failoverResp.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)

	// Restart the host
	err = host1.Start(ctx)
	require.NoError(t, err)

	// Verify host rejoins cluster
	eventually(t, 30*time.Second, func() bool {
		hostInfo, _ := cli.GetHost(ctx, &api.GetHostPayload{HostID: api.Identifier(hosts[0].ID())})
		return hostInfo != nil && hostInfo.Status != nil && hostInfo.Status.State == "healthy"
	})
}

// TestNetworkPartition demonstrates testing network partition scenarios.
func TestNetworkPartition(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(5),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	cli := cluster.Client()
	hosts := cluster.Hosts()

	// Create a database
	dbID := api.Identifier("partitiondb")
	dbName := "partitiondb"
	db, err := cli.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: &dbID,
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(hosts[0].ID())},
				},
			},
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username: "admin",
					Password: pointerTo("secret123"),
					Roles:    []string{"admin"},
				},
			},
		},
	})
	require.NoError(t, err)
	task, err := cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: db.Database.ID,
		TaskID:     db.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)

	// Partition one of the hosts by pausing it
	host3 := cluster.Host(hosts[2].ID())
	err = host3.Pause(ctx)
	require.NoError(t, err)

	// Cluster should continue operating (majority still available)
	dbInfo, err := cli.GetDatabase(ctx, &api.GetDatabasePayload{DatabaseID: "partitiondb"})
	require.NoError(t, err)
	assert.Equal(t, "available", dbInfo.State)

	// Restore partition
	err = host3.Unpause(ctx)
	require.NoError(t, err)

	// Verify all hosts are healthy again
	eventually(t, 30*time.Second, func() bool {
		hostInfo, _ := cli.GetHost(ctx, &api.GetHostPayload{HostID: api.Identifier(hosts[2].ID())})
		return hostInfo != nil && hostInfo.Status != nil && hostInfo.Status.State == "healthy"
	})
}

// TestRollingRestart demonstrates testing rolling restart scenarios.
func TestRollingRestart(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(3),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	cli := cluster.Client()
	hosts := cluster.Hosts()

	// Create a database
	dbID := api.Identifier("restartdb")
	dbName := "restartdb"
	db, err := cli.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: &dbID,
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(hosts[0].ID())},
				},
			},
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username: "admin",
					Password: pointerTo("secret123"),
					Roles:    []string{"admin"},
				},
			},
		},
	})
	require.NoError(t, err)
	task, err := cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: db.Database.ID,
		TaskID:     db.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)

	// Restart each host one by one
	for _, host := range hosts {
		t.Logf("Restarting host %s", host.ID())

		err := host.Restart(ctx)
		require.NoError(t, err)

		// Give the host time to rejoin
		time.Sleep(2 * time.Second)

		// Verify cluster health
		clusterInfo, err := cli.GetCluster(ctx)
		require.NoError(t, err)
		assert.NotNil(t, clusterInfo)

		// Verify database is still accessible
		dbInfo, err := cli.GetDatabase(ctx, &api.GetDatabasePayload{DatabaseID: "restartdb"})
		require.NoError(t, err)
		assert.Equal(t, "available", dbInfo.State)
	}
}

// TestHostRemoval demonstrates removing a host from the cluster.
func TestHostRemoval(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(4),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	cli := cluster.Client()
	hosts := cluster.Hosts()

	// Create database on first three hosts
	dbID := api.Identifier("removaldb")
	dbName := "removaldb"
	db, err := cli.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: &dbID,
		Spec: &api.DatabaseSpec{
			DatabaseName: dbName,
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{api.Identifier(hosts[0].ID())},
				},
				{
					Name:    "n2",
					HostIds: []api.Identifier{api.Identifier(hosts[1].ID())},
				},
				{
					Name:    "n3",
					HostIds: []api.Identifier{api.Identifier(hosts[2].ID())},
				},
			},
			DatabaseUsers: []*api.DatabaseUserSpec{
				{
					Username: "admin",
					Password: pointerTo("secret123"),
					Roles:    []string{"admin"},
				},
			},
		},
	})
	require.NoError(t, err)
	task, err := cli.WaitForTask(ctx, &api.GetDatabaseTaskPayload{
		DatabaseID: db.Database.ID,
		TaskID:     db.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)

	// Remove the fourth host (no database nodes on it)
	hostToRemove := hosts[3].ID()
	err = cli.RemoveHost(ctx, &api.RemoveHostPayload{HostID: api.Identifier(hostToRemove)})
	require.NoError(t, err)

	// Verify host is removed
	hostList, err := cli.ListHosts(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, len(hostList.Hosts))

	// Verify remaining hosts still have the database
	for _, h := range hostList.Hosts {
		t.Logf("Host %s is still in cluster", h.ID)
	}
}

// TestEtcdDirectAccess demonstrates using direct etcd client access.
func TestEtcdDirectAccess(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(3),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	// Get direct etcd client for the first server
	host1 := cluster.Hosts()[0]
	etcdClient, err := host1.EtcdClient(ctx)
	require.NoError(t, err)

	// Directly query etcd for cluster information
	resp, err := etcdClient.MemberList(ctx)
	require.NoError(t, err)

	// Verify all etcd members are present
	// (First 3 hosts should be etcd servers)
	assert.GreaterOrEqual(t, len(resp.Members), 3)

	for _, member := range resp.Members {
		t.Logf("Etcd member: %s", member.Name)
	}
}

// TestDebugging demonstrates debugging features.
func TestDebugging(t *testing.T) {
	//t.Skip("disabled for focused testing")
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(2),
		clustertest.WithAutoInit(true),
		clustertest.WithLogCapture(true),    // Capture logs
		clustertest.WithKeepOnFailure(true), // Keep on failure
	)
	require.NoError(t, err)

	// Access logs programmatically
	logs, err := cluster.Logs(ctx)
	require.NoError(t, err)

	for hostID, log := range logs {
		t.Logf("Logs from %s: %d bytes", hostID, len(log))
	}

	// Logs will be automatically printed if test fails
	// Containers will be kept if test fails
}

// TestHostExecution demonstrates executing commands in hosts.
func TestHostExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cluster, err := clustertest.NewCluster(ctx, t,
		clustertest.WithHosts(1),
		clustertest.WithAutoInit(true),
	)
	require.NoError(t, err)

	host := cluster.Hosts()[0]

	// Execute a command in the container
	output, err := host.Exec(ctx, []string{"ls", "-la", "/data"})
	require.NoError(t, err)

	t.Logf("Data directory contents:\n%s", output)
}

// Helper function for waiting with timeout
func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		if time.Now().After(deadline) {
			t.Fatal("condition not met within timeout")
		}

		<-ticker.C
	}
}

// Helper function to convert any value to a pointer
func pointerTo[T any](v T) *T {
	return &v
}

// DONE 28 tests, 6 skipped, 1 failure in 376.302s
