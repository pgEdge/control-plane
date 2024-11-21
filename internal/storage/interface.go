package storage

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdClient interface {
	clientv3.KV
	clientv3.Lease
}

// Value is the interface that all stored values must adhere to. Values must be
// JSON-serializable and have a 'version' field that they expose through the
// methods on this interface. The 'version' field should be omitted from the
// JSON representation using a `json:"-"` tag.
type Value interface {
	Version() int64
	SetVersion(version int64)
}

// TxnOperation is a storage operation that can be used in a transaction.
type TxnOperation interface {
	Ops(ctx context.Context) ([]clientv3.Op, error)
	Cmps() []clientv3.Cmp
}

// Txn is a group of operations that will be executed together in a transaction.
// Similar to transactions in other systems, if any of the operations contains a
// condition, such as the CreateOp operation, and that condition fails, the
// entire transaction will fail. Each operation in the transaction must operate
// on a unique key.
type Txn interface {
	AddOps(ops ...TxnOperation)
	Commit(ctx context.Context) error
}

// GetOp is an operation that returns a single value.
type GetOp[V Value] interface {
	Exec(ctx context.Context) (V, error)
}

// GetMultipleOp is an operation that returns multiple values.
type GetMultipleOp[V Value] interface {
	Exec(ctx context.Context) ([]V, error)
}

// ExistsOp is an operation that returns true if a the given key(s) exist.
type ExistsOp interface {
	Exec(ctx context.Context) (bool, error)
}

// PutOp is an operation that puts a key-value pair into storage.
type PutOp[V Value] interface {
	TxnOperation
	// WithTTL sets a time-to-live for this value. The value will automatically
	// be removed after the TTL has expired.
	WithTTL(ttl time.Duration) PutOp[V]
	Exec(ctx context.Context) error
}

// DeleteOp is an operation that deletes one or more values from storage, and
// returns the number of values deleted.
type DeleteOp interface {
	TxnOperation
	Exec(ctx context.Context) (int64, error)
}

// DeleteValueOp is a delete operation that deletes a single value from storage
// and enforces value version constraints. Implementations should return an
// ErrValueVersionMismatch if the constraint fails.
type DeleteValueOp[V Value] interface {
	TxnOperation
	Exec(ctx context.Context) error
}
