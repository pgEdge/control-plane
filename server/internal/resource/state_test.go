package resource_test

import (
	"context"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	t.Run("Plan", func(t *testing.T) {
		t.Run("from empty state", func(t *testing.T) {
			// registry := resource.NewRegistry()
			// resource.RegisterResource[*TestResource](*registry, "test_resource")

			resource1 := &testResource{
				identifier: testResourceID("test1"),
				dependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				identifier: testResourceID("test2"),
				dependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				identifier: testResourceID("test3"),
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			desired.AddResource(resource1)
			desired.AddResource(resource2)
			desired.AddResource(resource3)

			plan, err := current.Plan(desired, false)
			assert.NoError(t, err)

			expected := []*resource.Event{
				{
					Type:     resource.EventTypeCreate,
					Resource: resource3Data,
				},
				{
					Type:     resource.EventTypeCreate,
					Resource: resource2Data,
				},
				{
					Type:     resource.EventTypeCreate,
					Resource: resource1Data,
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("from nonempty state", func(t *testing.T) {
			// registry := resource.NewRegistry()
			// resource.RegisterResource[*TestResource](*registry, "test_resource")

			resource1 := &testResource{
				identifier: testResourceID("test1"),
				dependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				identifier: testResourceID("test2"),
				dependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				identifier: testResourceID("test3"),
			}

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource3)

			desired.AddResource(resource1)
			desired.AddResource(resource2)
			desired.AddResource(resource3)

			plan, err := current.Plan(desired, false)
			assert.NoError(t, err)

			expected := []*resource.Event{
				{
					Type:     resource.EventTypeCreate,
					Resource: resource2Data,
				},
				{
					Type:     resource.EventTypeCreate,
					Resource: resource1Data,
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("with update", func(t *testing.T) {
			// registry := resource.NewRegistry()
			// resource.RegisterResource[*TestResource](*registry, "test_resource")

			resource1 := &testResource{
				identifier: testResourceID("test1"),
				dependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				identifier: testResourceID("test2"),
				dependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}

			resource3 := &testResource{
				identifier: testResourceID("test3"),
			}

			updatedResource2 := &testResource{
				SomeAttribute: "updated",
				identifier:    testResourceID("test2"),
				dependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			updatedResource2Data, err := resource.ToResourceData(updatedResource2)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource1)
			current.AddResource(resource2)
			current.AddResource(resource3)

			desired.AddResource(resource1)
			desired.AddResource(updatedResource2)
			desired.AddResource(resource3)

			plan, err := current.Plan(desired, false)
			assert.NoError(t, err)

			expected := []*resource.Event{
				{
					Type:     resource.EventTypeUpdate,
					Resource: updatedResource2Data,
				},
				// Resource 1 should be marked for update because it depends on
				// resource 2.
				{
					Type:     resource.EventTypeUpdate,
					Resource: resource1Data,
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("to empty state", func(t *testing.T) {
			// registry := resource.NewRegistry()
			// resource.RegisterResource[*TestResource](*registry, "test_resource")

			resource1 := &testResource{
				identifier: testResourceID("test1"),
				dependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				identifier: testResourceID("test2"),
				dependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				identifier: testResourceID("test3"),
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource1)
			current.AddResource(resource2)
			current.AddResource(resource3)

			plan, err := current.Plan(desired, false)
			assert.NoError(t, err)

			expected := []*resource.Event{
				{
					Type:     resource.EventTypeDelete,
					Resource: resource1Data,
				},
				{
					Type:     resource.EventTypeDelete,
					Resource: resource2Data,
				},
				{
					Type:     resource.EventTypeDelete,
					Resource: resource3Data,
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("missing dependency", func(t *testing.T) {
			// registry := resource.NewRegistry()
			// resource.RegisterResource[*TestResource](*registry, "test_resource")

			resource1 := &testResource{
				identifier: testResourceID("test1"),
				dependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource1)

			plan, err := current.Plan(desired, false)
			assert.ErrorContains(t, err, "dependency of test_resource::test1 not found: test_resource::test2")
			assert.Nil(t, plan)
		})
	})
}

func testResourceID(id string) resource.Identifier {
	return resource.Identifier{
		Type: "test_resource",
		ID:   id,
	}
}

type testResource struct {
	SomeAttribute string `json:"some_attribute"`
	identifier    resource.Identifier
	dependencies  []resource.Identifier
}

func (r *testResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   "test",
	}
}

func (r *testResource) Identifier() resource.Identifier {
	return r.identifier
}

func (r *testResource) Dependencies() []resource.Identifier {
	return r.dependencies
}

func (r *testResource) Validate() error {
	return nil
}

func (r *testResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *testResource) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *testResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *testResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
