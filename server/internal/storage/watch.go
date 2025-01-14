package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pgEdge/control-plane/server/internal/utils"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type watchOp[V Value] struct {
	mu      sync.Mutex
	client  EtcdClient
	key     string
	options []clientv3.OpOption
	ch      clientv3.WatchChan
	cancel  context.CancelFunc
}

func NewWatchOp[V Value](client EtcdClient, key string, options ...clientv3.OpOption) WatchOp[V] {
	return &watchOp[V]{
		client:  client,
		key:     key,
		options: options,
	}
}

func NewWatchPrefixOp[V Value](client EtcdClient, key string, options ...clientv3.OpOption) WatchOp[V] {
	allOptions := []clientv3.OpOption{clientv3.WithPrefix()}
	allOptions = append(allOptions, options...)

	return &watchOp[V]{
		client:  client,
		key:     key,
		options: allOptions,
	}
}

func (o *watchOp[V]) start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.ch != nil {
		return ErrWatchAlreadyInProgress
	}

	ctx, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	o.ch = o.client.Watch(ctx, o.key, o.options...)

	return nil
}

func (o *watchOp[V]) Watch(ctx context.Context, handle func(e *Event[V])) error {
	if err := o.start(ctx); err != nil {
		return err
	}

	go func() {
		o.mu.Lock()
		defer o.mu.Unlock()

		for resp := range o.ch {
			if err := resp.Err(); err != nil {
				defer o.Close()
				handle(&Event[V]{
					Err: err,
				})
				return
			}

			for _, event := range resp.Events {
				handle(convertEvent[V](event))
			}
		}
	}()

	return nil
}

func (o *watchOp[V]) Until(ctx context.Context, timeout time.Duration, handle func(e *Event[V]) bool) error {
	defer o.Close()

	err := utils.WithTimeout(ctx, timeout, func(ctx context.Context) error {
		if err := o.start(ctx); err != nil {
			return err
		}

		o.mu.Lock()
		defer o.mu.Unlock()

		for resp := range o.ch {
			if err := resp.Err(); err != nil {
				return fmt.Errorf("watch failed: %w", err)
			}

			for _, event := range resp.Events {
				if handle(convertEvent[V](event)) {
					return nil
				}
			}
		}

		return nil
	})
	if errors.Is(err, utils.ErrTimedOut) {
		// Convert to a more specific timeout error
		return ErrWatchUntilTimedOut
	}
	return err
}

func (o *watchOp[V]) Close() {
	if o.cancel != nil {
		o.cancel()
	}
	o.mu.Lock()
	o.ch = nil
	o.mu.Unlock()
}

func convertEvent[V Value](in *clientv3.Event) *Event[V] {
	key := string(in.Kv.Key)
	val, err := decodeKV[V](in.Kv)
	if err != nil {
		return &Event[V]{
			Err: err,
		}
	}

	var eventType EventType
	switch in.Type {
	case clientv3.EventTypeDelete:
		eventType = EventTypeDelete
	case clientv3.EventTypePut:
		eventType = EventTypePut

	}

	return &Event[V]{
		Type:     eventType,
		Key:      key,
		Value:    val,
		IsCreate: in.IsCreate(),
		IsModify: in.IsModify(),
	}
}
