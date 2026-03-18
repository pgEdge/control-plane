package storage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestPutOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("puts a value", func(t *testing.T) {
		ctx := t.Context()
		op := storage.NewPutOp(client, "foo", &TestValue{SomeField: "foo"})
		err := op.Exec(ctx)

		assert.NoError(t, err)
		assert.NotZero(t, op.Revision())

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "foo", val.SomeField)
	})

	t.Run("multiple puts succeed", func(t *testing.T) {
		ctx := t.Context()

		op1 := storage.NewPutOp(client, "bar", &TestValue{SomeField: "bar"})
		err := op1.Exec(ctx)
		assert.NoError(t, err)
		assert.NotZero(t, op1.Revision())

		op2 := storage.NewPutOp(client, "bar", &TestValue{SomeField: "baz"})
		err = op2.Exec(ctx)
		assert.NoError(t, err)
		assert.Greater(t, op2.Revision(), op1.Revision())

		val, err := storage.NewGetOp[*TestValue](client, "bar").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "baz", val.SomeField)
	})

	t.Run("with TTL", func(t *testing.T) {
		ctx := t.Context()
		op := storage.NewPutOp(client, "expires", &TestValue{SomeField: "qux"}).
			WithTTL(500 * time.Millisecond)
		err := op.Exec(ctx)

		assert.NoError(t, err)
		assert.NotZero(t, op.Revision())

		val, err := storage.NewGetOp[*TestValue](client, "expires").Exec(ctx)
		require.NoError(t, err)

		assert.Equal(t, "qux", val.SomeField)

		// It can take a bit for expired keys to be cleaned up.
		time.Sleep(2500 * time.Millisecond)

		val, err = storage.NewGetOp[*TestValue](client, "expires").Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrNotFound)
		assert.Nil(t, val)
	})

	t.Run("with updated version", func(t *testing.T) {
		ctx := t.Context()

		item := &TestValue{SomeField: "bar"}
		key := uuid.NewString()
		assert.NoError(t, storage.NewPutOp(client, key, item).WithUpdatedVersion().Exec(ctx))
		assert.Equal(t, int64(1), item.Version())

		item.SomeField = "baz"
		assert.NoError(t, storage.NewPutOp(client, key, item).WithUpdatedVersion().Exec(ctx))
		assert.Equal(t, int64(2), item.Version())
	})
}

func TestCreateOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("key does not exist", func(t *testing.T) {
		ctx := t.Context()
		op := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"})
		err := op.Exec(ctx)

		assert.NoError(t, err)
		assert.NotZero(t, op.Revision())

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)
		require.NotNil(t, val)
		require.Equal(t, "foo", val.SomeField)
	})

	t.Run("key exists", func(t *testing.T) {
		ctx := t.Context()

		op1 := storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"})
		err := op1.Exec(ctx)
		assert.NoError(t, err)
		assert.NotZero(t, op1.Revision())

		// The second create should fail because the value already exists
		op2 := storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"})
		err = op2.Exec(ctx)
		assert.ErrorIs(t, err, storage.ErrAlreadyExists)
		assert.NotZero(t, op2.Revision())
	})

	t.Run("with updated version", func(t *testing.T) {
		item := &TestValue{SomeField: "foo"}
		ctx := t.Context()
		err := storage.NewCreateOp(client, uuid.NewString(), item).WithUpdatedVersion().Exec(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), item.Version())
	})
}

func TestUpdateOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("valid update", func(t *testing.T) {
		ctx := t.Context()
		err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)

		val.SomeField = "bar"
		op := storage.NewUpdateOp(client, "foo", val)
		err = op.Exec(ctx)

		assert.NoError(t, err)
		assert.NotZero(t, op.Revision())

		updated, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)

		assert.Equal(t, "bar", updated.SomeField)
	})

	t.Run("version mismatch", func(t *testing.T) {
		ctx := t.Context()
		err := storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "bar").Exec(ctx)
		require.NoError(t, err)

		err = storage.NewPutOp(client, "bar", &TestValue{SomeField: "qux"}).Exec(ctx)
		require.NoError(t, err)

		op := storage.NewUpdateOp(client, "bar", val)
		err = op.Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrValueVersionMismatch)
		assert.NotZero(t, op.Revision())
	})

	t.Run("with updated version", func(t *testing.T) {
		ctx := t.Context()

		item := &TestValue{SomeField: "bar"}
		key := uuid.NewString()
		assert.NoError(t, storage.NewCreateOp(client, key, item).WithUpdatedVersion().Exec(ctx))
		assert.Equal(t, int64(1), item.Version())

		item.SomeField = "baz"
		assert.NoError(t, storage.NewUpdateOp(client, key, item).WithUpdatedVersion().Exec(ctx))
		assert.Equal(t, int64(2), item.Version())
	})
}
