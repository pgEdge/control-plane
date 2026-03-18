package storage_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestWatchOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("delivers initial items via Get", func(t *testing.T) {
		ctx := t.Context()
		key := uuid.NewString()

		// Pre-create a key before starting the watch.
		err := storage.NewCreateOp(client, key, &TestValue{SomeField: "existing"}).
			Exec(ctx)
		require.NoError(t, err)

		watch := storage.NewWatchOp[*TestValue](client, key)

		received := make(chan *storage.Event[*TestValue], 1)
		handler := func(e *storage.Event[*TestValue]) error {
			received <- e
			return nil
		}
		require.NoError(t, watch.Watch(ctx, handler))
		t.Cleanup(watch.Close)

		e := <-received
		assert.Equal(t, storage.EventTypePut, e.Type)
		assert.Equal(t, key, e.Key)
		assert.Equal(t, "existing", e.Value.SomeField)
	})

	t.Run("delivers subsequent modifications", func(t *testing.T) {
		ctx := t.Context()
		key := uuid.NewString()

		watch := storage.NewWatchOp[*TestValue](client, key)

		received := make(chan *storage.Event[*TestValue], 1)
		handler := func(e *storage.Event[*TestValue]) error {
			received <- e
			return nil
		}
		require.NoError(t, watch.Watch(ctx, handler))
		t.Cleanup(watch.Close)

		err := storage.NewCreateOp(client, key, &TestValue{SomeField: "qux"}).
			Exec(ctx)
		require.NoError(t, err)

		e := <-received
		assert.Equal(t, storage.EventTypePut, e.Type)
		assert.Equal(t, key, e.Key)
		assert.Equal(t, "qux", e.Value.SomeField)
		assert.True(t, e.IsCreate)
	})

	t.Run("returns error from initial get", func(t *testing.T) {
		ctx := t.Context()
		key := uuid.NewString()

		watch := storage.NewWatchOp[*TestValue](client, key)

		// Pre-create a key so the initial Get delivers an event.
		err := storage.NewCreateOp(client, key, &TestValue{SomeField: "v"}).
			Exec(ctx)
		require.NoError(t, err)

		sentinel := assert.AnError
		handler := func(e *storage.Event[*TestValue]) error {
			return sentinel
		}
		require.ErrorIs(t, watch.Watch(ctx, handler), sentinel)
	})

	t.Run("delivers error from handler", func(t *testing.T) {
		ctx := t.Context()
		watch := storage.NewWatchOp[*TestValue](client, "watch-err")

		sentinel := assert.AnError
		handler := func(e *storage.Event[*TestValue]) error {
			return sentinel
		}
		require.NoError(t, watch.Watch(ctx, handler))
		t.Cleanup(watch.Close)

		err := storage.NewCreateOp(client, "watch-err", &TestValue{SomeField: "v"}).
			Exec(ctx)
		require.NoError(t, err)

		require.ErrorIs(t, <-watch.Error(), sentinel)
	})
}
