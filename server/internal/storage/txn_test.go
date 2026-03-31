package storage_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestTxn(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("all conditions met", func(t *testing.T) {
		ctx := context.Background()
		ops := []storage.TxnOperation{
			storage.NewCreateOp(client, "foo", &TestValue{SomeField: "foo"}),
			storage.NewCreateOp(client, "bar", &TestValue{SomeField: "bar"}),
			storage.NewCreateOp(client, "baz", &TestValue{SomeField: "baz"}),
		}
		err := storage.NewTxn(client, ops...).Commit(ctx)

		assert.NoError(t, err)
		for _, op := range ops {
			assert.NotZero(t, op.Revision())
		}

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

		ops := []storage.TxnOperation{
			storage.NewCreateOp(client, "1", &TestValue{SomeField: "1"}),
			storage.NewCreateOp(client, "2", &TestValue{SomeField: "2"}),
			storage.NewCreateOp(client, "3", &TestValue{SomeField: "3"}),
		}
		err = storage.NewTxn(client, ops...).Commit(ctx)

		assert.ErrorIs(t, err, storage.ErrOperationConstraintViolated)
		for _, op := range ops {
			assert.NotZero(t, op.Revision())
		}

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
		ops := []storage.TxnOperation{
			storage.NewPutOp(client, "a", &TestValue{SomeField: "a"}),
			storage.NewDeleteKeyOp(client, "a"),
		}
		err := storage.NewTxn(client, ops...).Commit(ctx)

		assert.ErrorIs(t, err, storage.ErrDuplicateKeysInTransaction)
		for _, op := range ops {
			// These should be zero since this transaction failed before making
			// a request to the server.
			assert.Zero(t, op.Revision())
		}

		assert.ErrorContains(t, err, "put a")
		assert.ErrorContains(t, err, "delete a")
	})

	t.Run("updates versions", func(t *testing.T) {
		item1 := &TestValue{SomeField: "foo"}
		item2 := &TestValue{SomeField: "foo"}
		item3 := &TestValue{SomeField: "foo"}
		item4 := &TestValue{SomeField: "foo"}
		key1 := uuid.NewString()
		key2 := uuid.NewString()
		key3 := uuid.NewString()
		key4 := uuid.NewString()

		err := storage.NewTxn(client,
			storage.NewCreateOp(client, key1, item1).WithUpdatedVersion(),
			// Updates are logically equivalent to create when the version is 0.
			storage.NewUpdateOp(client, key2, item2).WithUpdatedVersion(),
			storage.NewPutOp(client, key3, item3).WithUpdatedVersion(),
			storage.NewPutOp(client, key4, item4),
		).Commit(t.Context())
		assert.NoError(t, err)
		assert.Equal(t, int64(1), item1.Version())
		assert.Equal(t, int64(1), item2.Version())
		assert.Equal(t, int64(1), item3.Version())
		assert.Equal(t, int64(0), item4.Version()) // This op didn't have WithUpdatedVersion

		// Reinitialize items 1-3 to zero out their versions
		item1 = &TestValue{SomeField: "foo"}
		item2 = &TestValue{SomeField: "foo"}
		item3 = &TestValue{SomeField: "foo"}
		err = storage.NewTxn(client,
			storage.NewPutOp(client, key1, item1).WithUpdatedVersion(),
			storage.NewPutOp(client, key2, item2).WithUpdatedVersion(),
			storage.NewPutOp(client, key3, item3).WithUpdatedVersion(),
		).Commit(t.Context())
		assert.NoError(t, err)
		assert.Equal(t, int64(2), item1.Version())
		assert.Equal(t, int64(2), item2.Version())
		assert.Equal(t, int64(2), item3.Version())
	})
}
