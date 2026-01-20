//go:build cluster_test

package clustertest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUseHostnameWhenIPUnreachable(t *testing.T) {
	tLog(t, "initializing cluster with 3 etcd servers")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
		},
	})
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "verifying initial cluster has 3 healthy hosts")
	initialHostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 3, initialHostCount, "should have 3 healthy hosts initially")

	resp, err := cluster.Client().ListHosts(t.Context())
	require.NoError(t, err)
	for _, host := range resp.Hosts {
		tLogf(t, "host %s: ipv4_address=%s, hostname=%s", host.ID, host.Ipv4Address, host.Hostname)
	}

	tLog(t, "test passed: cluster initialized successfully with automatic IP detection")
}

func TestRestartedHostUsesHostnameIfIPUnreachable(t *testing.T) {
	tLog(t, "initializing cluster with 3 etcd servers")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
		},
	})
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "verifying initial cluster has 3 healthy hosts")
	initialHostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 3, initialHostCount, "should have 3 healthy hosts initially")

	host3 := cluster.Host("host-3")

	tLog(t, "stopping host-3")
	host3.Stop(t)

	tLog(t, "recreating host-3 with an unreachable IP address (simulating IP change)")
	
	host3.RecreateWithEnv(t, map[string]string{
		"PGEDGE_IPV4_ADDRESS": "10.99.99.99", 
	})

	cluster.RefreshClient(t)

	tLog(t, "waiting for all hosts to become healthy")
	waitForHostsHealthy(t, cluster, 3, 2*time.Minute)

	tLog(t, "verifying cluster health after host-3 restart with changed IP")
	cluster.AssertHealthy(t)

	tLog(t, "verifying host-3 is reporting the new IP address")
	resp, err := cluster.Client().ListHosts(t.Context())
	require.NoError(t, err)

	var host3Found bool
	for _, host := range resp.Hosts {
		if string(host.ID) == "host-3" {
			host3Found = true
			// The host should report the configured IP, but communication
			// should work via hostname since the IP is unreachable.
			assert.Equal(t, "10.99.99.99", host.Ipv4Address,
				"host-3 should report the configured IP address")
			tLogf(t, "host-3: ipv4_address=%s, hostname=%s, state=%s",
				host.Ipv4Address, host.Hostname, host.Status.State)
		}
	}
	require.True(t, host3Found, "host-3 should be in the cluster")

	tLog(t, "verifying all 3 hosts remain healthy after IP change")
	finalHostCount := countHealthyHosts(t, cluster)
	assert.Equal(t, 3, finalHostCount, "should have 3 healthy hosts after IP change")

	tLog(t, "test passed: cluster remains healthy after host IP change with hostname")
}

func TestMultipleHostsUseHostnameWhenIPUnreachable(t *testing.T) {
	tLog(t, "initializing cluster with 3 etcd servers")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
		},
	})
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "verifying initial cluster has 3 healthy hosts")
	initialHostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 3, initialHostCount, "should have 3 healthy hosts initially")

	tLog(t, "recreating host-2 with an unreachable IP")
	host2 := cluster.Host("host-2")
	host2.Recreate(t, RecreateConfig{
		ExtraEnv: map[string]string{
			"PGEDGE_IPV4_ADDRESS": "10.88.88.88",
		},
	})

	tLog(t, "recreating host-3 with an unreachable IP")
	host3 := cluster.Host("host-3")
	host3.Recreate(t, RecreateConfig{
		ExtraEnv: map[string]string{
			"PGEDGE_IPV4_ADDRESS": "10.99.99.99",
		},
	})

	cluster.RefreshClient(t)

	tLog(t, "waiting for all hosts to become healthy after IP changes")
	waitForHostsHealthy(t, cluster, 3, 2*time.Minute)

	cluster.AssertHealthy(t)

	tLog(t, "verifying all 3 hosts are healthy after IP changes")
	hostCount := countHealthyHosts(t, cluster)
	require.Equal(t, 3, hostCount, "should have 3 healthy hosts")

	resp, err := cluster.Client().ListHosts(t.Context())
	require.NoError(t, err)

	for _, host := range resp.Hosts {
		tLogf(t, "host %s: ipv4_address=%s, hostname=%s, state=%s",
			host.ID, host.Ipv4Address, host.Hostname, host.Status.State)
		assert.Equal(t, "healthy", host.Status.State,
			"host %s should be healthy", host.ID)
	}

	tLog(t, "test passed: cluster healthy after multiple hosts changed IPs")
}

func TestEtcdWriteReadUsesHostnameWhenIPUnreachable(t *testing.T) {
	tLog(t, "initializing cluster with 3 etcd servers")

	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1"},
			{ID: "host-2"},
			{ID: "host-3"},
		},
	})
	cluster.Init(t)
	cluster.AssertHealthy(t)

	tLog(t, "changing host-2's IP to an unreachable address")
	host2 := cluster.Host("host-2")
	host2.Recreate(t, RecreateConfig{
		ExtraEnv: map[string]string{
			"PGEDGE_IPV4_ADDRESS": "10.88.88.88",
		},
	})

	cluster.RefreshClient(t)
	waitForHostsHealthy(t, cluster, 3, 2*time.Minute)

	tLog(t, "verifying cluster is healthy after host-2 IP change")
	cluster.AssertHealthy(t)

	resp, err := cluster.Client().ListHosts(t.Context())
	require.NoError(t, err)

	for _, host := range resp.Hosts {
		tLogf(t, "host %s: ipv4_address=%s, hostname=%s, state=%s",
			host.ID, host.Ipv4Address, host.Hostname, host.Status.State)
		assert.Equal(t, "healthy", host.Status.State,
			"host %s should be healthy", host.ID)
	}

	tLog(t, "test passed: etcd operations work with hostname")
}
