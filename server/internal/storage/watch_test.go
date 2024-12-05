package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("Until", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			ctx := context.Background()
			watch := storage.NewWatchOp[*TestValue](client, "foo")

			done := make(chan error, 1)
			go func() {
				done <- watch.Until(ctx, 5*time.Second, func(e *storage.Event[*TestValue]) bool {
					expectedVal := &TestValue{
						SomeField: "bar",
					}
					expectedVal.SetVersion(1)
					expected := &storage.Event[*TestValue]{
						Type:     storage.EventTypePut,
						Key:      "foo",
						IsCreate: true,
						Value:    expectedVal,
					}

					assert.NoError(t, e.Err)
					assert.Equal(t, expected, e)

					return true
				})
			}()

			err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "bar"}).
				Exec(ctx)
			require.NoError(t, err)

			// Block until watch completes
			err = <-done

			assert.NoError(t, err)
		})

		t.Run("timeout", func(t *testing.T) {
			ctx := context.Background()
			watch := storage.NewWatchOp[*TestValue](client, "bar")

			done := make(chan error, 1)
			go func() {
				done <- watch.Until(ctx, 500*time.Millisecond, func(e *storage.Event[*TestValue]) bool {
					// Should not be reached in a successful run
					t.Fail()

					return true
				})
			}()

			// Block until watch completes
			err := <-done

			assert.ErrorIs(t, err, storage.ErrWatchUntilTimedOut)
		})
	})

	t.Run("Watch", func(t *testing.T) {
		ctx := context.Background()
		watch := storage.NewWatchOp[*TestValue](client, "baz")

		done := make(chan bool, 1)
		watch.Watch(ctx, func(e *storage.Event[*TestValue]) {
			expectedVal := &TestValue{
				SomeField: "qux",
			}
			expectedVal.SetVersion(1)
			expected := &storage.Event[*TestValue]{
				Type:     storage.EventTypePut,
				Key:      "baz",
				IsCreate: true,
				Value:    expectedVal,
			}

			assert.NoError(t, e.Err)
			assert.Equal(t, expected, e)

			done <- true
		})

		err := storage.NewCreateOp(client, "baz", &TestValue{SomeField: "qux"}).
			Exec(ctx)
		require.NoError(t, err)

		assert.True(t, <-done)
	})
}
