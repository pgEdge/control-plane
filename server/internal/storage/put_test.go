package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestPutOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("puts a value", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewPutOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)

		assert.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "foo", val.SomeField)
	})

	t.Run("multiple puts succeed", func(t *testing.T) {
		ctx := context.Background()

		err := storage.NewPutOp(client, "bar", &TestValue{SomeField: "bar"}).Exec(ctx)
		assert.NoError(t, err)
		err = storage.NewPutOp(client, "bar", &TestValue{SomeField: "baz"}).Exec(ctx)
		assert.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "bar").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "baz", val.SomeField)
	})

	t.Run("with TTL", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewPutOp(client, "expires", &TestValue{SomeField: "qux"}).
			WithTTL(500 * time.Millisecond).
			Exec(ctx)

		assert.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "expires").Exec(ctx)
		require.NoError(t, err)

		assert.Equal(t, "qux", val.SomeField)

		// It can take a bit for expired keys to be cleaned up.
		time.Sleep(2500 * time.Millisecond)

		val, err = storage.NewGetOp[*TestValue](client, "expires").Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrNotFound)
		assert.Nil(t, val)
	})
}

func TestCreateOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)

		assert.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "foo", val.SomeField)
	})

	t.Run("key exists", func(t *testing.T) {
		ctx := context.Background()

		err := storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}).Exec(ctx)
		assert.NoError(t, err)

		// The second create should fail because the value already exists
		err = storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}).Exec(ctx)
		assert.ErrorIs(t, err, storage.ErrAlreadyExists)
	})
}

func TestUpdateOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("valid update", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)

		val.SomeField = "bar"
		err = storage.NewUpdateOp(client, "foo", val).Exec(ctx)

		assert.NoError(t, err)

		updated, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)

		assert.Equal(t, "bar", updated.SomeField)
	})

	t.Run("version mismatch", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "bar").Exec(ctx)
		require.NoError(t, err)

		err = storage.NewPutOp(client, "bar", &TestValue{SomeField: "qux"}).Exec(ctx)
		require.NoError(t, err)

		err = storage.NewUpdateOp(client, "bar", val).Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrValueVersionMismatch)
	})

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewUpdateOp(client, "baz", &TestValue{SomeField: "baz"}).Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrValueVersionMismatch)
	})
}
