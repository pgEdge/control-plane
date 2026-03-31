package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests directly invoke the unexported cache methods to simulate
// different event and operation orders that are difficult to reproduce reliably
// with the public interface.
func TestCacheOrdering(t *testing.T) {
	t.Run("stale put after watch delete", func(t *testing.T) {
		// Simulates when a write-through put's UpdateCache fires after the
		// watch has already processed the delete for the same key at a higher
		// revision. The put should not resurrect the key.
		c := newOrderingCache()
		item := &cacheTestItem{K: "foo", V: "bar"}

		seedCache(c, "foo", "bar", 5)
		simulateWatchDelete(c, "foo", 10) // watch advances to R=10, key gone

		// Delayed write-through put at R=5 — must not resurrect the key because
		// it's from an older revision.
		c.put(item, 5)

		_, err := c.get("foo")
		assert.ErrorIs(t, err, ErrNotFound, "stale write-through put must not resurrect a watched delete")
	})

	t.Run("stale delete after watch put", func(t *testing.T) {
		// Simulates when a write-through delete's UpdateCache fires after the
		// watch has already delivered a put at a higher revision. The delete
		// should not erase the newer watched value.
		c := newOrderingCache()
		item := &cacheTestItem{K: "foo", V: "new"}

		simulateWatchPut(c, item, 10) // watch advances to R=10, key present

		// Delayed write-through delete at R=5 — must not erase the key.
		c.delete("foo", 5)

		val, err := c.get("foo")
		require.NoError(t, err)
		assert.Equal(t, "new", val.V, "stale write-through delete must not erase a newer watched put")
	})

	t.Run("tombstone blocks stale put", func(t *testing.T) {
		// Simulates when write-through delete fires before its watch event,
		// writing a tombstone, and a stale watch put (for a revision before the
		// delete) must not overwrite it.
		c := newOrderingCache()
		staleItem := &cacheTestItem{K: "foo", V: "old"}

		seedCache(c, "foo", "old", 5)
		c.delete("foo", 10) // write-through delete fires first, tombstone at R=10

		// A stale watch put at R=5 must not overwrite the tombstone.
		c.mu.Lock()
		c.unlockedWrite(staleItem, 5)
		c.mu.Unlock()

		_, err := c.get("foo")
		assert.ErrorIs(t, err, ErrNotFound, "tombstone must block a stale watch put")
	})

	t.Run("tombstone allows newer put", func(t *testing.T) {
		// Simulates a re-create after delete: write-through delete fires first
		// (creating a tombstone), then a watch put at a higher revision
		// arrives. The put should win because it represents a genuinely newer
		// write in etcd's ordering.
		c := newOrderingCache()
		newItem := &cacheTestItem{K: "foo", V: "recreated"}

		seedCache(c, "foo", "old", 5)
		c.delete("foo", 8) // write-through fires first

		// Watch delivers a put at R=12 (a re-create after the delete).
		simulateWatchPut(c, newItem, 12)

		val, err := c.get("foo")
		require.NoError(t, err)
		assert.Equal(t, "recreated", val.V, "watch put at higher revision must overwrite tombstone")
	})

	t.Run("write-through delete then watch event", func(t *testing.T) {
		// Simulates the normal ordering (write-through fires before watch) for
		// a delete: the tombstone is written by the write-through, then the
		// matching watch event cleans it up. Neither the tombstone nor the
		// prior value should be readable after the watch event.
		c := newOrderingCache()

		seedCache(c, "foo", "bar", 5)
		c.delete("foo", 10) // write-through fires first, tombstone at R=10

		// The tombstone should be invisible to readers immediately.
		_, err := c.get("foo")
		assert.ErrorIs(t, err, ErrNotFound, "tombstone must be invisible to readers")
		assert.Len(t, c.tombstones, 1, "tombstone should be pending cleanup")

		// The matching watch event arrives at the same revision and cleans up.
		simulateWatchDelete(c, "foo", 10)

		_, err = c.get("foo")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Empty(t, c.tombstones, "tombstone must be purged once watch catches up")
	})
}

// cacheTestItem is a minimal Value implementation for white-box cache tests.
type cacheTestItem struct {
	StoredValue
	K string `json:"k"`
	V string `json:"v"`
}

func newOrderingCache() *cache[*cacheTestItem] {
	return &cache[*cacheTestItem]{
		items: map[string]*cachedValue{},
		key:   func(v *cacheTestItem) string { return v.K },
	}
}

// seed writes a key directly at a given revision, bypassing all guards.
// This simulates the state of the cache after an initial load or prior event.
func seedCache(c *cache[*cacheTestItem], key, value string, revision int64) {
	c.unlockedWrite(&cacheTestItem{K: key, V: value}, revision)
}

// simulateWatchPut simulates a watch event delivering a put at the given revision.
// In production this runs under c.mu held by the Start() watch handler.
func simulateWatchPut(c *cache[*cacheTestItem], item *cacheTestItem, revision int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastWatchRevision = revision
	c.unlockedWrite(item, revision)
	c.purgeTombstones(revision)
}

// simulateWatchDelete simulates a watch event delivering a delete at the given revision.
func simulateWatchDelete(c *cache[*cacheTestItem], key string, revision int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastWatchRevision = revision
	c.unlockedDelete(key, revision)
	c.purgeTombstones(revision)
}
