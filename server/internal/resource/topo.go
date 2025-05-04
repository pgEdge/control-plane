package resource

import (
	"errors"

	"gonum.org/v1/gonum/graph"
)

// LayeredTopoSort performs a layered topological sort on a directed graph.
// It returns a slice of slices, where each inner slice is a set of nodes that
// can be processed concurrently. If a cycle is detected, it returns an error.
func layeredTopoSort(g graph.Directed) ([][]graph.Node, error) {
	inDegree := map[int64]int{}
	nodeMap := map[int64]graph.Node{}

	// Initialize in-degree map
	nodes := g.Nodes()
	for nodes.Next() {
		n := nodes.Node()
		nodeMap[n.ID()] = n
		inDegree[n.ID()] = 0
	}

	// Compute in-degree for each node
	nodes = g.Nodes()
	for nodes.Next() {
		n := nodes.Node()
		to := g.From(n.ID())
		for to.Next() {
			child := to.Node()
			inDegree[child.ID()]++
		}
	}

	// Initialize queue of ready-to-process nodes
	var ready []graph.Node
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, nodeMap[id])
		}
	}

	var phases [][]graph.Node
	processedCount := 0

	for len(ready) > 0 {
		current := ready
		ready = nil
		phases = append(phases, current)
		processedCount += len(current)

		for _, n := range current {
			children := g.From(n.ID())
			for children.Next() {
				child := children.Node()
				inDegree[child.ID()]--
				if inDegree[child.ID()] == 0 {
					ready = append(ready, child)
				}
			}
		}
	}

	// Check if all nodes were processed
	if processedCount < len(inDegree) {
		return nil, errors.New("cycle detected in graph: topological sort not possible")
	}

	return phases, nil
}
