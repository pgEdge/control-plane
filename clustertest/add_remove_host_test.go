//go:build cluster_test

package clustertest

import (
	"context"
	"testing"
	"time"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

func TestInitialization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config ClusterConfig
	}{
		{
			name: "single host",
			config: ClusterConfig{
				Hosts: []HostConfig{
					{ID: "host-1"},
				},
			},
		},
		{
			name: "three etcd servers",
			config: ClusterConfig{
				Hosts: []HostConfig{
					{ID: "host-1"},
					{ID: "host-2"},
					{ID: "host-3"},
				},
			},
		},
		{
			name: "one etcd server and one client",
			config: ClusterConfig{
				Hosts: []HostConfig{
					{ID: "host-1"},
					{ID: "host-2", EtcdMode: EtcdModeClient},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cluster := NewCluster(t, tc.config)
			cluster.Init(t)
		})
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()

	tLog(t, "initializing cluster with one host")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
		},
	})
	cluster.Init(t)

	tLog(t, "adding an etcd server host")

	cluster.Add(t, HostConfig{ID: "host-2"})
	cluster.Init(t)

	tLog(t, "adding an etcd client host")

	cluster.Add(t, HostConfig{ID: "host-3", EtcdMode: EtcdModeClient})
	cluster.Init(t)
}

func TestRemove(t *testing.T) {
	t.Parallel()

	tLog(t, "initializing a cluster with three hosts")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
		},
	})
	cluster.Init(t)

	tLog(t, "removing one of the hosts")

	cluster.Host("host-3").Stop(t)
	cluster.Remove(t, "host-3")
	cluster.AssertHealthy(t)
}

// create a 3 node cluster, forcibly kill n3, call "remove-host --force", assert that the 2 node cluster is healthy
func TestForcedHostRemovalWithDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tLog(t, "creating cluster")
	cluster := NewCluster(t, ClusterConfig{
		ID: "forced-removal-test",
		Hosts: []HostConfig{
			{ID: "host-1", EtcdMode: EtcdModeServer},
			{ID: "host-2", EtcdMode: EtcdModeServer},
			{ID: "host-3", EtcdMode: EtcdModeServer},
		},
	})

	// InitCluster and join
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "creating 3-node database")
	createResp, err := cluster.Client().CreateDatabase(ctx, &api.CreateDatabaseRequest{
		ID: pointerTo(api.Identifier("testdb")),
		Spec: &api.DatabaseSpec{
			DatabaseName: "testdb",
			Nodes: []*api.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []api.Identifier{"host-1"},
				},
				{
					Name:    "n2",
					HostIds: []api.Identifier{"host-2"},
				},
				{
					Name:    "n3",
					HostIds: []api.Identifier{"host-3"},
				},
			},
		},
	})
	require.NoError(t, err, "failed to create database")

	dbID := createResp.Database.ID

	tLog(t, "waiting for database creation task to complete")
	err = waitForTaskComplete(ctx, cluster.Client(), dbID, createResp.Task.TaskID, 3*time.Minute)
	require.NoError(t, err, "database creation task failed")

	tLog(t, "waiting for database to become available")
	err = waitForDatabaseAvailable(ctx, cluster.Client(), dbID, 3*time.Minute)
	require.NoError(t, err, "database failed to reach available state")

	tLog(t, "verifying database health with 3 nodes")
	err = verifyDatabaseHealth(ctx, t, cluster.Client(), dbID, 3)
	require.NoError(t, err, "database not healthy with 3 nodes")

	tLog(t, "forcibly stopping host-3 container")
	host3 := cluster.Host("host-3")
	err = host3.container.Terminate(ctx)
	require.NoError(t, err, "failed to terminate host-3 container")

	// Allow cluster some time to detect the container termination
	time.Sleep(5 * time.Second)

	tLog(t, "removing host-3 from cluster")
	resp, err := cluster.Client().RemoveHost(ctx, &api.RemoveHostPayload{
		HostID: "host-3",
		Force:  true,
	})
	require.NoError(t, err, "failed to remove host-3")
	require.NotNil(t, resp, "RemoveHost response should not be nil")
	require.NotNil(t, resp.UpdateDatabaseTasks, "UpdateDatabaseTasks should not be nil")

	tLog(t, "waiting for database to recover and become available")
	err = waitForDatabaseAvailable(ctx, cluster.Client(), dbID, 5*time.Minute)
	require.NoError(t, err, "database failed to recover after host removal")

	tLog(t, "verifying database health with 2 nodes")
	// err = verifyDatabaseHealth(ctx, t, cluster.Client(), dbID, 2)
	err = verifyDatabaseHealthWORKAROUND(ctx, t, cluster.Client(), dbID, 2)
	require.NoError(t, err, "database not healthy with 2 nodes")

	tLog(t, "verifying cluster has 2 hosts")
	err = waitForHostCount(ctx, cluster.Client(), 2, 30*time.Second)
	require.NoError(t, err, "cluster should have 2 hosts")

	// Ensure host-3 is not in the list
	hosts, err := cluster.Client().ListHosts(ctx)
	require.NoError(t, err)
	for _, h := range hosts.Hosts {
		require.NotEqual(t, "host-3", string(h.ID), "host-3 should be removed from cluster")
	}

	tLog(t, "cleaning up: deleting database")
	deleteResp, err := cluster.Client().DeleteDatabase(ctx, &api.DeleteDatabasePayload{
		DatabaseID: dbID,
	})
	require.NoError(t, err, "failed to delete database")

	tLog(t, "waiting for database deletion task to complete")
	err = waitForTaskComplete(ctx, cluster.Client(), dbID, deleteResp.Task.TaskID, 3*time.Minute)
	require.NoError(t, err, "database deletion task failed")

	tLog(t, "test completed successfully")
}
