package storage

import (
	"context"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type deleteKeyOp struct {
	client  *clientv3.Client
	key     string
	options []clientv3.OpOption
}

// NewDeleteKeyOp returns an operation that deletes a single value by key.
func NewDeleteKeyOp(client *clientv3.Client, key string, options ...clientv3.OpOption) DeleteOp {
	return &deleteKeyOp{
		client:  client,
		key:     key,
		options: options,
	}
}

func (o *deleteKeyOp) Ops(ctx context.Context) ([]clientv3.Op, error) {
	return []clientv3.Op{clientv3.OpDelete(o.key, o.options...)}, nil
}

func (o *deleteKeyOp) Cmps() []clientv3.Cmp {
	return nil
}

// Exec returns the number of records deleted.
func (o *deleteKeyOp) Exec(ctx context.Context) (int64, error) {
	resp, err := o.client.Delete(ctx, o.key, o.options...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete key %q: %w", o.key, err)
	}

	return resp.Deleted, nil
}

type deletePrefixOp struct {
	client  *clientv3.Client
	prefix  string
	options []clientv3.OpOption
}

// NewDeletePrefixOp returns an operation that deletes a multiple values by
// prefix.
func NewDeletePrefixOp(client *clientv3.Client, prefix string, options ...clientv3.OpOption) DeleteOp {
	return &deletePrefixOp{
		client:  client,
		prefix:  ensureTrailingSlash(prefix),
		options: options,
	}
}

func (o *deletePrefixOp) Ops(ctx context.Context) ([]clientv3.Op, error) {
	options := []clientv3.OpOption{clientv3.WithPrefix()}
	options = append(options, o.options...)
	return []clientv3.Op{clientv3.OpDelete(o.prefix, options...)}, nil
}

func (o *deletePrefixOp) Cmps() []clientv3.Cmp {
	return nil
}

// Exec returns the number of values that were deleted.
func (o *deletePrefixOp) Exec(ctx context.Context) (int64, error) {
	options := []clientv3.OpOption{clientv3.WithPrefix()}
	options = append(options, o.options...)
	resp, err := o.client.Delete(ctx, o.prefix, options...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete prefix %q: %w", o.prefix, err)
	}

	return resp.Deleted, nil
}

type deleteValueOp[V Value] struct {
	client  *clientv3.Client
	key     string
	val     V
	options []clientv3.OpOption
}

// NewDeleteValueOp deletes a single value if its version matches the given
// value's version. Its Exec method will return an ErrValueVersionMismatch if
// the stored value version did not match the given value version.
func NewDeleteValueOp[V Value](client *clientv3.Client, key string, val V, options ...clientv3.OpOption) DeleteValueOp[V] {
	return &deleteValueOp[V]{
		client:  client,
		key:     key,
		val:     val,
		options: options,
	}
}

func (o *deleteValueOp[V]) Ops(ctx context.Context) ([]clientv3.Op, error) {
	return []clientv3.Op{clientv3.OpDelete(o.key, o.options...)}, nil
}

func (o *deleteValueOp[V]) Cmps() []clientv3.Cmp {
	return []clientv3.Cmp{clientv3.Compare(clientv3.Version(o.key), "=", o.val.Version())}
}

// Exec returns an ErrValueVersionMismatch if the stored value version did not
// match the given value version.
func (o *deleteValueOp[V]) Exec(ctx context.Context) error {
	ops, _ := o.Ops(ctx)
	resp, err := o.client.Txn(ctx).
		If(o.Cmps()...).
		Then(ops...).
		Commit()
	if err != nil {
		return fmt.Errorf("failed to delete value %q: %w", o.key, err)
	}
	if !resp.Succeeded {
		return ErrValueVersionMismatch
	}

	return nil
}
