//go:build cluster_test

package clustertest

import (
	"testing"
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
