package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestDeleteKeyOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("key exists", func(t *testing.T) {
		ctx := context.Background()
		client.Put(ctx, "foo", "bar")

		deleted, err := storage.NewDeleteKeyOp(client, "foo").Exec(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int64(1), deleted)

		resp, err := client.Get(ctx, "foo")
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 0)
	})

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()
		deleted, err := storage.NewDeleteKeyOp(client, "baz").Exec(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})
}

func TestDeletePrefixOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("keys exist", func(t *testing.T) {
		ctx := context.Background()
		client.Put(ctx, "/prefix/foo", "1")
		client.Put(ctx, "/prefix/bar", "2")

		deleted, err := storage.NewDeletePrefixOp(client, "/prefix").Exec(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), deleted)

		resp, err := client.Get(ctx, "/prefix", clientv3.WithPrefix())
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 0)
	})

	t.Run("keys do not exist", func(t *testing.T) {
		ctx := context.Background()
		deleted, err := storage.NewDeletePrefixOp(client, "/prefix2").Exec(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})
}

func TestDeleteValueOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("key exists", func(t *testing.T) {
		ctx := context.Background()

		err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)
		require.NoError(t, err)

		err = storage.NewDeleteValueOp(client, "foo", val).Exec(ctx)

		assert.NoError(t, err)

		resp, err := client.Get(ctx, "foo")
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 0)
	})

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()

		val := &TestValue{SomeField: "bar"}
		err := storage.NewDeleteValueOp(client, "baz", val).Exec(ctx)

		assert.NoError(t, err)
	})

	t.Run("version mismatch", func(t *testing.T) {
		ctx := context.Background()

		err := storage.NewCreateOp(client, "baz", &TestValue{SomeField: "baz"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "baz").Exec(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1), val.Version())

		err = storage.NewPutOp(client, "baz", &TestValue{SomeField: "new baz"}).Exec(ctx)
		require.NoError(t, err)

		err = storage.NewDeleteValueOp(client, "baz", val).Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrValueVersionMismatch)
	})
}
