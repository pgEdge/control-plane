package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var _ Cache[*StoredValue] = (*cache[*StoredValue])(nil)

type cachedValue struct {
	// This is the global Etcd database revision.
	revision int64
	// This is the value's version.
	version int64
	// We cache values as JSON so that each read returns a unique instance.
	// Otherwise there's a risk that concurrent transactions could interfere
	// with each other.
	raw []byte
	// Set to true to record a cached delete operation. Values with this flag
	// will get cleared when we get a watch event that's at or beyond the cached
	// delete's revision.
	deleted bool
}

type cache[V Value] struct {
	client *clientv3.Client
	prefix string
	key    func(v V) string

	mu                sync.RWMutex
	items             map[string]*cachedValue
	op                WatchOp[V]
	opMu              sync.Mutex
	errCh             chan error
	lastWatchRevision int64
	tombstones        []string
}

func NewCache[V Value](client *clientv3.Client, prefix string, key func(v V) string) Cache[V] {
	return &cache[V]{
		client: client,
		prefix: ensureTrailingSlash(prefix),
		key:    key,
		items:  map[string]*cachedValue{},
		errCh:  make(chan error, 1),
	}
}

func (c *cache[V]) Start(ctx context.Context) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	if c.op != nil {
		return errors.New("cache has already been started")
	}

	c.op = NewWatchPrefixOp[V](c.client, c.prefix)
	err := c.op.Watch(ctx, func(e *Event[V]) error {
		if e.Type != EventTypePut && e.Type != EventTypeDelete {
			// Return early to avoid locking
			return nil
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		if e.Revision > c.lastWatchRevision {
			c.lastWatchRevision = e.Revision
		}

		switch e.Type {
		case EventTypePut:
			c.unlockedWrite(e.Value, e.Revision)
		case EventTypeDelete:
			c.unlockedDelete(e.Key, e.Revision)
		}

		// Clear out any cached deletes that are older than our latest watch
		// position.
		c.purgeTombstones(e.Revision)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to initialize watch: %w", err)
	}
	c.op.PropagateErrors(ctx, c.errCh)

	return nil
}

func (c *cache[V]) Put(item V, options ...clientv3.OpOption) PutOp[V] {
	return &cachePutOp[V]{
		cache: c,
		item:  item,
		put:   NewPutOp(c.client, c.key(item), item, options...).WithUpdatedVersion(),
	}
}

func (c *cache[V]) Create(item V, options ...clientv3.OpOption) PutOp[V] {
	return &cachePutOp[V]{
		cache: c,
		item:  item,
		put:   NewCreateOp(c.client, c.key(item), item, options...).WithUpdatedVersion(),
	}
}

func (c *cache[V]) Update(item V, options ...clientv3.OpOption) PutOp[V] {
	return &cachePutOp[V]{
		cache: c,
		item:  item,
		put:   NewUpdateOp(c.client, c.key(item), item, options...).WithUpdatedVersion(),
	}
}

func (c *cache[V]) DeleteByKey(key string, options ...clientv3.OpOption) DeleteOp {
	return &cacheDeleteKeyOp[V]{
		cache:  c,
		key:    key,
		delete: NewDeleteKeyOp(c.client, key, options...),
	}
}

func (c *cache[V]) DeleteValue(item V, options ...clientv3.OpOption) DeleteValueOp[V] {
	key := c.key(item)
	return &cacheDeleteValueOp[V]{
		cache:  c,
		key:    key,
		delete: NewDeleteValueOp(c.client, key, item, options...),
	}
}

func (c *cache[V]) DeletePrefix(prefix string, options ...clientv3.OpOption) DeleteOp {
	return &cacheDeletePrefixOp[V]{
		cache:  c,
		prefix: prefix,
		delete: NewDeletePrefixOp(c.client, prefix, options...),
	}
}

func (c *cache[V]) Get(key string) GetOp[V] {
	return &cacheGetOp[V]{
		key:   key,
		cache: c,
	}
}

func (c *cache[V]) GetPrefix(prefix string) GetMultipleOp[V] {
	return &cacheGetPrefixOp[V]{
		prefix: prefix,
		cache:  c,
	}
}

func (c *cache[V]) Stop() {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	if c.op != nil {
		c.op.Close()
		c.op = nil
		c.lastWatchRevision = 0
		c.items = map[string]*cachedValue{}
		c.tombstones = nil
	}
}

func (c *cache[V]) Error() <-chan error {
	return c.errCh
}

func (c *cache[V]) PropagateErrors(ctx context.Context, ch chan error) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-c.errCh:
				select {
				case ch <- err:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

func (c *cache[V]) unlockedWrite(item V, revision int64) {
	key := c.key(item)
	if existing, ok := c.items[key]; ok && existing.revision >= revision {
		// avoid overwriting the item if we already have a newer version of it.
		return
	}
	raw, err := json.Marshal(item)
	if err != nil {
		// This should never happen, but if it does it's due to a programmer
		// error. This needs to crash during development and testing.
		panic(fmt.Errorf("failed to marshal cached value: %w", err))
	}
	c.items[key] = &cachedValue{
		revision: revision,
		version:  item.Version(),
		raw:      raw,
	}
}

func (c *cache[V]) unlockedWriteTombstone(key string, revision int64) {
	if existing, ok := c.items[key]; ok && existing.revision >= revision {
		// avoid overwriting the item if we already have a newer version of it.
		return
	}
	c.items[key] = &cachedValue{
		revision: revision,
		deleted:  true,
	}
	c.tombstones = append(c.tombstones, key)
}

func (c *cache[V]) unlockedDelete(key string, revision int64) {
	existing, ok := c.items[key]
	if !ok || existing.revision > revision {
		// not using >= for this condition so that we can delete the tombstone
		// record for this delete operation.
		return
	}
	delete(c.items, key)
}

func (c *cache[V]) purgeTombstones(revision int64) {
	if len(c.tombstones) == 0 {
		return
	}
	var remaining []string
	for _, key := range c.tombstones {
		existing, ok := c.items[key]
		if !ok {
			continue
		}
		switch {
		case existing.revision > revision:
			remaining = append(remaining, key)
		case existing.deleted:
			delete(c.items, key)
		default:
			// this case can happen if our tombstone was overwritten by a
			// watched put event. doing nothing here means that we leave the
			// existing value alone and filter the key from our tombstone list.
		}
	}
	c.tombstones = remaining
}

func (c *cache[V]) unmarshal(cached *cachedValue) V {
	var v V
	if err := json.Unmarshal(cached.raw, &v); err != nil {
		// This should never happen, but if it does it's due to a programmer
		// error. This needs to crash during development and testing.
		panic(fmt.Errorf("failed to unmarshal cached value: %w", err))
	}
	v.SetVersion(cached.version)
	return v
}

func (c *cache[V]) get(key string) (V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var zero V

	cached, ok := c.items[key]
	if !ok {
		return zero, ErrNotFound
	}
	if cached.deleted {
		return zero, ErrNotFound
	}

	return c.unmarshal(cached), nil
}

func (c *cache[V]) getPrefix(prefix string) ([]V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var items []V
	for key, cached := range c.items {
		if cached.deleted {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			items = append(items, c.unmarshal(cached))
		}
	}

	return items, nil
}

func (c *cache[V]) put(item V, revision int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if revision <= c.lastWatchRevision {
		// Our watch is at or ahead of this operation, so we can skip it.
		return
	}

	c.unlockedWrite(item, revision)
}

func (c *cache[V]) delete(key string, revision int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if revision <= c.lastWatchRevision {
		// Our watch is at or ahead of this operation, so we can skip it.
		return
	}

	c.unlockedWriteTombstone(key, revision)
}

func (c *cache[V]) deletePrefix(prefix string, revision int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if revision <= c.lastWatchRevision {
		// Our watch is at or ahead of this operation, so we can skip it.
		return
	}
	for key := range c.items {
		if strings.HasPrefix(key, prefix) {
			c.unlockedWriteTombstone(key, revision)
		}
	}
}

var _ GetOp[*StoredValue] = (*cacheGetOp[*StoredValue])(nil)

type cacheGetOp[V Value] struct {
	key   string
	cache *cache[V]
}

func (c *cacheGetOp[V]) Exec(_ context.Context) (V, error) {
	return c.cache.get(c.key)
}

var _ GetMultipleOp[*StoredValue] = (*cacheGetPrefixOp[*StoredValue])(nil)

type cacheGetPrefixOp[V Value] struct {
	prefix string
	cache  *cache[V]
}

func (c *cacheGetPrefixOp[V]) Exec(_ context.Context) ([]V, error) {
	return c.cache.getPrefix(c.prefix)
}

var _ PutOp[*StoredValue] = (*cachePutOp[*StoredValue])(nil)
var _ CachedTxnOp = (*cachePutOp[*StoredValue])(nil)

type cachePutOp[V Value] struct {
	cache *cache[V]
	item  V
	put   PutOp[V]
}

func (c *cachePutOp[V]) ClientOp(ctx context.Context) (clientv3.Op, error) {
	return c.put.ClientOp(ctx)
}

func (c *cachePutOp[V]) Cmps() []clientv3.Cmp {
	return c.put.Cmps()
}

func (c *cachePutOp[V]) WithTTL(ttl time.Duration) PutOp[V] {
	c.put = c.put.WithTTL(ttl)
	return c
}

func (c *cachePutOp[V]) WithUpdatedVersion() PutOp[V] {
	// We already enable this by default.
	return c
}

func (c *cachePutOp[V]) Exec(ctx context.Context) error {
	err := c.put.Exec(ctx)
	if err != nil {
		return err
	}
	c.UpdateCache()

	return nil
}

func (c *cachePutOp[V]) Revision() int64 {
	return c.put.Revision()
}

func (c *cachePutOp[V]) UpdateRevision(revision int64) {
	c.put.UpdateRevision(revision)
}

func (o *cachePutOp[V]) UpdateVersionEnabled() bool {
	return o.put.UpdateVersionEnabled()
}

func (o *cachePutOp[V]) UpdateVersion(prevKVs map[string]*mvccpb.KeyValue) {
	o.put.UpdateVersion(prevKVs)
}

func (c *cachePutOp[V]) UpdateCache() {
	c.cache.put(c.item, c.put.Revision())
}

var _ DeleteOp = (*cacheDeleteKeyOp[*StoredValue])(nil)
var _ CachedTxnOp = (*cacheDeleteKeyOp[*StoredValue])(nil)

type cacheDeleteKeyOp[V Value] struct {
	cache  *cache[V]
	key    string
	delete DeleteOp
}

func (c *cacheDeleteKeyOp[V]) ClientOp(ctx context.Context) (clientv3.Op, error) {
	return c.delete.ClientOp(ctx)
}

func (c *cacheDeleteKeyOp[V]) Cmps() []clientv3.Cmp {
	return c.delete.Cmps()
}

func (c *cacheDeleteKeyOp[V]) Exec(ctx context.Context) (int64, error) {
	count, err := c.delete.Exec(ctx)
	if err != nil {
		return count, err
	}
	c.UpdateCache()

	return count, nil
}

func (c *cacheDeleteKeyOp[V]) Revision() int64 {
	return c.delete.Revision()
}

func (c *cacheDeleteKeyOp[V]) UpdateRevision(revision int64) {
	c.delete.UpdateRevision(revision)
}

func (c *cacheDeleteKeyOp[V]) UpdateCache() {
	c.cache.delete(c.key, c.delete.Revision())
}

var _ DeleteValueOp[*StoredValue] = (*cacheDeleteValueOp[*StoredValue])(nil)
var _ CachedTxnOp = (*cacheDeleteValueOp[*StoredValue])(nil)

type cacheDeleteValueOp[V Value] struct {
	cache  *cache[V]
	key    string
	delete DeleteValueOp[V]
}

func (c *cacheDeleteValueOp[V]) ClientOp(ctx context.Context) (clientv3.Op, error) {
	return c.delete.ClientOp(ctx)
}

func (c *cacheDeleteValueOp[V]) Cmps() []clientv3.Cmp {
	return c.delete.Cmps()
}

func (c *cacheDeleteValueOp[V]) Exec(ctx context.Context) error {
	if err := c.delete.Exec(ctx); err != nil {
		return err
	}
	c.UpdateCache()

	return nil
}

func (c *cacheDeleteValueOp[V]) Revision() int64 {
	return c.delete.Revision()
}

func (c *cacheDeleteValueOp[V]) UpdateRevision(revision int64) {
	c.delete.UpdateRevision(revision)
}

func (c *cacheDeleteValueOp[V]) UpdateCache() {
	c.cache.delete(c.key, c.delete.Revision())
}

var _ DeleteOp = (*cacheDeletePrefixOp[*StoredValue])(nil)
var _ CachedTxnOp = (*cacheDeletePrefixOp[*StoredValue])(nil)

type cacheDeletePrefixOp[V Value] struct {
	cache  *cache[V]
	prefix string
	delete DeleteOp
}

func (c *cacheDeletePrefixOp[V]) ClientOp(ctx context.Context) (clientv3.Op, error) {
	return c.delete.ClientOp(ctx)
}

func (c *cacheDeletePrefixOp[V]) Cmps() []clientv3.Cmp {
	return c.delete.Cmps()
}

func (c *cacheDeletePrefixOp[V]) Exec(ctx context.Context) (int64, error) {
	count, err := c.delete.Exec(ctx)
	if err != nil {
		return count, err
	}
	c.UpdateCache()

	return count, nil
}

func (c *cacheDeletePrefixOp[V]) Revision() int64 {
	return c.delete.Revision()
}

func (c *cacheDeletePrefixOp[V]) UpdateRevision(revision int64) {
	c.delete.UpdateRevision(revision)
}

func (c *cacheDeletePrefixOp[V]) UpdateCache() {
	c.cache.deletePrefix(c.prefix, c.delete.Revision())
}
