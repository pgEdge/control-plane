package database

import (
	"fmt"
	"testing"
)

func TestBuildServiceHostList(t *testing.T) {
	// Helper to build a minimal InstanceSpec for testing.
	inst := func(instanceID, hostID string) *InstanceSpec {
		return &InstanceSpec{
			InstanceID: instanceID,
			HostID:     hostID,
		}
	}

	// Helper to build a ServiceHostEntry.
	he := func(instanceID string) ServiceHostEntry {
		return ServiceHostEntry{
			Host: fmt.Sprintf("postgres-%s", instanceID),
			Port: 5432,
		}
	}

	t.Run("standard multi-active no replicas", func(t *testing.T) {
		// 2 nodes, 1 host each. Service on host-1 -> local node first.
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{inst("db1-n1-h1", "host-1")}},
				{NodeName: "n2", Instances: []*InstanceSpec{inst("db1-n2-h2", "host-2")}},
			},
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []ServiceHostEntry{
			he("db1-n1-h1"), // local node first
			he("db1-n2-h2"), // remote node second
		}
		assertHostsEqual(t, expected, result.Hosts)
	})

	t.Run("HA within node", func(t *testing.T) {
		// Node with 2 hosts. Service on host-2 -> co-located instance first
		// within the local node group.
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-2",
			NodeInstances: []*NodeInstances{
				{
					NodeName: "n1",
					Instances: []*InstanceSpec{
						inst("db1-n1-h1", "host-1"),
						inst("db1-n1-h2", "host-2"),
					},
				},
			},
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []ServiceHostEntry{
			he("db1-n1-h2"), // co-located first
			he("db1-n1-h1"), // other instance in same node
		}
		assertHostsEqual(t, expected, result.Hosts)
	})

	t.Run("dedicated service host", func(t *testing.T) {
		// Service on a host with no database instances -> all instances
		// included, no local-first reordering. The service host must NOT
		// appear in the host list.
		params := &BuildServiceHostListParams{
			ServiceHostID: "service-host",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{inst("db1-n1-h1", "host-1")}},
				{NodeName: "n2", Instances: []*InstanceSpec{inst("db1-n2-h2", "host-2")}},
			},
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// No local-first reordering (service host has no instances), so
		// iteration order is preserved.
		expected := []ServiceHostEntry{
			he("db1-n1-h1"),
			he("db1-n2-h2"),
		}
		assertHostsEqual(t, expected, result.Hosts)

		// Assert the dedicated service host does NOT appear in any entry.
		for _, h := range result.Hosts {
			if h.Host == "postgres-service-host" {
				t.Error("service host should not appear in the host list")
			}
		}
	})

	t.Run("3-node multi-active plus HA", func(t *testing.T) {
		// 3 nodes: n1 has 2 hosts, n2 has 1 host, n3 has 1 host.
		// Service on host-1b (second host of n1).
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1b",
			NodeInstances: []*NodeInstances{
				{
					NodeName: "n1",
					Instances: []*InstanceSpec{
						inst("db1-n1-h1a", "host-1a"),
						inst("db1-n1-h1b", "host-1b"),
					},
				},
				{NodeName: "n2", Instances: []*InstanceSpec{inst("db1-n2-h2", "host-2")}},
				{NodeName: "n3", Instances: []*InstanceSpec{inst("db1-n3-h3", "host-3")}},
			},
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// n1 first (local), with co-located instance first within n1.
		// Then n2, n3 in iteration order.
		expected := []ServiceHostEntry{
			he("db1-n1-h1b"), // co-located in local node
			he("db1-n1-h1a"), // other instance in local node
			he("db1-n2-h2"),  // remote node
			he("db1-n3-h3"),  // remote node
		}
		assertHostsEqual(t, expected, result.Hosts)
	})

	t.Run("with TargetNodes", func(t *testing.T) {
		// Filter to subset of nodes; user-specified order wins over co-location
		// for node ordering. Within each node, co-location still applies.
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{inst("db1-n1-h1", "host-1")}},
				{NodeName: "n2", Instances: []*InstanceSpec{inst("db1-n2-h2", "host-2")}},
				{NodeName: "n3", Instances: []*InstanceSpec{inst("db1-n3-h3", "host-3")}},
			},
			TargetNodes: []string{"n3", "n1"}, // user-specified order: n3 first, n1 second
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// n3 first (user-specified), then n1. n2 excluded entirely.
		expected := []ServiceHostEntry{
			he("db1-n3-h3"),
			he("db1-n1-h1"),
		}
		assertHostsEqual(t, expected, result.Hosts)
	})

	t.Run("TargetNodes with non-existent node", func(t *testing.T) {
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{inst("db1-n1-h1", "host-1")}},
			},
			TargetNodes: []string{"n1", "does-not-exist"},
		}

		_, err := BuildServiceHostList(params)
		if err == nil {
			t.Fatal("expected error for non-existent target node, got nil")
		}
	})

	t.Run("target_session_attrs passthrough", func(t *testing.T) {
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{inst("db1-n1-h1", "host-1")}},
			},
			TargetSessionAttrs: TargetSessionAttrsPreferStandby,
		}

		result, err := BuildServiceHostList(params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TargetSessionAttrs != TargetSessionAttrsPreferStandby {
			t.Errorf("TargetSessionAttrs = %q, want %q", result.TargetSessionAttrs, TargetSessionAttrsPreferStandby)
		}
	})

	t.Run("empty node instances", func(t *testing.T) {
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{},
		}

		_, err := BuildServiceHostList(params)
		if err == nil {
			t.Fatal("expected error for empty node instances, got nil")
		}
	})

	t.Run("nil node instances", func(t *testing.T) {
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: nil,
		}

		_, err := BuildServiceHostList(params)
		if err == nil {
			t.Fatal("expected error for nil node instances, got nil")
		}
	})

	t.Run("nodes with empty instances slice", func(t *testing.T) {
		// Nodes exist but have no instances in them.
		params := &BuildServiceHostListParams{
			ServiceHostID: "host-1",
			NodeInstances: []*NodeInstances{
				{NodeName: "n1", Instances: []*InstanceSpec{}},
			},
		}

		_, err := BuildServiceHostList(params)
		if err == nil {
			t.Fatal("expected error when all nodes have empty instances, got nil")
		}
	})
}

// assertHostsEqual compares two slices of ServiceHostEntry for equality.
func assertHostsEqual(t *testing.T, expected, actual []ServiceHostEntry) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("host list length = %d, want %d\n  got:  %v\n  want: %v", len(actual), len(expected), actual, expected)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("host[%d] = %v, want %v", i, actual[i], expected[i])
		}
	}
}
