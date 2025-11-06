package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/samber/do"
	"github.com/wI2L/jsondiff"
)

var ErrNotFound = errors.New("resource not found")

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
	NeedsRecreate   bool            `json:"needs_recreate"`
	Executor        Executor        `json:"executor"`
	Identifier      Identifier      `json:"identifier"`
	Attributes      json.RawMessage `json:"attributes"`
	Dependencies    []Identifier    `json:"dependencies"`
	DiffIgnore      []string        `json:"diff_ignore"`
	ResourceVersion string          `json:"resource_version"`
	PendingDeletion bool            `json:"pending_deletion"`
}

func (r *ResourceData) Diff(other *ResourceData) (jsondiff.Patch, error) {
	if r.ResourceVersion != other.ResourceVersion {
		return nil, nil
	}
	diff, err := jsondiff.CompareJSON(
		r.Attributes,
		other.Attributes,
		jsondiff.Ignores(r.DiffIgnore...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compare resource attributes: %w", err)
	}
	return diff, nil
}

func (r *ResourceData) Clone() *ResourceData {
	return &ResourceData{
		NeedsRecreate:   r.NeedsRecreate,
		Executor:        r.Executor,
		Identifier:      r.Identifier,
		Attributes:      slices.Clone(r.Attributes),
		Dependencies:    slices.Clone(r.Dependencies),
		DiffIgnore:      slices.Clone(r.DiffIgnore),
		ResourceVersion: r.ResourceVersion,
		PendingDeletion: r.PendingDeletion,
	}
}

func ToResource[T Resource](data *ResourceData) (T, error) {
	var resource T
	if err := json.Unmarshal(data.Attributes, &resource); err != nil {
		return resource, fmt.Errorf("failed to unmarshal resource attributes: %w", err)
	}
	return resource, nil
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
	ExecutorTypePrimary ExecutorType = "primary"
	ExecutorTypeAny     ExecutorType = "any"
	ExecutorTypeManager ExecutorType = "manager"
)

// Executor identifies where a resource's lifecycle methods should be executed.
type Executor struct {
	Type ExecutorType `json:"type"`
	ID   string       `json:"id"`
}

// HostExecutor will execute resource methods on the given host.
func HostExecutor(hostID string) Executor {
	return Executor{Type: ExecutorTypeHost, ID: hostID}
}

// PrimaryExecutor will execute resource methods on the host that's running the
// primary instance for the given node.
func PrimaryExecutor(nodeName string) Executor {
	return Executor{Type: ExecutorTypePrimary, ID: nodeName}
}

// AnyExecutor will execute resource methods on any host.
func AnyExecutor() Executor {
	return Executor{Type: ExecutorTypeAny}
}

// ManagerExecutor will execute resource methods on any host with cohort manager
// capabilities.
func ManagerExecutor() Executor {
	return Executor{Type: ExecutorTypeManager}
}

type Resource interface {
	Executor() Executor
	Identifier() Identifier
	Dependencies() []Identifier
	Refresh(ctx context.Context, rc *Context) error
	Create(ctx context.Context, rc *Context) error
	Update(ctx context.Context, rc *Context) error
	Delete(ctx context.Context, rc *Context) error
	DiffIgnore() []string
	ResourceVersion() string
}

func ToResourceData(resource Resource) (*ResourceData, error) {
	attributes, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource attributes: %w", err)
	}
	return &ResourceData{
		Executor:        resource.Executor(),
		Identifier:      resource.Identifier(),
		Attributes:      attributes,
		Dependencies:    resource.Dependencies(),
		DiffIgnore:      resource.DiffIgnore(),
		ResourceVersion: resource.ResourceVersion(),
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
		return ToResource[T](data)
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
