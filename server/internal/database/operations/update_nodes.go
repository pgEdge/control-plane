package operations

import (
	"fmt"
	"slices"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// UpdateNode returns a sequence of diffs to update the given node, where each
// state only contains changed resources from the previous state. These states
// should be cumulatively merged with previous states to produce the sequence of
// desired states. This function always updates the node's replica instances.
func UpdateNode(start *resource.State, node *NodeResources) ([]*resource.State, error) {
	var primary *resource.State
	var replicaUpdates, replicaAdds []*resource.State
	var primaryHostID string

	for _, inst := range node.InstanceResources {
		state, err := inst.InstanceState()
		if err != nil {
			return nil, err
		}

		switch {
		case inst.InstanceID() == node.PrimaryInstanceID:
			if !start.HasResources(inst.Instance.Identifier()) {
				return nil, fmt.Errorf("invalid state: node %s exists, but its primary instance '%s' hasn't been created yet", node.NodeName, node.PrimaryInstanceID)
			}
			primary = state
			primaryHostID = inst.HostID()
		case start.HasResources(inst.Instance.Identifier()):
			replicaUpdates = append(replicaUpdates, state)
		default:
			replicaAdds = append(replicaAdds, state)
		}
	}
	if primary == nil {
		// TODO(PLAT-582): This is another place where we assume that a node has
		// a primary instance. We're returning an error here so that we don't
		// break downstream components that make the same assumption. We'll need
		// to change this if we want workflows to handle nodes without a primary
		// instance.
		return nil, fmt.Errorf("node %s has no primary instance", node.NodeName)
	}

	// This condition is true when we have existing replicas
	if len(replicaUpdates) != 0 {
		// Ensure that we always switch back to the original primary
		err := primary.AddResource(&database.SwitchoverResource{
			HostID:     primaryHostID,
			InstanceID: node.PrimaryInstanceID,
			TargetRole: patroni.InstanceRolePrimary,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add switchover resource to replica state: %w", err)
		}
	}

	// Existing replicas should be updated first and new replicas should be
	// added last.
	states := slices.Concat(replicaUpdates, []*resource.State{primary}, replicaAdds)

	nodeState, err := node.nodeResourceState()
	if err != nil {
		return nil, err
	}
	states[len(states)-1].Merge(nodeState)

	return states, nil
}

// RollingUpdateNodes returns a sequence of state diffs where each of the given
// nodes is updated in-order sequentially. This function retains the
// replica-first order from UpdateNode.
func RollingUpdateNodes(start *resource.State, nodes []*NodeResources) ([]*resource.State, error) {
	// Updates each node sequentially
	var states []*resource.State
	for _, node := range nodes {
		update, err := UpdateNode(start, node)
		if err != nil {
			return nil, err
		}
		states = append(states, update...)
	}
	return states, nil
}

// ConcurrentUpdateNodes returns a sequence of state diffs where each of the
// given nodes is updated simultaneously. This function retains the
// replica-first order from UpdateNode.
func ConcurrentUpdateNodes(start *resource.State, nodes []*NodeResources) ([]*resource.State, error) {
	// Updates each node simultaneously
	states := make([][]*resource.State, len(nodes))
	for i, node := range nodes {
		update, err := UpdateNode(start, node)
		if err != nil {
			return nil, err
		}
		states[i] = update
	}
	return mergePartialStates(states), nil
}
