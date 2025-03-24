package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type getOp[V Value] struct {
	client  *clientv3.Client
	key     string
	options []clientv3.OpOption
}

// NewGetOp returns an operation that returns a single value by key.
func NewGetOp[V Value](client *clientv3.Client, key string, options ...clientv3.OpOption) GetOp[V] {
	return &getOp[V]{
		client:  client,
		key:     key,
		options: options,
	}
}

func (o *getOp[V]) Exec(ctx context.Context) (V, error) {
	var zero V
	resp, err := o.client.Get(ctx, o.key, o.options...)
	if err != nil {
		return zero, fmt.Errorf("failed to get %q: %w", o.key, err)
	}
	vals, err := DecodeGetResponse[V](resp)
	if err != nil {
		return zero, err
	}
	if len(vals) < 1 {
		return zero, fmt.Errorf("%q: %w", o.key, ErrNotFound)
	}

	return vals[0], nil
}

type getMultipleOp[V Value] struct {
	client  *clientv3.Client
	keys    []string
	options []clientv3.OpOption
}

// NewGetMultipleOp returns an operation that returns multiple values by key.
func NewGetMultipleOp[V Value](client *clientv3.Client, keys []string, options ...clientv3.OpOption) GetMultipleOp[V] {
	return &getMultipleOp[V]{
		client:  client,
		keys:    keys,
		options: options,
	}
}

func (o *getMultipleOp[V]) Exec(ctx context.Context) ([]V, error) {
	ops := make([]clientv3.Op, len(o.keys))
	for idx, key := range o.keys {
		ops[idx] = clientv3.OpGet(key, o.options...)
	}
	resp, err := o.client.Txn(ctx).
		Then(ops...).
		Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to get %d keys: %w", len(o.keys), err)
	}
	var vals []V
	for _, r := range resp.Responses {
		v, err := decodeKVs[V](r.GetResponseRange().Kvs)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v...)
	}

	return vals, nil
}

type getPrefixOp[V Value] struct {
	client  *clientv3.Client
	prefix  string
	options []clientv3.OpOption
}

// NewGetPrefixOp returns an operation that returns multiple values by prefix.
func NewGetPrefixOp[V Value](client *clientv3.Client, prefix string, options ...clientv3.OpOption) GetMultipleOp[V] {
	return &getPrefixOp[V]{
		client:  client,
		prefix:  prefix,
		options: options,
	}
}

func (o *getPrefixOp[V]) Exec(ctx context.Context) ([]V, error) {
	options := []clientv3.OpOption{clientv3.WithPrefix()}
	options = append(options, o.options...)
	resp, err := o.client.Get(ctx, o.prefix, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to get prefix %q: %w", o.prefix, err)
	}
	return DecodeGetResponse[V](resp)
}

type getRangeOp[V Value] struct {
	start   string
	end     string
	client  *clientv3.Client
	options []clientv3.OpOption
}

// NewGetRangeOp returns an operation that returns values in the range
// [start, end).
func NewGetRangeOp[V Value](client *clientv3.Client, start, end string, options ...clientv3.OpOption) GetMultipleOp[V] {
	return &getRangeOp[V]{
		client:  client,
		start:   start,
		end:     end,
		options: options,
	}
}

func (o *getRangeOp[V]) Exec(ctx context.Context) ([]V, error) {
	options := []clientv3.OpOption{clientv3.WithRange(o.end)}
	options = append(options, o.options...)
	resp, err := o.client.Get(ctx, o.start, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to get range [%q, %q): %w", o.start, o.end, err)
	}
	return DecodeGetResponse[V](resp)
}

type existsOp struct {
	client *clientv3.Client
	key    string
}

// NewExistsOp returns an operation that returns true if a key exists.
func NewExistsOp(client *clientv3.Client, key string) ExistsOp {
	return &existsOp{
		client: client,
		key:    key,
	}
}

func (o *existsOp) Exec(ctx context.Context) (bool, error) {
	resp, err := o.client.Get(ctx, o.key, clientv3.WithCountOnly())
	if err != nil {
		return false, fmt.Errorf("failed get operation: %w", err)
	}

	return resp.Count > 0, nil
}

// DecodeGetResponse is a helper function to extract typed values from a
// clientv3.GetResponse
func DecodeGetResponse[V Value](resp *clientv3.GetResponse) ([]V, error) {
	return decodeKVs[V](resp.Kvs)
}

func decodeKVs[V Value](kvs []*mvccpb.KeyValue) ([]V, error) {
	vals := make([]V, len(kvs))
	for idx, kv := range kvs {
		v, err := decodeKV[V](kv)
		if err != nil {
			return nil, err
		}
		vals[idx] = v
	}

	return vals, nil
}

func decodeKV[V Value](kv *mvccpb.KeyValue) (V, error) {
	var zero V
	key := string(kv.Key)
	val, err := decodeJSON[V](kv.Value)
	if err != nil {
		return zero, fmt.Errorf("failed to decode %q: %w", key, err)
	}
	val.SetVersion(kv.Version)
	return val, nil
}

func decodeJSON[V any](val []byte) (V, error) {
	var out V
	if err := json.Unmarshal(val, &out); err != nil {
		return out, err
	}
	return out, nil
}
