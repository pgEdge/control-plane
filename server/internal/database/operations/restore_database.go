package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// NodeRestoreResources contains the restore resources for the primary instance
// as well as the end state for both the primary instance and the replica
// instances for the given node.
type NodeRestoreResources struct {
	NodeName         string
	PrimaryInstance  *database.InstanceResources
	RestoreInstance  *database.InstanceResources
	ReplicaInstances []*database.InstanceResources
}

func (n *NodeRestoreResources) ToNodeResources() *NodeResources {
	all := make([]*database.InstanceResources, 0, 1+len(n.ReplicaInstances))
	all = append(all, n.PrimaryInstance)
	all = append(all, n.ReplicaInstances...)

	return &NodeResources{
		NodeName:          n.NodeName,
		PrimaryInstanceID: n.PrimaryInstance.InstanceID(),
		InstanceResources: all,
	}
}

// RestoreNode returns a sequence of states that restore the given node. Unlike
// some of the other functions in this package, these states are not diffs, and
// each state should be merged onto the same base state. This is necessary to
// facilitate deletes during the process, such as deleting the node and instance
// resources before running the restore. See RestoreDatabase for how these
// states are used.
func RestoreNode(node *NodeRestoreResources) ([]*resource.State, error) {
	instanceIDs := make([]string, 0, 1+len(node.ReplicaInstances))
	states := make([]*resource.State, 0, 4)

	// The pre-restore state only contains the orchestrator resources.
	preRestoreState := resource.NewState()
	preRestoreState.Add(node.PrimaryInstance.Resources...)
	states = append(states, preRestoreState)
	instanceIDs = append(instanceIDs, node.PrimaryInstance.InstanceID())

	// The restore state has the restore resources, the instance and the
	// instance monitor.
	restoreState, err := instanceState(node.RestoreInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to compute state for restore resources: %w", err)
	}
	states = append(states, restoreState)

	postRestore, err := instanceState(node.PrimaryInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to compute post-restore state for primary instance: %w", err)
	}
	states = append(states, postRestore)

	for _, inst := range node.ReplicaInstances {
		instanceIDs = append(instanceIDs, inst.InstanceID())

		replica, err := instanceState(inst)
		if err != nil {
			return nil, fmt.Errorf("failed to compute post-restore state for replica instance: %w", err)
		}
		postRestore.Merge(replica)
	}

	err = addNodeResource(states, &database.NodeResource{
		Name:        node.NodeName,
		InstanceIDs: instanceIDs,
	})
	if err != nil {
		return nil, err
	}

	return states, nil
}

// RestoreDatabase returns a sequence of plans that will restore a database
// using the restore resources for each target node. Note that the `nodes`
// argument should only contain nodes that are _not_ being restored, and
// `targets` should only contain nodes that _are_ being restored. In other
// words, they should be disjoint with each other.
func RestoreDatabase(
	start *resource.State,
	nodes []*NodeResources,
	targets []*NodeRestoreResources,
) ([]resource.Plan, error) {
	// The other states will be applied on top of this base state.
	base, err := EndState(nodes)
	if err != nil {
		return nil, err
	}

	allNodes := make([]*NodeResources, 0, len(nodes)+len(targets))
	allNodes = append(allNodes, nodes...)

	all := make([][]*resource.State, len(targets))
	for i, target := range targets {
		allNodes = append(allNodes, target.ToNodeResources())

		states, err := RestoreNode(target)
		if err != nil {
			return nil, err
		}
		all[i] = states
	}

	states := mergePartialStates(all)
	for i, state := range states {
		merged := base.Clone()
		merged.Merge(state)

		states[i] = merged
	}

	end, err := EndState(allNodes)
	if err != nil {
		return nil, err
	}
	states = append(states, end)

	plans, err := start.PlanAll(resource.PlanOptions{}, states...)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plans: %w", err)
	}

	return plans, nil
}
