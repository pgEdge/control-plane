package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

// AddNode returns a sequence of diffs to add the given node, where each state
// only contains changed resources from the previous state. These states should
// be cumulatively merged with previous states to produce the sequence of
// desired states. This function always creates primary instances first and
// creates replica instances simultaneously.
func AddNode(node *NodeResources) ([]*resource.State, error) {
	if len(node.InstanceResources) == 0 {
		return nil, fmt.Errorf("got empty instances for node %s", node.NodeName)
	}

	states := make([]*resource.State, 0, 2)

	primary, err := instanceState(node.InstanceResources[0])
	if err != nil {
		return nil, err
	}
	states = append(states, primary)

	var replicas *resource.State
	for _, inst := range node.InstanceResources[1:] {
		replica, err := instanceState(inst)
		if err != nil {
			return nil, fmt.Errorf("failed to compute replica instance resource state: %w", err)
		}
		if replicas == nil {
			replicas = replica
		} else {
			replicas.Merge(replica)
		}
	}

	if replicas != nil {
		states = append(states, replicas)
	}

	nodeState, err := node.nodeResourceState()
	if err != nil {
		return nil, err
	}
	states[len(states)-1].Merge(nodeState)

	return states, nil
}

// AddNodes returns a sequence of state diffs where each of the given nodes are
// added simultaneously. This function retains the primary-first order from
// AddNode, so primary instances are created simultaneously, followed by a
// single state for all replica instances.
func AddNodes(new []*NodeResources) ([]*resource.State, error) {
	all := make([][]*resource.State, len(new))
	for i, node := range new {
		states, err := AddNode(node)
		if err != nil {
			return nil, err
		}

		all[i] = states
	}

	return mergePartialStates(all), nil
}
