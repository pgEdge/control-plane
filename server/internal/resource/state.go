package resource

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

var (
	StateVersion_1_0_0 = ds.MustParseVersion("1.0.0")
	StateVersion_1_1_0 = ds.MustParseVersion("1.1.0")

	CurrentVersion = StateVersion_1_1_0
)

var (
	ErrStateNeedsUpgrade        = errors.New("state needs to be upgraded")
	ErrControlPlaneNeedsUpgrade = errors.New("control plane upgrade required: cannot operate on a state produced by a newer control plane version")
)

type State struct {
	Version   *ds.Version                       `json:"version"`
	Resources map[Type]map[string]*ResourceData `json:"resources"`
}

func NewState() *State {
	return &State{
		Version:   CurrentVersion.Clone(),
		Resources: make(map[Type]map[string]*ResourceData),
	}
}

func (s *State) ValidateVersion() error {
	if s.Version == nil {
		s.Version = &ds.Version{}
	}
	comparison := CurrentVersion.Compare(s.Version)
	switch {
	case comparison < 0:
		return ErrControlPlaneNeedsUpgrade
	case comparison > 0:
		return ErrStateNeedsUpgrade
	default:
		return nil
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

	var version *ds.Version
	if s.Version != nil {
		version = s.Version.Clone()
	}

	return &State{
		Version:   version,
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

// MarkPendingDeletion takes an end state and marks all current resources that
// aren't in the end state with PendingDeletion = true.
func (s *State) MarkPendingDeletion(end *State) {
	for t, byType := range s.Resources {
		endByType, ok := end.Resources[t]
		if !ok {
			for _, resource := range byType {
				resource.PendingDeletion = true
			}
			continue
		}
		for id, resource := range byType {
			if _, ok := endByType[id]; !ok {
				resource.PendingDeletion = true
			}
		}
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
	nodeIDsByType := map[Type][]int64{}
	g := simple.NewDirectedGraph()
	currID := int64(1)
	// First pass to add nodes
	for _, resources := range s.Resources {
		for _, resource := range resources {
			nodeIDs[resource.Identifier] = currID
			nodeIDsByType[resource.Identifier.Type] = append(nodeIDsByType[resource.Identifier.Type], currID)
			g.AddNode(&node{
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
			to := g.Node(toID)
			for _, dep := range resource.Dependencies {
				if dep == resource.Identifier {
					return nil, fmt.Errorf("invalid dependency: resource '%s' cannot depend on itself", resource.Identifier)
				}
				fromID, ok := nodeIDs[dep]
				from := g.Node(fromID)
				if !ok {
					if opts.ignoreMissingDeps {
						continue
					} else {
						return nil, fmt.Errorf("dependency of %s not found: %s", resource.Identifier, dep)
					}
				}
				addEdge(opts, g, from, to)
			}
			for _, ty := range resource.TypeDependencies {
				if ty == resource.Identifier.Type {
					return nil, fmt.Errorf("invalid type dependency: resource '%s' cannot depend on its own type '%s'", resource.Identifier, ty)
				}
				for _, fromID := range nodeIDsByType[ty] {
					from := g.Node(fromID)
					addEdge(opts, g, from, to)
				}
			}
		}
	}
	return g, nil
}

func addEdge(opts graphOptions, g *simple.DirectedGraph, from, to graph.Node) {
	// Our layered topological sort returns in 'from' to 'to' order.
	// So modeling from dependency to dependent gets us the order we
	// want for creates and updates.
	if opts.creationOrdered {
		g.SetEdge(simple.Edge{
			T: to,
			F: from,
		})
	} else {
		// For deletion order we need to reverse the edge.
		g.SetEdge(simple.Edge{
			T: from,
			F: to,
		})
	}
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
	modifiedTypes := ds.NewSet[Type]()
	for layer := range layers {
		var phase []*Event

		for _, resource := range layer {
			var event *Event

			currentResource, ok := s.Get(resource.Identifier)
			switch {
			case !ok:
				event = &Event{
					Type:     EventTypeCreate,
					Resource: resource,
					Reason:   EventReasonDoesNotExist,
				}
			case currentResource.PendingDeletion:
				// Skip create/update for resources that are pending deletion.
				continue
			case currentResource.NeedsRecreate:
				event = &Event{
					Type:     EventTypeCreate,
					Resource: resource,
					Reason:   EventReasonNeedsRecreate,
				}
			case currentResource.Error != "":
				event = &Event{
					Type:     EventTypeUpdate,
					Resource: resource,
					Reason:   EventReasonHasError,
				}
			case options.ForceUpdate:
				event = &Event{
					Type:     EventTypeUpdate,
					Resource: resource,
					Reason:   EventReasonForceUpdate,
				}
			case slices.ContainsFunc(resource.TypeDependencies, modifiedTypes.Has), slices.ContainsFunc(resource.Dependencies, modified.Has):
				event = &Event{
					Type:     EventTypeUpdate,
					Resource: resource,
					Reason:   EventReasonDependencyUpdated,
				}
			default:
				diff, err := currentResource.Diff(resource)
				if err != nil {
					return nil, fmt.Errorf("failed to compare resource %s: %w", resource.Identifier, err)
				}
				if diff != nil {
					event = &Event{
						Type:     EventTypeUpdate,
						Resource: resource,
						Reason:   EventReasonHasDiff,
						Diff:     diff,
					}
				}
			}

			if event != nil {
				phase = append(phase, event)
				modified.Add(resource.Identifier)
				modifiedTypes.Add(resource.Identifier.Type)
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

func (s *State) HasResources(identifiers ...Identifier) bool {
	for _, identifier := range identifiers {
		_, ok := s.Get(identifier)
		if !ok {
			return false
		}
	}
	return true
}

func FromState[T Resource](state *State, identifier Identifier) (T, error) {
	var zero T
	data, ok := state.Get(identifier)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrNotFound, identifier.String())
	}
	if data.NeedsRecreate {
		return zero, fmt.Errorf("%w: %s needs recreate", ErrNotFound, identifier.String())
	}
	return ToResource[T](data)
}

func AllFromState[T Resource](state *State, resourceType Type) ([]T, error) {
	data := state.GetAll(resourceType)
	all := make([]T, len(data))
	for i, d := range data {
		resource, err := ToResource[T](d)
		if err != nil {
			return nil, err
		}
		all[i] = resource
	}

	return all, nil
}

func FromContext[T Resource](rc *Context, identifier Identifier) (T, error) {
	return FromState[T](rc.State, identifier)
}

func AllFromContext[T Resource](rc *Context, resourceType Type) ([]T, error) {
	return AllFromState[T](rc.State, resourceType)
}
