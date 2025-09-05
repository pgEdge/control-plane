package resource

import (
	"fmt"
	"iter"
	"maps"
	"slices"

	"gonum.org/v1/gonum/graph/simple"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

type EventType string

const (
	EventTypeRefresh EventType = "refresh"
	EventTypeCreate  EventType = "create"
	EventTypeUpdate  EventType = "update"
	EventTypeDelete  EventType = "delete"
)

type Event struct {
	Type     EventType     `json:"type"`
	Resource *ResourceData `json:"resource"`
}

type State struct {
	Resources map[Type]map[string]*ResourceData `json:"resources"`
}

func NewState() *State {
	return &State{
		Resources: make(map[Type]map[string]*ResourceData),
	}
}

func (s *State) Add(data ...*ResourceData) {
	for _, d := range data {
		resources, ok := s.Resources[d.Identifier.Type]
		if !ok {
			resources = make(map[string]*ResourceData)
		}
		resources[d.Identifier.ID] = d
		s.Resources[d.Identifier.Type] = resources
	}
}

func (s *State) RemoveByIdentifier(identifier Identifier) {
	resources, ok := s.Resources[identifier.Type]
	if !ok {
		return
	}
	delete(resources, identifier.ID)
	if len(resources) == 0 {
		delete(s.Resources, identifier.Type)
	} else {
		s.Resources[identifier.Type] = resources
	}
}

func (s *State) Remove(data *ResourceData) {
	s.RemoveByIdentifier(data.Identifier)
}

func (s *State) Get(identifier Identifier) (*ResourceData, bool) {
	resources, ok := s.Resources[identifier.Type]
	if !ok {
		return nil, false
	}
	resource, ok := resources[identifier.ID]
	if !ok {
		return nil, false
	}
	return resource, true
}

func (s *State) GetAll(resourceType Type) []*ResourceData {
	resources := s.Resources[resourceType]
	return slices.Collect(maps.Values(resources))
}

func (s *State) Apply(event *Event) error {
	switch event.Type {
	case EventTypeRefresh, EventTypeCreate, EventTypeUpdate:
		s.Add(event.Resource)
	case EventTypeDelete:
		s.Remove(event.Resource)
	default:
		return fmt.Errorf("unknown event type: %s", event.Type)
	}
	return nil
}

func (s *State) Clone() *State {
	resources := make(map[Type]map[string]*ResourceData, len(s.Resources))
	for t, byID := range s.Resources {
		resources[t] = make(map[string]*ResourceData, len(byID))
		for i, resource := range byID {
			resources[t][i] = resource.Clone()
		}
	}

	return &State{
		Resources: resources,
	}
}

func (s *State) Merge(other *State) {
	for t, byID := range other.Resources {
		if _, ok := s.Resources[t]; !ok {
			s.Resources[t] = make(map[string]*ResourceData)
		}
		maps.Copy(s.Resources[t], byID)
	}
}

type node struct {
	id       int64
	resource *ResourceData
}

func (n *node) ID() int64 {
	return n.id
}

// CreationOrdered returns a sequence of resources in the order they should be
// created. In this order dependencies are returned before dependents.
func (s *State) CreationOrdered(ignoreMissingDeps bool) (iter.Seq[[]*ResourceData], error) {
	return s.topoIter(graphOptions{
		ignoreMissingDeps: ignoreMissingDeps,
		creationOrdered:   true,
	})
}

// DeletionOrdered returns a sequence of resources in the order they should be
// deleted. In this order dependents are returned before dependencies.
func (s *State) DeletionOrdered(ignoreMissingDeps bool) (iter.Seq[[]*ResourceData], error) {
	return s.topoIter(graphOptions{
		ignoreMissingDeps: ignoreMissingDeps,
		creationOrdered:   false,
	})
}

type graphOptions struct {
	ignoreMissingDeps bool
	creationOrdered   bool
}

func (s *State) topoIter(opts graphOptions) (iter.Seq[[]*ResourceData], error) {
	graph, err := s.graph(opts)
	if err != nil {
		return nil, err
	}
	sorted, err := layeredTopoSort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to sort resource graph: %w", err)
	}
	return func(yield func(data []*ResourceData) bool) {
		for _, layer := range sorted {
			resources := make([]*ResourceData, len(layer))
			for i, n := range layer {
				resource := n.(*node).resource
				resources[i] = resource
			}
			if !yield(resources) {
				return
			}
		}
	}, nil
}

