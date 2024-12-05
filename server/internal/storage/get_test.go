package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestGetOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("key exists", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}).Exec(ctx)
		require.NoError(t, err)

		val, err := storage.NewGetOp[*TestValue](client, "foo").Exec(ctx)

		expected := &TestValue{SomeField: "foo"}
		expected.SetVersion(1)

		assert.NoError(t, err)
		assert.Equal(t, expected, val)
	})

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()
		val, err := storage.NewGetOp[*TestValue](client, "bar").Exec(ctx)

		assert.ErrorIs(t, err, storage.ErrNotFound)
		assert.Nil(t, val)
	})
}

func TestGetMultipleOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("keys exist", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewTxn(client,
			storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}),
			storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}),
		).Commit(ctx)
		require.NoError(t, err)

		vals, err := storage.NewGetMultipleOp[*TestValue](client, []string{"foo", "bar"}).Exec(ctx)

		expected := []*TestValue{
			{SomeField: "foo"},
			{SomeField: "bar"},
		}
		expected[0].SetVersion(1)
		expected[1].SetVersion(1)

		assert.NoError(t, err)
		assert.ElementsMatch(t, expected, vals)
	})

	t.Run("keys do not exist", func(t *testing.T) {
		ctx := context.Background()
		vals, err := storage.NewGetMultipleOp[*TestValue](client, []string{"baz", "qux"}).Exec(ctx)

		assert.NoError(t, err)
		assert.Len(t, vals, 0)
	})
}

func TestGetPrefixOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("keys exist", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewTxn(client,
			storage.NewCreateOp(client, "/prefix/foo", &TestValue{SomeField: "foo"}),
			storage.NewCreateOp(client, "/prefix/bar", &TestValue{SomeField: "bar"}),
		).Commit(ctx)
		require.NoError(t, err)

		vals, err := storage.NewGetPrefixOp[*TestValue](client, "/prefix").Exec(ctx)

		expected := []*TestValue{
			{SomeField: "foo"},
			{SomeField: "bar"},
		}
		expected[0].SetVersion(1)
		expected[1].SetVersion(1)

		assert.NoError(t, err)
		assert.ElementsMatch(t, expected, vals)
	})

	t.Run("keys do not exist", func(t *testing.T) {
		ctx := context.Background()
		vals, err := storage.NewGetPrefixOp[*TestValue](client, "/prefix2").Exec(ctx)

		assert.NoError(t, err)
		assert.Len(t, vals, 0)
	})
}

func TestGetRangeOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("keys exist", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewTxn(client,
			storage.NewCreateOp(client, "/prefix/00001", &TestValue{SomeField: "1"}),
			storage.NewCreateOp(client, "/prefix/00002", &TestValue{SomeField: "2"}),
			storage.NewCreateOp(client, "/prefix/00003", &TestValue{SomeField: "3"}),
			storage.NewCreateOp(client, "/prefix/00004", &TestValue{SomeField: "4"}),
			storage.NewCreateOp(client, "/prefix/00005", &TestValue{SomeField: "5"}),
		).Commit(ctx)
		require.NoError(t, err)

		vals, err := storage.NewGetRangeOp[*TestValue](client, "/prefix/00002", "/prefix/00005").Exec(ctx)

		expected := []*TestValue{
			{SomeField: "2"},
			{SomeField: "3"},
			{SomeField: "4"},
		}
		expected[0].SetVersion(1)
		expected[1].SetVersion(1)
		expected[2].SetVersion(1)

		assert.NoError(t, err)
		assert.ElementsMatch(t, expected, vals)
	})

	t.Run("keys do not exist", func(t *testing.T) {
		ctx := context.Background()
		vals, err := storage.NewGetRangeOp[*TestValue](client, "/prefix2/00002", "/prefix2/00005").Exec(ctx)

		assert.NoError(t, err)
		assert.Len(t, vals, 0)
	})
}

func TestExistsOp(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("key exists", func(t *testing.T) {
		ctx := context.Background()
		_, err := client.Put(ctx, "foo", "bar")
		require.NoError(t, err)

		exists, err := storage.NewExistsOp(client, "foo").Exec(ctx)

		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("key does not exist", func(t *testing.T) {
		ctx := context.Background()

		exists, err := storage.NewExistsOp(client, "bar").Exec(ctx)

		assert.NoError(t, err)
		assert.False(t, exists)
	})
}
