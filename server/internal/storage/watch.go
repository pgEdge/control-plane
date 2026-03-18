package storage

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/time/rate"
)

type watchOp[V Value] struct {
	mu       sync.Mutex
	client   *clientv3.Client
	key      string
	options  []clientv3.OpOption
	revision int64
	ch       clientv3.WatchChan
	cancel   context.CancelFunc
	errCh    chan error
	running  atomic.Bool
}

func NewWatchOp[V Value](client *clientv3.Client, key string, options ...clientv3.OpOption) WatchOp[V] {
	return &watchOp[V]{
		client:  client,
		key:     key,
		options: options,
		// An error will terminate the watch, so we only need capacity for 1
		errCh: make(chan error, 1),
	}
}

func NewWatchPrefixOp[V Value](client *clientv3.Client, key string, options ...clientv3.OpOption) WatchOp[V] {
	allOptions := []clientv3.OpOption{clientv3.WithPrefix()}
	allOptions = append(allOptions, options...)

	return &watchOp[V]{
		client:  client,
		key:     ensureTrailingSlash(key),
		options: allOptions,
		// An error will terminate the watch, so we only need capacity for 1
		errCh: make(chan error, 1),
	}
}

// load performs an initial Get of the current items at the watched key or
// prefix, calls handle for each, stores the revision from the response header.
func (o *watchOp[V]) load(ctx context.Context, handle func(e *Event[V]) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	resp, err := o.client.Get(ctx, o.key, o.options...)
	if err != nil {
		return fmt.Errorf("failed to get initial items for watch: %w", err)
	}

	for _, kv := range resp.Kvs {
		if err := handle(convertKVToEvent[V](kv)); err != nil {
			return err
		}
	}

	o.revision = resp.Header.Revision

	return nil
}

// setupWatch initializes the etcd watch channel, starting from o.revision if
// it has been set.
func (o *watchOp[V]) setupWatch(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.cancel != nil {
		// This will be true if we're restarting the watch.
		o.cancel()
	}

	watchOptions := slices.Clone(o.options)
	if o.revision > 0 {
		watchOptions = append(watchOptions, clientv3.WithRev(o.revision+1))
	}

	ctx, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	o.ch = o.client.Watch(ctx, o.key, watchOptions...)

	return nil
}

func (o *watchOp[V]) reportErr(err error) {
	if err != nil && o.running.Load() {
		// We avoid reporting errors if the watch was intentionally stopped.
		o.errCh <- err
	}
}

func (o *watchOp[V]) Watch(ctx context.Context, handle func(e *Event[V]) error) error {
	if o.running.Load() {
		return ErrWatchAlreadyInProgress
	}
	o.running.Store(true)

	if err := o.load(ctx, handle); err != nil {
		return err
	}
	go func() {
		// Allow 1 restart per second
		restartLimiter := rate.NewLimiter(1, 1)

		for {
			if err := o.setupWatch(ctx); err != nil {
				o.reportErr(err)
				return
			}
		eventLoop:
			for {
				select {
				case resp := <-o.ch:
					if resp.Header.Revision > o.revision {
						// We always want to bump this revision, even in case of
						// an error.
						o.revision = resp.Header.Revision
					}
					if err := resp.Err(); err != nil {
						// The watch can be interrupted for a few benign
						// reasons. Rather than push that down to the clients,
						// we only report an error if we're unable to open the
						// watch again.
						break eventLoop
					}
					for _, event := range resp.Events {
						if err := handle(convertEvent[V](event)); err != nil {
							o.reportErr(err)
							o.Close()
							return
						}
					}
				case <-ctx.Done():
					o.reportErr(ctx.Err())
					return
				}
			}

			if !o.running.Load() {
				// Exit if Close was called.
				return
			}
			if err := ctx.Err(); err != nil {
				o.reportErr(err)
				return
			}
			if err := restartLimiter.Wait(ctx); err != nil {
				o.reportErr(fmt.Errorf("failed to wait for next watch restart: %w", err))
				return
			}
		}
	}()

	return nil
}

func (o *watchOp[V]) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.running.Store(false)
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
}

func (o *watchOp[V]) Error() <-chan error {
	return o.errCh
}

func (o *watchOp[V]) PropagateErrors(ctx context.Context, ch chan error) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-o.errCh:
				// We intentionally drop errors that happen after the
				// application context is cancelled.
				if ctx.Err() == nil {
					ch <- err
				}
			}
		}
	}()
}

func convertKVToEvent[V Value](kv *mvccpb.KeyValue) *Event[V] {
	v, err := decodeKV[V](kv)
	if err != nil {
		return &Event[V]{
			Type: EventTypeError,
			Err:  err,
		}
	}
	return &Event[V]{
		Type:     EventTypePut,
		Key:      string(kv.Key),
		Value:    v,
		IsCreate: kv.CreateRevision == kv.ModRevision,
	}
}

func convertEvent[V Value](in *clientv3.Event) *Event[V] {
	key := string(in.Kv.Key)
	var val V
	if len(in.Kv.Value) > 0 {
		v, err := decodeKV[V](in.Kv)
		if err != nil {
			return &Event[V]{
				Type: EventTypeError,
				Err:  err,
			}
		}
		val = v
	}

	var eventType EventType
	switch in.Type {
	case clientv3.EventTypeDelete:
		eventType = EventTypeDelete
	case clientv3.EventTypePut:
		eventType = EventTypePut
	default:
		eventType = EventTypeUnknown
	}

	return &Event[V]{
		Type:     eventType,
		Key:      key,
		Value:    val,
		IsCreate: in.IsCreate(),
		IsModify: in.IsModify(),
	}
}