func (s *State) graph(opts graphOptions) (*simple.DirectedGraph, error) {
	nodeIDs := map[Identifier]int64{}
	graph := simple.NewDirectedGraph()
	currID := int64(1)
	// First pass to add nodes
	for _, resources := range s.Resources {
		for _, resource := range resources {
			nodeIDs[resource.Identifier] = currID
			graph.AddNode(&node{
				id:       currID,
				resource: resource,
			})
			currID++
		}
	}
	// second pass to add edges
	for _, resources := range s.Resources {
		for _, resource := range resources {
			toID := nodeIDs[resource.Identifier]
			to := graph.Node(toID)
			for _, dep := range resource.Dependencies {
				fromID, ok := nodeIDs[dep]
				from := graph.Node(fromID)
				if !ok {
					if opts.ignoreMissingDeps {
						continue
					} else {
						return nil, fmt.Errorf("dependency of %s not found: %s", resource.Identifier, dep)
					}
				}
				// Our layered topological sort returns in 'from' to 'to' order.
				// So modeling from dependency to dependent gets us the order we
				// want for creates and updates.
				if opts.creationOrdered {
					graph.SetEdge(simple.Edge{
						T: to,
						F: from,
					})
				} else {
					// For deletion order we need to reverse the edge.
					graph.SetEdge(simple.Edge{
						T: from,
						F: to,
					})
				}
			}
		}
	}
	return graph, nil
}

func (s *State) PlanRefresh() (Plan, error) {
	layers, err := s.CreationOrdered(true)
	if err != nil {
		return nil, err
	}
	var plan Plan
	for layer := range layers {
		phase := make([]*Event, len(layer))
		for i, resource := range layer {
			phase[i] = &Event{
				Type:     EventTypeRefresh,
				Resource: resource,
			}
		}
		plan = append(plan, phase)
	}
	return plan, nil
}

type PlanOptions struct {
	ForceUpdate bool
}

type Plan [][]*Event

func (s *State) Plan(options PlanOptions, desired *State) (Plan, error) {
	creates, err := s.planCreates(options, desired)
	if err != nil {
		return nil, err
	}
	deletes, err := s.planDeletes(desired)
	if err != nil {
		return nil, err
	}

	return append(creates, deletes...), nil
}

func (s *State) planCreates(options PlanOptions, desired *State) (Plan, error) {
	layers, err := desired.CreationOrdered(false)
	if err != nil {
		return nil, err
	}
	var plan Plan

	// Keeps track of all modified resources so that we can update their
	// dependents.
	modified := ds.NewSet[Identifier]()
	for layer := range layers {
		var phase []*Event

		for _, resource := range layer {
			var event *Event

			currentResource, ok := s.Get(resource.Identifier)
			if !ok || currentResource.NeedsCreate {
				event = &Event{
					Type:     EventTypeCreate,
					Resource: resource,
				}
			} else if options.ForceUpdate || slices.ContainsFunc(resource.Dependencies, modified.Has) {
				event = &Event{
					Type:     EventTypeUpdate,
					Resource: resource,
				}
			} else {
				differs, err := resource.Differs(currentResource)
				if err != nil {
					return nil, fmt.Errorf("failed to compare resource %s: %w", resource.Identifier, err)
				}
				if differs {
					event = &Event{
						Type:     EventTypeUpdate,
						Resource: resource,
					}
				}
			}

			if event != nil {
				phase = append(phase, event)
				modified.Add(resource.Identifier)
			}
		}

		if len(phase) > 0 {
			plan = append(plan, phase)
		}
	}
	return plan, nil
}

func (s *State) planDeletes(desired *State) (Plan, error) {
	layers, err := s.DeletionOrdered(true)
	if err != nil {
		return nil, err
	}
	var plan Plan
	for layer := range layers {
		var phase []*Event

		for _, resource := range layer {
			if _, ok := desired.Get(resource.Identifier); ok {
				// This resource exists in the desired state, so we don't want to
				// delete it.
				continue
			}
			phase = append(phase, &Event{
				Type:     EventTypeDelete,
				Resource: resource,
			})
		}
		if len(phase) > 0 {
			plan = append(plan, phase)
		}
	}
	return plan, nil
}

func (s *State) PlanAll(options PlanOptions, new ...*State) ([]Plan, error) {
	var plans []Plan
	curr := s
	for i, state := range new {
		opts := PlanOptions{}
		if options.ForceUpdate && i == len(new)-1 {
			// We only want to apply the forced update to the final state.
			// Otherwise we'll end up with a huge number of redundant updates
			// in most scenarios.
			opts.ForceUpdate = true
		}

		plan, err := curr.Plan(opts, state)
		if err != nil {
			return nil, fmt.Errorf("error at state index %d: %w", i, err)
		}
		if len(plan) > 0 {
			// Only include non-empty plans
			plans = append(plans, plan)
		}
		curr = state
	}
	return plans, nil
}

func (s *State) AddResource(resources ...Resource) error {
	for _, r := range resources {
		data, err := ToResourceData(r)
		if err != nil {
			return err
		}
		s.Add(data)
	}
	return nil
}

func FromState[T Resource](state *State, identifier Identifier) (T, error) {
	var zero T
	data, ok := state.Get(identifier)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrNotFound, identifier.String())
	}
	return ToResource[T](data)
}

func FromContext[T Resource](rc *Context, identifier Identifier) (T, error) {
	return FromState[T](rc.State, identifier)
}
