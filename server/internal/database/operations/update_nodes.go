package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// UpdateNode returns a sequence of diffs to update the given node, where each
// state only contains changed resources from the previous state. These states
// should be cumulatively merged with previous states to produce the sequence of
// desired states. This function always updates the node's replica instances.
func UpdateNode(node *NodeResources) ([]*resource.State, error) {
	// Updates are performed on replica instances first
	var primary *resource.State

	var primaryHostID string
	instanceIDs := make([]string, len(node.InstanceResources))
	states := make([]*resource.State, 0, len(node.InstanceResources))
	for i, inst := range node.InstanceResources {
		instanceID := inst.InstanceID()
		instanceIDs[i] = instanceID

		state, err := instanceState(inst)
		if err != nil {
			return nil, err
		}
		if instanceID == node.PrimaryInstanceID {
			primary = state
			primaryHostID = inst.HostID()
		} else {
			states = append(states, state)
		}
	}
	if primary == nil {
		// TODO(PLAT-240): This is another place where we assume that a node has
		// a primary instance. We're returning an error here so that we don't
		// break downstream components that make the same assumption. We'll need
		// to change this if we want workflows to handle nodes without a primary
		// instance.
		return nil, fmt.Errorf("node %s has no primary instance", node.NodeName)
	}

	// This condition is true when we have replicas
	if len(states) != 0 {
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

	states = append(states, primary)
	err := addNodeResource(states, &database.NodeResource{
		Name:        node.NodeName,
		InstanceIDs: instanceIDs,
	})
	if err != nil {
		return nil, err
	}

	return states, nil
}

// RollingUpdateNodes returns a sequence of state diffs where each of the given
// nodes is updated in-order sequentially. This function retains the
// replica-first order from UpdateNode.
func RollingUpdateNodes(nodes []*NodeResources) ([]*resource.State, error) {
	// Updates each node sequentially
	var states []*resource.State
	for _, node := range nodes {
		update, err := UpdateNode(node)
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
func ConcurrentUpdateNodes(nodes []*NodeResources) ([]*resource.State, error) {
	// Updates each node simultaneously
	states := make([][]*resource.State, len(nodes))
	for i, node := range nodes {
		update, err := UpdateNode(node)
		if err != nil {
			return nil, err
		}
		states[i] = update
	}
	return mergePartialStates(states), nil
}
