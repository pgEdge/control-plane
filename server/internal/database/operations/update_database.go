package operations

import (
	"errors"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// NodeUpdateStrategy determines the order that nodes are updated within a
// database.
type NodeUpdateStrategy string

const (
	// NodeUpdateStrategyRolling will update nodes sequentially, meaning that
	// downtime is limited to a single node.
	NodeUpdateStrategyRolling = "rolling"
	// NodeUpdateStrategyConcurrent will update nodes simultaneously, which will
	// minimize runtime, but could result in database-wide downtime.
	NodeUpdateStrategyConcurrent = "concurrent"
)

type UpdateDatabaseOptions struct {
	NodeUpdateStrategy NodeUpdateStrategy
	PlanOptions        resource.PlanOptions
}

// UpdateDatabase returns a sequence of plans that will update a database to
// match the nodes in the `nodes` argument. The plans always use the same order:
// - Update existing nodes
// - Add new nodes
// - Populate new nodes
// - Delete nodes and other extraneous resources
func UpdateDatabase(
	options UpdateDatabaseOptions,
	start *resource.State,
	nodes []*NodeResources,
) ([]resource.Plan, error) {
	update, err := updateFunc(options)
	if err != nil {
		return nil, err
	}

	updates, adds, err := partitionNodes(start, nodes)
	if err != nil {
		return nil, err
	}

	for _, n := range nodes {
		if n.RestoreConfig != nil && n.SourceNode != "" {
			return nil, database.ErrInvalidSourceNode
		}
	}

	// Updates are always performed first to guarantee that any existing node
	// is available to be a source node.
	var states []*resource.State
	if len(updates) > 0 {
		u, err := update(updates)
		if err != nil {
			return nil, err
		}
		states = append(states, u...)
	}

	// Auto-select source node ONLY when both SourceNode and RestoreConfig are empty.
	// If no existing nodes (fresh cluster), skip auto-select (don’t error).
	if len(adds) > 0 {
		var defaultSource string
		if len(updates) > 0 {
			defaultSource = updates[0].NodeName
		}
		for _, n := range adds {
			if n.SourceNode == "" && n.RestoreConfig == nil && defaultSource != "" {
				n.SourceNode = defaultSource
			}
		}
	}

	// New rule: new nodes cannot use other new nodes as their source.
	// Only existing nodes (updates) are valid source_node values for added nodes.
	if len(adds) > 0 {
		newNames := make(map[string]struct{}, len(adds))
		for _, n := range adds {
			newNames[n.NodeName] = struct{}{}
		}

		for _, n := range adds {
			if n.SourceNode == "" {
				continue
			}
			if _, isNew := newNames[n.SourceNode]; isNew {
				return nil, database.ErrInvalidSourceNode
			}
		}
	}

	if len(adds) > 0 {
		a, err := AddNodes(adds)
		if err != nil {
			return nil, err
		}
		states = append(states, a...)

		populate, err := PopulateNodes(updates, adds)
		if err != nil {
			return nil, err
		}
		if populate != nil {
			states = append(states, populate)
		}
	}

	end, err := EndState(nodes)
	if err != nil {
		return nil, err
	}
	// Mark resources not in the end state with PendingDeletion = true so that
	// we skip updating them.
	start.MarkPendingDeletion(end)

	// The states produced by the *Nodes functions are just diffs. Here's where
	// we create a sequence of incremental updates by iteratively applying those
	// diffs.
	prev := start
	for i, state := range states {
		// Clone the previous state and apply our diff on top of it
		curr := prev.Clone()
		curr.Merge(state)
		// Write the updated state back to our states slice.
		states[i] = curr
		prev = curr
	}
	states = append(states, end)

	plans, err := start.PlanAll(options.PlanOptions, states...)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plans: %w", err)
	}

	return plans, nil
}

func partitionNodes(start *resource.State, nodes []*NodeResources) ([]*NodeResources, []*NodeResources, error) {
	var updates []*NodeResources
	var adds []*NodeResources

	for _, node := range nodes {
		ident := database.NodeResourceIdentifier(node.NodeName)
		existing, err := resource.FromState[*database.NodeResource](start, ident)
		switch {
		case errors.Is(err, resource.ErrNotFound):
			adds = append(adds, node)
		case err != nil:
			return nil, nil, fmt.Errorf("failed to check for node %s in current state: %w", node.NodeName, err)
		default:
			node.PrimaryInstanceID = existing.PrimaryInstanceID
			updates = append(updates, node)
		}
	}

	return updates, adds, nil
}

func updateFunc(options UpdateDatabaseOptions) (func([]*NodeResources) ([]*resource.State, error), error) {
	switch options.NodeUpdateStrategy {
	case "", NodeUpdateStrategyRolling:
		return RollingUpdateNodes, nil
	case NodeUpdateStrategyConcurrent:
		return ConcurrentUpdateNodes, nil
	default:
		return nil, fmt.Errorf("unrecognized node update strategy %s", options.NodeUpdateStrategy)
	}
}
