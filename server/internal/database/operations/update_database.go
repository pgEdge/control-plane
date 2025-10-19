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
	for _, n := range adds {
		if n.SourceNode == "" {
			continue
		}
		if n.SourceNode == n.NodeName {
			return nil, database.ErrInvalidSourceNode
		}
		ident := database.NodeResourceIdentifier(n.SourceNode)
		if _, err := resource.FromState[*database.NodeResource](start, ident); err != nil {
			if errors.Is(err, resource.ErrNotFound) {
				return nil, database.ErrInvalidSourceNode
			}
			return nil, err
		}
	}
	// Updates first to ensure an existing node is available as a source.
	var states []*resource.State
	if len(updates) > 0 {
		u, err := update(updates)
		if err != nil {
			return nil, err
		}
		states = append(states, u...)
	}

	// Auto-select source node for adds if not provided:
	// Pick the first existing node (from updates) as default.
	if len(adds) > 0 && len(updates) > 0 {
		defaultSource := updates[0].NodeName
		for _, n := range adds {
			if n.SourceNode == "" {
				n.SourceNode = defaultSource
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

	// Build incremental states by applying diffs in sequence.
	prev := start
	for i, state := range states {
		curr := prev.Clone()
		curr.Merge(state)
		states[i] = curr
		prev = curr
	}

	end, err := EndState(nodes)
	if err != nil {
		return nil, err
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
