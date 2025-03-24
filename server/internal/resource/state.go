package resource

import (
	"bytes"
	"fmt"
	"iter"
	"maps"
	"slices"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"

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

func (s *State) Add(data *ResourceData) {
	resources, ok := s.Resources[data.Identifier.Type]
	if !ok {
		resources = make(map[string]*ResourceData)
	}
	resources[data.Identifier.ID] = data
	s.Resources[data.Identifier.Type] = resources
}

func (s *State) Remove(data *ResourceData) {
	resources, ok := s.Resources[data.Identifier.Type]
	if !ok {
		return
	}
	delete(resources, data.Identifier.ID)
	if len(resources) == 0 {
		delete(s.Resources, data.Identifier.Type)
	} else {
		s.Resources[data.Identifier.Type] = resources
	}
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

type node struct {
	id       int64
	resource *ResourceData
}

func (n *node) ID() int64 {
	return n.id
}

// CreationOrdered returns a sequence of resources in the order they should be
// created. In this order dependencies are returned before dependents.
func (s *State) CreationOrdered() (iter.Seq[*ResourceData], error) {
	graph, err := s.graph()
	if err != nil {
		return nil, err
	}
	sorted, err := topo.Sort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to sort resource graph: %w", err)
	}
	return func(yield func(data *ResourceData) bool) {
		for _, n := range sorted {
			resource := n.(*node).resource
			if !yield(resource) {
				return
			}
		}
	}, nil
}

// DeletionOrdered returns a sequence of resources in the order they should be
// deleted. In this order dependents are returned before dependencies.
func (s *State) DeletionOrdered() (iter.Seq[*ResourceData], error) {
	graph, err := s.graph()
	if err != nil {
		return nil, err
	}
	sorted, err := topo.Sort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to sort resource graph: %w", err)
	}
	return func(yield func(data *ResourceData) bool) {
		for i := len(sorted) - 1; i >= 0; i-- {
			n := sorted[i]
			resource := n.(*node).resource
			if !yield(resource) {
				return
			}
		}
	}, nil
}

func (s *State) graph() (*simple.DirectedGraph, error) {
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
					return nil, fmt.Errorf("dependency of %s not found: %s", resource.Identifier, dep)
				}
				// gonum's topological sort returns in 'from' to 'to' order.
				// So modeling from dependency to dependent gets us the order we
				// want for creates and updates.
				graph.SetEdge(simple.Edge{
					T: to,
					F: from,
				})
			}
		}
	}
	return graph, nil
}

func (s *State) PlanRefresh() ([]*Event, error) {
	resources, err := s.CreationOrdered()
	if err != nil {
		return nil, err
	}
	var events []*Event
	for resource := range resources {
		events = append(events, &Event{
			Type:     EventTypeRefresh,
			Resource: resource,
		})
	}
	return events, nil
}

func (s *State) Plan(desired *State, forceUpdate bool) ([]*Event, error) {
	desiredSorted, err := desired.CreationOrdered()
	if err != nil {
		return nil, err
	}
	var events []*Event
	// Keep track of updated resources so that we can update their dependents.
	updated := ds.NewSet[Identifier]()
	for resource := range desiredSorted {
		currentResource, ok := s.Get(resource.Identifier)
		if !ok || currentResource.NeedsCreate {
			events = append(events, &Event{
				Type:     EventTypeCreate,
				Resource: resource,
			})
		} else if forceUpdate || !bytes.Equal(currentResource.Attributes, resource.Attributes) {
			events = append(events, &Event{
				Type:     EventTypeUpdate,
				Resource: resource,
			})
			updated.Add(resource.Identifier)
		} else {
			// If one of this resource's dependencies has been updated, we need
			// to update it as well.
			if slices.ContainsFunc(resource.Dependencies, updated.Has) {
				events = append(events, &Event{
					Type:     EventTypeUpdate,
					Resource: resource,
				})
				updated.Add(resource.Identifier)
			}
		}
	}
	currentSorted, err := s.DeletionOrdered()
	if err != nil {
		return nil, err
	}
	// Go in reverse order for deletes so that dependents get deleted before
	// their dependencies.
	for resource := range currentSorted {
		_, ok := desired.Get(resource.Identifier)
		if !ok {
			events = append(events, &Event{
				Type:     EventTypeDelete,
				Resource: resource,
			})
		}
	}
	return events, nil
}

func (s *State) AddResource(resource Resource) error {
	data, err := ToResourceData(resource)
	if err != nil {
		return err
	}
	s.Add(data)
	return nil
}

func FromState[T Resource](state *State, registry *Registry, identifier Identifier) (T, error) {
	var zero T
	data, ok := state.Get(identifier)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrNotFound, identifier.String())
	}
	return TypedFromRegistry[T](registry, data)
}

func FromContext[T Resource](rc *Context, identifier Identifier) (T, error) {
	return FromState[T](rc.State, rc.Registry, identifier)
}
