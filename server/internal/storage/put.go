package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type putOp[V Value] struct {
	client              *clientv3.Client
	key                 string
	val                 V
	ttl                 *time.Duration
	options             []clientv3.OpOption
	shouldUpdateVersion bool
}

// NewPutOp returns an operation that stores a key value pair with an optional
// time-to-live. This operation does not enforce any version constraints.
func NewPutOp[V Value](client *clientv3.Client, key string, val V, options ...clientv3.OpOption) PutOp[V] {
	return &putOp[V]{
		client:  client,
		key:     key,
		val:     val,
		options: options,
	}
}

func (o *putOp[V]) Ops(ctx context.Context) ([]clientv3.Op, error) {
	return putOps(ctx, o.client, o.key, o.val, o.ttl, o.options...)
}

func (o *putOp[V]) Cmps() []clientv3.Cmp {
	return nil
}

func (o *putOp[V]) WithTTL(ttl time.Duration) PutOp[V] {
	o.ttl = &ttl
	return o
}

func (o *putOp[V]) WithUpdatedVersion() PutOp[V] {
	o.shouldUpdateVersion = true
	o.options = append(o.options, clientv3.WithPrevKV())
	return o
}

func (o *putOp[V]) Exec(ctx context.Context) error {
	ops, err := o.Ops(ctx)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(ctx, ops[0])
	if err != nil {
		return fmt.Errorf("failed to put %q: %w", o.key, err)
	}
	if o.shouldUpdateVersion {
		put := resp.Put()
		if put != nil && put.PrevKv != nil {
			o.val.SetVersion(put.PrevKv.Version + 1)
		} else {
			// PrevKV is nil for creates
			o.val.SetVersion(o.val.Version() + 1)
		}
	}

	return nil
}

func (o *putOp[V]) UpdateVersionEnabled() bool {
	return o.shouldUpdateVersion
}

func (o *putOp[V]) UpdateVersion(prevKVs map[string]*mvccpb.KeyValue) {
	updateVersion(o.key, o.val, prevKVs)
}

type createOp[V Value] struct {
	client              *clientv3.Client
	key                 string
	val                 V
	ttl                 *time.Duration
	options             []clientv3.OpOption
	shouldUpdateVersion bool
}

// NewCreateOp returns an operation that creates a key value pair with an
// optional time-to-live. This operation will fail with ErrAlreadyExists if the
// given key already exists.
func NewCreateOp[V Value](client *clientv3.Client, key string, val V, options ...clientv3.OpOption) PutOp[V] {
	return &createOp[V]{
		client:  client,
		key:     key,
		val:     val,
		options: options,
	}
}

func (o *createOp[V]) Ops(ctx context.Context) ([]clientv3.Op, error) {
	return putOps(ctx, o.client, o.key, o.val, o.ttl, o.options...)
}

func (o *createOp[V]) Cmps() []clientv3.Cmp {
	return []clientv3.Cmp{clientv3.Compare(clientv3.Version(o.key), "=", 0)}
}

func (o *createOp[V]) WithTTL(ttl time.Duration) PutOp[V] {
	o.ttl = &ttl
	return o
}

func (o *createOp[V]) WithUpdatedVersion() PutOp[V] {
	o.shouldUpdateVersion = true
	o.options = append(o.options, clientv3.WithPrevKV())
	return o
}

func (o *createOp[V]) Exec(ctx context.Context) error {
	ops, err := o.Ops(ctx)
	if err != nil {
		return err
	}
	resp, err := o.client.Txn(ctx).
		If(o.Cmps()...).
		Then(ops...).
		Commit()
	if err != nil {
		return fmt.Errorf("failed to create %q: %w", o.key, err)
	}
	if !resp.Succeeded {
		return fmt.Errorf("%q: %w", o.key, ErrAlreadyExists)
	}
	if o.shouldUpdateVersion {
		updateVersion(o.key, o.val, extractPrevKVs(resp))
	}

	return nil
}

func (o *createOp[V]) UpdateVersionEnabled() bool {
	return o.shouldUpdateVersion
}

func (o *createOp[V]) UpdateVersion(prevKVs map[string]*mvccpb.KeyValue) {
	updateVersion(o.key, o.val, prevKVs)
}

type updateOp[V Value] struct {
	client              *clientv3.Client
	key                 string
	val                 V
	ttl                 *time.Duration
	options             []clientv3.OpOption
	shouldUpdateVersion bool
}

