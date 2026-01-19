package resource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	t.Run("PlanRefresh", func(t *testing.T) {
		t.Run("from empty state", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
				TestDependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			current := resource.NewState()

			current.AddResource(resource1)
			current.AddResource(resource2)
			current.AddResource(resource3)

			plan, err := current.PlanRefresh()
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeRefresh,
						Resource: resource3Data,
					},
				},
				{
					{
						Type:     resource.EventTypeRefresh,
						Resource: resource2Data,
					},
				},
				{
					{
						Type:     resource.EventTypeRefresh,
						Resource: resource1Data,
					},
				},
			}

			assert.Equal(t, expected, plan)
		})
	})
	t.Run("Plan", func(t *testing.T) {
		t.Run("from empty state", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
				TestDependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			desired.AddResource(resource1)
			desired.AddResource(resource2)
			desired.AddResource(resource3)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource3Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource2Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource1Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("from nonempty state", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
				TestDependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
			}

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource3)

			desired.AddResource(resource1)
			desired.AddResource(resource2)
			desired.AddResource(resource3)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource2Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource1Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("with update", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
				TestDependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
			}

			updatedResource2 := &testResource{
				SomeAttribute: "updated",
				ID:            "test2",
				TestDependencies: []resource.Identifier{
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

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			expectedDiff, err := resource2Data.Diff(updatedResource2Data)
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeUpdate,
						Resource: updatedResource2Data,
						Reason:   resource.EventReasonHasDiff,
						Diff:     expectedDiff,
					},
				},
				{
					// Resource 1 should be marked for update because it depends on
					// resource 2.
					{
						Type:     resource.EventTypeUpdate,
						Resource: resource1Data,
						Reason:   resource.EventReasonDependencyUpdated,
					},
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("to empty state", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
				TestDependencies: []resource.Identifier{
					testResourceID("test3"),
				},
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(resource1)
			current.AddResource(resource2)
			current.AddResource(resource3)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource1Data,
					},
				},
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource2Data,
					},
				},
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource3Data,
					},
				},
			}

			assert.Equal(t, expected, plan)
		})
		t.Run("mixed creates and deletes", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			resource2 := &testResource{
				ID: "test2",
			}
			resource2Data, err := resource.ToResourceData(resource2)
			require.NoError(t, err)

			resource3 := &testResource{
				ID: "test3",
				TestDependencies: []resource.Identifier{
					testResourceID("test4"),
				},
			}
			resource3Data, err := resource.ToResourceData(resource3)
			require.NoError(t, err)

			resource4 := &testResource{
				ID: "test4",
			}
			resource4Data, err := resource.ToResourceData(resource4)
			require.NoError(t, err)

			resource5 := &testResource{
				ID: "test5",
			}
			resource5Data, err := resource.ToResourceData(resource5)
			require.NoError(t, err)

			resource6 := &testResource{
				ID: "test6",
				TestDependencies: []resource.Identifier{
					testResourceID("test5"),
				},
			}
			resource6Data, err := resource.ToResourceData(resource6)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			desired.AddResource(resource1)
			desired.AddResource(resource2)
			desired.AddResource(resource3)
			desired.AddResource(resource4)

			current.AddResource(resource5)
			current.AddResource(resource6)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			// The order of the content of each phase is non-deterministic
			// because of map iteration.
			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource2Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
					{
						Type:     resource.EventTypeCreate,
						Resource: resource4Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
				{
					{
						Type:     resource.EventTypeCreate,
						Resource: resource1Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
					{
						Type:     resource.EventTypeCreate,
						Resource: resource3Data,
						Reason:   resource.EventReasonDoesNotExist,
					},
				},
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource6Data,
					},
				},
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource5Data,
					},
				},
			}

			assert.Len(t, plan, len(expected))
			for i, phase := range plan {
				assert.ElementsMatch(t, expected[i], phase)
			}
		})
		t.Run("missing create dependency", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}

			current := resource.NewState()
			desired := resource.NewState()

			// missing dependencies produce an error during creates
			desired.AddResource(resource1)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.ErrorContains(t, err, "dependency of test_resource::test1 not found: test_resource::test2")
			assert.Nil(t, plan)
		})

		t.Run("missing delete dependency", func(t *testing.T) {
			resource1 := &testResource{
				ID: "test1",
				TestDependencies: []resource.Identifier{
					testResourceID("test2"),
				},
			}
			resource1Data, err := resource.ToResourceData(resource1)
			require.NoError(t, err)

			current := resource.NewState()
			desired := resource.NewState()

			// missing dependencies are allowed during deletes
			current.AddResource(resource1)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			expected := resource.Plan{
				{
					{
						Type:     resource.EventTypeDelete,
						Resource: resource1Data,
					},
				},
			}
			assert.Equal(t, expected, plan)
		})

		t.Run("ignored attributes", func(t *testing.T) {
			currentResource := &testResource{
				SomeIgnoredAttribute: "ignored",
				ID:                   "test1",
			}
			desiredResource := &testResource{
				ID: "test1",
			}

			current := resource.NewState()
			desired := resource.NewState()

			current.AddResource(currentResource)
			desired.AddResource(desiredResource)

			plan, err := current.Plan(resource.PlanOptions{}, desired)
			assert.NoError(t, err)

			assert.Empty(t, plan)
		})
	})
}

const testResourceType = resource.Type("test_resource")

func testResourceID(id string) resource.Identifier {
	return resource.Identifier{
		Type: testResourceType,
		ID:   id,
	}
}

type testResource struct {
	SomeAttribute        string                `json:"some_attribute"`
	SomeIgnoredAttribute string                `json:"some_ignored_attribute"`
	ID                   string                `json:"id"`
	TestDependencies     []resource.Identifier `json:"test_dependencies"`
	NotFound             bool                  `json:"not_found"`
	Error                string                `json:"error"`
}

func (r *testResource) ResourceVersion() string {
	return "1"
}

func (r *testResource) DiffIgnore() []string {
	return []string{
		"/some_ignored_attribute",
	}
}

func (r *testResource) Executor() resource.Executor {
	return resource.HostExecutor("test")
}

func (r *testResource) Identifier() resource.Identifier {
	return testResourceID(r.ID)
}

func (r *testResource) Dependencies() []resource.Identifier {
	return r.TestDependencies
}

func (r *testResource) Refresh(ctx context.Context, rc *resource.Context) error {
	switch {
	case r.NotFound:
		return resource.ErrNotFound
	case r.Error != "":
		return errors.New(r.Error)
	default:
		return nil
	}
}

func (r *testResource) Create(ctx context.Context, rc *resource.Context) error {
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

func (r *testResource) Update(ctx context.Context, rc *resource.Context) error {
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

func (r *testResource) Delete(ctx context.Context, rc *resource.Context) error {
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

func (r *testResource) data(t testing.TB) *resource.ResourceData {
	data, err := resource.ToResourceData(r)
	require.NoError(t, err)

	return data
}
