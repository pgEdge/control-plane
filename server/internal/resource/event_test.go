package resource_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
	"github.com/stretchr/testify/assert"
)

func TestEvent(t *testing.T) {
	t.Run("Apply", func(t *testing.T) {
		registry := resource.NewRegistry()
		resource.RegisterResourceType[*testResource](registry, testResourceType)

		rc := &resource.Context{
			State:    resource.NewState(),
			Registry: registry,
			Injector: do.New(),
		}

		for _, tc := range []struct {
			name                          string
			eventType                     resource.EventType
			notFound                      bool
			lifecycleError                string
			originalResourceError         string
			originalResourceNeedsRecreate bool
			expectedErr                   string
			expectedResourceError         string
			expectedResourceNeedsRecreate bool
		}{
			{
				name:      "refresh success",
				eventType: resource.EventTypeRefresh,
			},
			{
				name:                          "refresh success retains Error and NeedsRecreate",
				eventType:                     resource.EventTypeRefresh,
				originalResourceError:         "some error",
				originalResourceNeedsRecreate: true,
				expectedResourceError:         "some error",
				expectedResourceNeedsRecreate: true,
			},
			{
				name:                          "refresh not found",
				eventType:                     resource.EventTypeRefresh,
				notFound:                      true,
				expectedResourceNeedsRecreate: true,
			},
			{
				name:           "refresh failed",
				eventType:      resource.EventTypeRefresh,
				lifecycleError: "some error",
				expectedErr:    "failed to refresh resource test_resource::test: some error",
			},
			{
				name:      "create success",
				eventType: resource.EventTypeCreate,
			},
			{
				name:                          "create success clears Error and NeedsRecreate",
				eventType:                     resource.EventTypeCreate,
				originalResourceError:         "some error",
				originalResourceNeedsRecreate: true,
			},
			{
				name:                          "create failed",
				eventType:                     resource.EventTypeCreate,
				lifecycleError:                "some error",
				expectedResourceError:         "failed to create resource test_resource::test: some error",
				expectedResourceNeedsRecreate: true,
			},
			{
				name:      "update success",
				eventType: resource.EventTypeUpdate,
			},
			{
				name:                          "update success clears Error and NeedsRecreate",
				eventType:                     resource.EventTypeUpdate,
				originalResourceError:         "some error",
				originalResourceNeedsRecreate: true,
			},
			{
				name:                  "update failed",
				eventType:             resource.EventTypeUpdate,
				lifecycleError:        "some error",
				expectedResourceError: "failed to update resource test_resource::test: some error",
			},
			{
				name:      "delete success",
				eventType: resource.EventTypeDelete,
			},
			{
				name:                          "delete success clears Error and NeedsRecreate",
				eventType:                     resource.EventTypeDelete,
				originalResourceError:         "some error",
				originalResourceNeedsRecreate: true,
			},
			{
				name:           "delete failed",
				eventType:      resource.EventTypeDelete,
				lifecycleError: "some error",
				expectedErr:    "failed to delete resource test_resource::test: some error",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := &testResource{
					ID:       "test",
					NotFound: tc.notFound,
					Error:    tc.lifecycleError,
				}

				original := r.data(t)
				original.Error = tc.originalResourceError
				original.NeedsRecreate = tc.originalResourceNeedsRecreate

				expected := r.data(t)
				expected.Error = tc.expectedResourceError
				expected.NeedsRecreate = tc.expectedResourceNeedsRecreate

				event := &resource.Event{
					Type:     tc.eventType,
					Resource: original,
				}

				err := event.Apply(t.Context(), rc)

				if tc.expectedErr != "" {
					assert.ErrorContains(t, err, tc.expectedErr)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, expected, event.Resource)
				}
			})
		}
	})
}
