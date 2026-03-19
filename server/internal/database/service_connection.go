package database

import (
	"errors"
	"fmt"
)

// internalPostgresPort is the Postgres port inside the container, used for
// service-to-database connections over the overlay network. This is always
// 5432 regardless of the published host port.
const internalPostgresPort = 5432

// libpq target_session_attrs values (PG 14+).
// See: https://www.postgresql.org/docs/current/libpq-connect.html
const (
	TargetSessionAttrsPrimary       = "primary"
	TargetSessionAttrsPreferStandby = "prefer-standby"
	TargetSessionAttrsStandby       = "standby"
	TargetSessionAttrsReadWrite     = "read-write"
	TargetSessionAttrsAny           = "any"
)

// ServiceHostEntry represents a single host:port pair in an ordered host list.
type ServiceHostEntry struct {
	Host string
	Port int
}

// ServiceConnectionInfo holds the ordered host list and connection parameters
// for a service instance's database connection.
type ServiceConnectionInfo struct {
	Hosts              []ServiceHostEntry
	TargetSessionAttrs string
}

// BuildServiceHostListParams holds the inputs for BuildServiceHostList.
type BuildServiceHostListParams struct {
	ServiceHostID      string           // Host where the service instance runs
	NodeInstances      []*NodeInstances // All database instances, grouped by node
	TargetNodes        []string         // Optional ordered node filter (from database_connection.target_nodes)
	TargetSessionAttrs string           // Caller-provided: "primary", "prefer-standby", etc.
}

// BuildServiceHostList produces an ordered list of database host:port entries
// for a service instance's connection string. The ordering is determined by
// co-location with the service host and optional node filtering.
//
// Algorithm:
//  1. Determine node list and ordering:
//     - If TargetNodes is set: use only listed nodes in that order, ignoring co-location
//     - If TargetNodes is not set: all nodes, with the local node (containing a
//     co-located instance) first, then remaining nodes in iteration order
//  2. Build host list, grouped by node:
//     - For each node group: co-located instance first (same host as service), then remaining
//     - Hostname format: "postgres-{instanceID}" (swarm overlay convention)
//     - Port: always 5432 (internal container port via overlay network)
//  3. Pass through TargetSessionAttrs unchanged.
//
// Invariant: Only database instances from NodeInstances generate entries.
// ServiceHostID affects ordering only, never membership. A service on a
// dedicated host (no database instance on that host) does not add the service
// host to the list.
func BuildServiceHostList(params *BuildServiceHostListParams) (*ServiceConnectionInfo, error) {
	nodesByName := make(map[string]*NodeInstances, len(params.NodeInstances))
	for _, ni := range params.NodeInstances {
		nodesByName[ni.NodeName] = ni
	}

	// Determine the ordered list of nodes to include.
	var orderedNodes []*NodeInstances
	if len(params.TargetNodes) > 0 {
		// TargetNodes mode: use only listed nodes in the caller-specified order.
		orderedNodes = make([]*NodeInstances, 0, len(params.TargetNodes))
		for _, name := range params.TargetNodes {
			ni, ok := nodesByName[name]
			if !ok {
				return nil, fmt.Errorf("target node %q does not exist in the database spec", name)
			}
			orderedNodes = append(orderedNodes, ni)
		}
	} else {
		// Default mode: all nodes, with the local node first.
		orderedNodes = make([]*NodeInstances, 0, len(params.NodeInstances))
		localIdx := -1
		for i, ni := range params.NodeInstances {
			if localIdx == -1 && containsHost(ni, params.ServiceHostID) {
				localIdx = i
			}
		}
		if localIdx >= 0 {
			orderedNodes = append(orderedNodes, params.NodeInstances[localIdx])
		}
		for i, ni := range params.NodeInstances {
			if i != localIdx {
				orderedNodes = append(orderedNodes, ni)
			}
		}
	}

	// Build the host list from the ordered nodes.
	var hosts []ServiceHostEntry
	for _, ni := range orderedNodes {
		hosts = append(hosts, buildNodeHosts(ni, params.ServiceHostID)...)
	}

	if len(hosts) == 0 {
		return nil, errors.New("no database instances found")
	}

	return &ServiceConnectionInfo{
		Hosts:              hosts,
		TargetSessionAttrs: params.TargetSessionAttrs,
	}, nil
}

// containsHost returns true if any instance in the node runs on the given host.
func containsHost(ni *NodeInstances, hostID string) bool {
	for _, inst := range ni.Instances {
		if inst.HostID == hostID {
			return true
		}
	}
	return false
}

// buildNodeHosts returns host entries for a single node, with the co-located
// instance (matching serviceHostID) first.
func buildNodeHosts(ni *NodeInstances, serviceHostID string) []ServiceHostEntry {
	var colocated []ServiceHostEntry
	var rest []ServiceHostEntry

	for _, inst := range ni.Instances {
		entry := ServiceHostEntry{
			Host: fmt.Sprintf("postgres-%s", inst.InstanceID),
			Port: internalPostgresPort,
		}
		if inst.HostID == serviceHostID {
			colocated = append(colocated, entry)
		} else {
			rest = append(rest, entry)
		}
	}

	return append(colocated, rest...)
}
