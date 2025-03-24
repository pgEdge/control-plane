package resource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/samber/do"
)

var ErrNotFound = fmt.Errorf("resource not found")

func ProvideRegistry(i *do.Injector) {
	do.Provide(i, func(_ *do.Injector) (*Registry, error) {
		return NewRegistry(), nil
	})
}

type Type string

func (r Type) String() string {
	return string(r)
}

type Identifier struct {
	ID   string `json:"id"`
	Type Type   `json:"type"`
}

func (r Identifier) String() string {
	return fmt.Sprintf("%s::%s", r.Type, r.ID)
}

type ResourceData struct {
	NeedsCreate  bool            `json:"needs_create"`
	Executor     Executor        `json:"executor"`
	Identifier   Identifier      `json:"identifier"`
	Attributes   json.RawMessage `json:"attributes"`
	Dependencies []Identifier    `json:"dependencies"`
}

type Context struct {
	State    *State
	Registry *Registry
	Injector *do.Injector
}

type ExecutorType string

func (e ExecutorType) String() string {
	return string(e)
}

const (
	ExecutorTypeHost    ExecutorType = "host"
	ExecutorTypeNode    ExecutorType = "node"
	ExecutorTypeCluster ExecutorType = "cluster"
	ExecutorTypeCohort  ExecutorType = "cohort"
)

type Executor struct {
	Type ExecutorType `json:"type"`
	ID   string       `json:"id"`
}

type Resource interface {
	Executor() Executor
	Identifier() Identifier
	Dependencies() []Identifier
	Refresh(ctx context.Context, rc *Context) error
	Create(ctx context.Context, rc *Context) error
	Update(ctx context.Context, rc *Context) error
	Delete(ctx context.Context, rc *Context) error
}

func ToResourceData(resource Resource) (*ResourceData, error) {
	attributes, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource attributes: %w", err)
	}
	return &ResourceData{
		Executor:     resource.Executor(),
		Identifier:   resource.Identifier(),
		Attributes:   attributes,
		Dependencies: resource.Dependencies(),
	}, nil
}

type Registry struct {
	factories map[Type]func(data *ResourceData) (Resource, error)
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[Type]func(data *ResourceData) (Resource, error)),
	}
}

func (r *Registry) Resource(data *ResourceData) (Resource, error) {
	f, ok := r.factories[data.Identifier.Type]
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", data.Identifier.Type)
	}
	resource, err := f(data)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	return resource, nil
}

func RegisterResourceType[T Resource](registry *Registry, t Type) {
	registry.factories[t] = func(data *ResourceData) (Resource, error) {
		var resource T
		if err := json.Unmarshal(data.Attributes, &resource); err != nil {
			return resource, fmt.Errorf("failed to unmarshal resource attributes: %w", err)
		}
		return resource, nil
	}
}

func TypedFromRegistry[T Resource](registry *Registry, data *ResourceData) (T, error) {
	var zero T
	resource, err := registry.Resource(data)
	if err != nil {
		return zero, fmt.Errorf("failed to create resource: %w", err)
	}
	typed, ok := resource.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected resource type: %T", resource)
	}
	return typed, nil
}
