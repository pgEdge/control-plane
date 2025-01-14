package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestTxn(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	defer server.Close()
	client := server.Client()
	defer client.Close()

	t.Run("all conditions met", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewTxn(client,
			storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}),
			storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}),
			storage.NewCreateOp(client, "baz", &TestValue{SomeField: "baz"}),
		).Commit(ctx)

		assert.NoError(t, err)

		expected := []*TestValue{
			{SomeField: "foo"},
			{SomeField: "bar"},
			{SomeField: "baz"},
		}
		expected[0].SetVersion(1)
		expected[1].SetVersion(1)
		expected[2].SetVersion(1)

		vals, err := storage.NewGetMultipleOp[*TestValue](client, []string{
			"foo",
			"bar",
			"baz",
		}).Exec(ctx)

		require.NoError(t, err)

		assert.ElementsMatch(t, expected, vals)
	})

	t.Run("one condition unmet", func(t *testing.T) {
		ctx := context.Background()

		err := storage.NewPutOp(client, "1", &TestValue{SomeField: "1"}).Exec(ctx)
		require.NoError(t, err)

		err = storage.NewTxn(client,
			storage.NewCreateOp(client, "1", &TestValue{SomeField: "1"}),
			storage.NewCreateOp(client, "2", &TestValue{SomeField: "2"}),
			storage.NewCreateOp(client, "3", &TestValue{SomeField: "3"}),
		).Commit(ctx)

		assert.ErrorIs(t, err, storage.ErrOperationConstraintViolated)

		// 1 already exists since we created it before the transaction.
		vals, err := storage.NewGetMultipleOp[*TestValue](client, []string{
			"2",
			"3",
		}).Exec(ctx)

		require.NoError(t, err)

		assert.Empty(t, vals)
	})

	t.Run("duplicate keys", func(t *testing.T) {
		ctx := context.Background()
		err := storage.NewTxn(client,
			storage.NewPutOp(client, "a", &TestValue{SomeField: "a"}),
			storage.NewDeleteKeyOp(client, "a"),
		).Commit(ctx)

		assert.ErrorIs(t, err, storage.ErrDuplicateKeysInTransaction)

		assert.ErrorContains(t, err, "put a")
		assert.ErrorContains(t, err, "delete a")
	})
}
