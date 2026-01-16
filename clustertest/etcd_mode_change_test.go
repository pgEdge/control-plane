//go:build cluster_test

package clustertest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromoteClientToServer(t *testing.T) {

	tLog(t, "initializing cluster with 3 etcd servers and 1 etcd client")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
			{ID: "host-4", EtcdMode: EtcdModeClient},
		},
	})
	cluster.Init(t)

	cluster.AssertHealthy(t)

	tLog(t, "verifying initial cluster has 4 healthy hosts")
	initialHostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 4, initialHostCount, "should have 4 healthy hosts initially")

	host4 := cluster.Host("host-4")
	initialMode := host4.GetEtcdMode(t, cluster.Client())
	require.Equal(t, "client", initialMode, "host-4 should initially be in client mode")

	tLog(t, "promoting host-4 from client to server mode")

	host4.RecreateWithMode(t, EtcdModeServer)

	cluster.RefreshClient(t)

	waitForHostsHealthy(t, cluster, 4, 2*time.Minute)

	tLog(t, "verifying cluster health after promotion")
	cluster.AssertHealthy(t)

	tLog(t, "verifying host-4 PGEDGE_ETCD_MODE changed to server")
	finalMode := host4.GetEtcdMode(t, cluster.Client())
	assert.Equal(t, "server", finalMode, "host-4 should be in server mode after promotion")

	tLog(t, "verifying all 4 hosts remain healthy after promotion")
	finalHostCount := countHealthyHosts(t, cluster)
	assert.Equal(t, 4, finalHostCount, "should have 4 healthy hosts after promotion")
}

func TestDemoteServerToClient(t *testing.T) {

	tLog(t, "initializing cluster with 4 etcd servers")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
			{ID: "host-4"},
		},
	})
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "verifying initial cluster has 4 healthy hosts")
	initialHostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 4, initialHostCount, "should have 4 healthy hosts initially")

	host4 := cluster.Host("host-4")
	initialMode := host4.GetEtcdMode(t, cluster.Client())
	require.Equal(t, "server", initialMode, "host-4 should initially be in server mode")

	tLog(t, "demoting host-4 from server to client mode")

	host4.RecreateWithMode(t, EtcdModeClient)

	cluster.RefreshClient(t)

	waitForHostsHealthy(t, cluster, 4, 2*time.Minute)

	tLog(t, "verifying cluster health after demotion")
	cluster.AssertHealthy(t)

	tLog(t, "verifying host-4 PGEDGE_ETCD_MODE changed to client")
	finalMode := host4.GetEtcdMode(t, cluster.Client())
	assert.Equal(t, "client", finalMode, "host-4 should be in client mode after demotion")

	tLog(t, "verifying all 4 hosts remain healthy after demotion")
	finalHostCount := countHealthyHosts(t, cluster)
	assert.Equal(t, 4, finalHostCount, "should have 4 healthy hosts after demotion")
}

func countHealthyHosts(t testing.TB, cluster *Cluster) int {
	t.Helper()

	resp, err := cluster.Client().ListHosts(t.Context())
	require.NoError(t, err, "should be able to list hosts")
	require.NotNil(t, resp, "list hosts response should not be nil")

	healthyCount := 0
	for _, host := range resp.Hosts {
		if host.Status.State == "healthy" {
			healthyCount++
		}
	}

	tLogf(t, "counted %d healthy hosts in cluster", healthyCount)
	return healthyCount
}

func waitForHostsHealthy(t testing.TB, cluster *Cluster, expectedCount int, timeout time.Duration) {
	t.Helper()

	tLogf(t, "waiting for %d hosts to become healthy (timeout: %v)", expectedCount, timeout)

	deadline := time.Now().Add(timeout)
	for {
		resp, err := cluster.Client().ListHosts(t.Context())
		if err == nil && resp != nil {
			healthyCount := 0
			for _, host := range resp.Hosts {
				if host.Status.State == "healthy" {
					healthyCount++
				}
			}

			if healthyCount == expectedCount {
				tLogf(t, "all %d hosts are healthy", expectedCount)
				return
			}

			tLogf(t, "currently %d/%d hosts healthy, waiting...", healthyCount, expectedCount)
		} else if err != nil {
			tLogf(t, "error checking hosts (will retry): %v", err)
		}

		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d healthy hosts", expectedCount)
		}

		time.Sleep(5 * time.Second)
	}
}
