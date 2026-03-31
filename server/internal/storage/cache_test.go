package storage_test

import (
	"errors"
	"path"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestCache(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	newCache := func(t testing.TB, prefix string) (storage.Cache[*TestValue], func(v *TestValue) string) {
		key := func(v *TestValue) string {
			return path.Join(prefix, v.SomeField)
		}
		cache := storage.NewCache(client, prefix, key)
		require.NoError(t, cache.Start(t.Context()))
		t.Cleanup(cache.Stop)

		return cache, key
	}

	t.Run("Get", func(t *testing.T) {
		t.Run("returns existing item", func(t *testing.T) {
			prefix := uuid.NewString()
			in := &TestValue{SomeField: "foo"}
			k := path.Join(prefix, in.SomeField)
			require.NoError(t, storage.NewPutOp(client, k, in).Exec(t.Context()))

			cache, _ := newCache(t, prefix)
			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)
		})

		t.Run("returns distinct items", func(t *testing.T) {
			prefix := uuid.NewString()
			cache, key := newCache(t, prefix)
			in := &TestValue{SomeField: "foo"}
			k := key(in)

			require.NoError(t, cache.Put(in).Exec(t.Context()))

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)

			// These should be equal because the cache Put has written the
			// updated version back to the item.
			require.Equal(t, in, out)

			require.NoError(t, cache.Put(in).Exec(t.Context()))

			// Now they're unequal because the Put updated the version on 'in',
			// but out is a distinct instance.
			require.NotEqual(t, in, out)

			out, err = cache.Get(k).Exec(t.Context())
			require.NoError(t, err)

			// Now they're equal again after refetching 'out'.
			require.Equal(t, in, out)
		})

		t.Run("updates via watch", func(t *testing.T) {
			prefix := uuid.NewString()
			cache, key := newCache(t, prefix)
			in := &TestValue{SomeField: "foo"}
			k := key(in)

			// Using WithUpdatedVersion to enable require.Equal assertion.
			require.NoError(t, storage.NewPutOp(client, k, in).
				WithUpdatedVersion().
				Exec(t.Context()))

			// Poll up to 1 second or until we see the value or an unexpected
			// error come back.
			var out *TestValue
			var err error
			for range 10 {
				out, err = cache.Get(k).Exec(t.Context())
				if !errors.Is(err, storage.ErrNotFound) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			// Update and compare with our Get output to validate that these are
			// distinct value instances.
			require.NoError(t, cache.Update(in).Exec(t.Context()))
			require.NotEqual(t, in, out)

			_, err = storage.NewDeleteKeyOp(client, k).Exec(t.Context())
			require.NoError(t, err)

			// Poll until we get an error.
			for range 10 {
				out, err = cache.Get(k).Exec(t.Context())
				if err != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})

		t.Run("returns not found", func(t *testing.T) {
			cache, _ := newCache(t, uuid.NewString())

			out, err := cache.Get("foo").Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})
	})

	t.Run("GetPrefix", func(t *testing.T) {
		t.Run("returns matching items", func(t *testing.T) {
			prefix := uuid.NewString()
			cache, _ := newCache(t, prefix)
			expected := []*TestValue{
				{SomeField: "foo/bar"},
				{SomeField: "foo/baz"},
			}
			unexpected := []*TestValue{
				{SomeField: "bar/foo"},
				{SomeField: "bar/baz"},
			}
			for _, in := range slices.Concat(expected, unexpected) {
				require.NoError(t, cache.Put(in).Exec(t.Context()))
			}

			getPrefix := path.Join(prefix, "foo") + "/"
			out, err := cache.GetPrefix(getPrefix).Exec(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, expected, out)
		})
	})

	t.Run("Put", func(t *testing.T) {
		t.Run("successful", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			// the write should have persisted to the cache
			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			// the write should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, storedOut.SomeField)
		})

		t.Run("failed update", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			// Set the version to a non-existent version
			in.SetVersion(1)
			k := key(in)

			err := cache.Update(in).Exec(t.Context())
			require.Error(t, err)

			// the write should not have persisted to the cache
			out, err := cache.Get(k).Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})

		t.Run("create", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Create(in).Exec(t.Context())
			require.NoError(t, err)

			// the write should have persisted to the cache
			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			// the write should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, storedOut.SomeField)

			// subsequent create should fail
			err = cache.Create(in).Exec(t.Context())
			require.ErrorIs(t, err, storage.ErrAlreadyExists)
		})

		t.Run("transaction", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			op := cache.Put(in)
			txn := storage.NewTxn(client, op)
			require.NoError(t, txn.Commit(t.Context()))

			// in's version should have updated
			require.Equal(t, int64(1), in.Version())

			// the write should have persisted to the cache
			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			// the write should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, storedOut.SomeField)
		})
	})

	t.Run("DeleteByKey", func(t *testing.T) {
		t.Run("key exists", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			count, err := cache.DeleteByKey(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(1), count)

			// the delete should have persisted to the cache
			out, err = cache.Get(k).Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.Nil(t, storedOut)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})

		t.Run("key does not exist", func(t *testing.T) {
			cache, _ := newCache(t, uuid.NewString())

			count, err := cache.DeleteByKey("foo").Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(0), count)
		})

		t.Run("transaction", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			op := cache.DeleteByKey(k)
			txn := storage.NewTxn(client, op)
			require.NoError(t, txn.Commit(t.Context()))

			// the delete should have persisted to the cache
			out, err = cache.Get(k).Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.Nil(t, storedOut)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})
	})

	t.Run("DeleteByPrefix", func(t *testing.T) {
		t.Run("deletes matching items", func(t *testing.T) {
			prefix := uuid.NewString()
			cache, _ := newCache(t, prefix)
			expected := []*TestValue{
				{SomeField: "foo/bar"},
				{SomeField: "foo/baz"},
			}
			unexpected := []*TestValue{
				{SomeField: "bar/foo"},
				{SomeField: "bar/baz"},
			}
			for _, in := range slices.Concat(expected, unexpected) {
				require.NoError(t, cache.Put(in).Exec(t.Context()))
			}

			deletePrefix := path.Join(prefix, "bar") + "/"
			count, err := cache.DeletePrefix(deletePrefix).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(2), count)

			out, err := cache.GetPrefix("").Exec(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, expected, out)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetPrefixOp[*TestValue](client, prefix).Exec(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, expected, storedOut)
		})

		t.Run("transaction", func(t *testing.T) {
			prefix := uuid.NewString()
			cache, _ := newCache(t, prefix)
			expected := []*TestValue{
				{SomeField: "foo/bar"},
				{SomeField: "foo/baz"},
			}
			unexpected := []*TestValue{
				{SomeField: "bar/foo"},
				{SomeField: "bar/baz"},
			}
			for _, in := range slices.Concat(expected, unexpected) {
				require.NoError(t, cache.Put(in).Exec(t.Context()))
			}

			deletePrefix := path.Join(prefix, "bar") + "/"
			op := cache.DeletePrefix(deletePrefix)
			txn := storage.NewTxn(client, op)
			require.NoError(t, txn.Commit(t.Context()))

			out, err := cache.GetPrefix("").Exec(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, expected, out)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetPrefixOp[*TestValue](client, prefix).Exec(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, expected, storedOut)
		})
	})

	t.Run("DeleteValue", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			err = cache.DeleteValue(in).Exec(t.Context())
			require.NoError(t, err)

			// the delete should have persisted to the cache
			out, err = cache.Get(k).Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.Nil(t, storedOut)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})

		t.Run("failure", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			// Simulate another writer
			err = cache.Put(&TestValue{SomeField: "foo"}).Exec(t.Context())
			require.NoError(t, err)

			err = cache.DeleteValue(in).Exec(t.Context())
			require.ErrorIs(t, err, storage.ErrValueVersionMismatch)
		})

		t.Run("transaction", func(t *testing.T) {
			cache, key := newCache(t, uuid.NewString())

			in := &TestValue{SomeField: "foo"}
			k := key(in)

			err := cache.Put(in).Exec(t.Context())
			require.NoError(t, err)

			out, err := cache.Get(k).Exec(t.Context())
			require.NoError(t, err)
			require.Equal(t, in.SomeField, out.SomeField)

			op := cache.DeleteValue(in)
			txn := storage.NewTxn(client, op)
			require.NoError(t, txn.Commit(t.Context()))

			// the delete should have persisted to the cache
			out, err = cache.Get(k).Exec(t.Context())
			require.Nil(t, out)
			require.ErrorIs(t, err, storage.ErrNotFound)

			// the delete should have also persisted to the underlying store
			storedOut, err := storage.NewGetOp[*TestValue](client, k).Exec(t.Context())
			require.Nil(t, storedOut)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})
	})
}