// NewUpdateOp returns an operation updates an existing key value pair with a
// new value and an optional time-to-live. This operation will fail with
// ErrValueVersionMismatch if the stored value's version does not match the
// given value's version. Note that this operation is equivalent to a create
// when the item version is 0.
func NewUpdateOp[V Value](client *clientv3.Client, key string, val V, options ...clientv3.OpOption) PutOp[V] {
	return &updateOp[V]{
		client:  client,
		key:     key,
		val:     val,
		options: options,
	}
}

func (o *updateOp[V]) Ops(ctx context.Context) ([]clientv3.Op, error) {
	return putOps(ctx, o.client, o.key, o.val, o.ttl, o.options...)
}

func (o *updateOp[V]) Cmps() []clientv3.Cmp {
	return []clientv3.Cmp{
		clientv3.Compare(clientv3.Version(o.key), "=", o.val.Version()),
	}
}

func (o *updateOp[V]) WithTTL(ttl time.Duration) PutOp[V] {
	o.ttl = &ttl
	return o
}

func (o *updateOp[V]) WithUpdatedVersion() PutOp[V] {
	o.shouldUpdateVersion = true
	o.options = append(o.options, clientv3.WithPrevKV())
	return o
}

func (o *updateOp[V]) Exec(ctx context.Context) error {
	ops, err := o.Ops(ctx)
	if err != nil {
		return err
	}
	resp, err := o.client.Txn(ctx).
		If(o.Cmps()...).
		Then(ops...).
		Commit()
	if err != nil {
		return fmt.Errorf("failed to update %q: %w", o.key, err)
	}
	if !resp.Succeeded {
		return fmt.Errorf("%q: %w", o.key, ErrValueVersionMismatch)
	}
	if o.shouldUpdateVersion {
		updateVersion(o.key, o.val, extractPrevKVs(resp))
	}

	return nil
}

func (o *updateOp[V]) UpdateVersionEnabled() bool {
	return o.shouldUpdateVersion
}

func (o *updateOp[V]) UpdateVersion(prevKVs map[string]*mvccpb.KeyValue) {
	updateVersion(o.key, o.val, prevKVs)
}

func encodeJSON(val any) (string, error) {
	raw, err := json.Marshal(val)
	if err != nil {
		return "", err
	}
	com, err := compress(raw)
	if err != nil {
		return "", err
	}

	return string(com), nil
}

const compressionThreshold = 2048 // 2KiB

func compress(in []byte) ([]byte, error) {
	if len(in) < compressionThreshold {
		// Don't compress if the data is below our threshold.
		return in, nil
	}
	var b bytes.Buffer
	gw := gzip.NewWriter(&b)
	if _, err := gw.Write(in); err != nil {
		return nil, fmt.Errorf("failed to compress data: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return b.Bytes(), nil
}

func putOps[V Value](
	ctx context.Context,
	client *clientv3.Client,
	key string,
	val V,
	ttl *time.Duration,
	options ...clientv3.OpOption,
) ([]clientv3.Op, error) {
	allOptions := append([]clientv3.OpOption{}, options...)
	if ttl != nil {
		leaseResp, err := client.Grant(ctx, int64(ttl.Seconds()))
		if err != nil {
			return nil, fmt.Errorf("failed to grant lease for %q: %w", key, err)
		}
		allOptions = append(allOptions, clientv3.WithLease(leaseResp.ID))
	}

	encoded, err := encodeJSON(val)
	if err != nil {
		return nil, fmt.Errorf("failed to encode value for %q: %w", key, err)
	}

	return []clientv3.Op{clientv3.OpPut(key, encoded, allOptions...)}, nil
}

func extractPrevKVs(resp *clientv3.TxnResponse) map[string]*mvccpb.KeyValue {
	prevKVs := map[string]*mvccpb.KeyValue{}
	for _, r := range resp.Responses {
		put := r.GetResponsePut()
		if put != nil && put.PrevKv != nil {
			prevKVs[string(put.PrevKv.Key)] = put.PrevKv
		}
	}
	return prevKVs
}

func updateVersion[V Value](key string, item V, prevKVs map[string]*mvccpb.KeyValue) {
	prev, ok := prevKVs[key]
	if ok {
		item.SetVersion(prev.Version + 1)
	} else {
		// PrevKV is nil for creates
		item.SetVersion(item.Version() + 1)
	}
}
