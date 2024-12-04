package storagetest

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/internal/storage"
)

type fakeGetOp[V storage.Value] struct {
	exec func() (V, error)
}

// NewFakeGetOp returns a fake GetOp with a pluggable implementation for
// testing.
func NewFakeGetOp[V storage.Value](exec func() (V, error)) storage.GetOp[V] {
	return &fakeGetOp[V]{
		exec: exec,
	}
}

func (o *fakeGetOp[V]) Exec(_ context.Context) (V, error) {
	return o.exec()
}

type fakeGetMultipleOp[V storage.Value] struct {
	exec func() ([]V, error)
}

// NewFakeGetMultipleOp returns a fake GetMultipleOp with a pluggable
// implementation for testing.
func NewFakeGetMultipleOp[V storage.Value](exec func() ([]V, error)) storage.GetMultipleOp[V] {
	return &fakeGetMultipleOp[V]{
		exec: exec,
	}
}

func (o *fakeGetMultipleOp[V]) Exec(_ context.Context) ([]V, error) {
	return o.exec()
}

type fakeExistsOp struct {
	exec func() (bool, error)
}

// NewFakeExistsOp returns a fake ExistsOp with a pluggable implementation for
// testing.
func NewFakeExistsOp[V storage.Value](exec func() (bool, error)) storage.ExistsOp {
	return &fakeExistsOp{
		exec: exec,
	}
}

func (o *fakeExistsOp) Exec(_ context.Context) (bool, error) {
	return o.exec()
}

type fakePutOp[V storage.Value] struct {
	exec func() error
}

// NewFakePutOp returns a fake PutOp with a pluggable implementation for
// testing.
func NewFakePutOp[V storage.Value](exec func() error) storage.PutOp[V] {
	return &fakePutOp[V]{
		exec: exec,
	}
}

func (o *fakePutOp[V]) Ops(_ context.Context) ([]clientv3.Op, error) {
	return nil, nil
}

func (o *fakePutOp[V]) Cmps() []clientv3.Cmp {
	return nil
}

func (o *fakePutOp[V]) WithTTL(_ time.Duration) storage.PutOp[V] {
	return o
}

func (o *fakePutOp[V]) Exec(_ context.Context) error {
	return o.exec()
}

type fakeDeleteOp struct {
	exec func() (int64, error)
}

// NewFakeDeleteOp returns a fake DeleteOp with a pluggable implementation for
// testing.
func NewFakeDeleteOp(exec func() (int64, error)) storage.DeleteOp {
	return &fakeDeleteOp{
		exec: exec,
	}
}

func (o *fakeDeleteOp) Ops(_ context.Context) ([]clientv3.Op, error) {
	return nil, nil
}

func (o *fakeDeleteOp) Cmps() []clientv3.Cmp {
	return nil
}

func (o *fakeDeleteOp) Exec(_ context.Context) (int64, error) {
	return o.exec()
}

type fakeDeleteValueOp struct {
	exec func() error
}

// NewFakeDeleteValueOp returns a fake DeleteValueOp with a pluggable
// implementation for testing.
func NewFakeDeleteValueOp[V storage.Value](exec func() error) storage.DeleteValueOp[V] {
	return &fakeDeleteValueOp{
		exec: exec,
	}
}

func (o *fakeDeleteValueOp) Ops(_ context.Context) ([]clientv3.Op, error) {
	return nil, nil
}

func (o *fakeDeleteValueOp) Cmps() []clientv3.Cmp {
	return nil
}

func (o *fakeDeleteValueOp) Exec(_ context.Context) error {
	return o.exec()
}

type fakeTxn struct {
	commit func() error
}

// NewFakeTxn returns a fake Txn with a pluggable implementation for testing.
func NewFakeTxn(commit func() error) storage.Txn {
	return &fakeTxn{
		commit: commit,
	}
}

func (t *fakeTxn) AddOps(_ ...storage.TxnOperation) {}

func (t *fakeTxn) Commit(_ context.Context) error {
	return t.commit()
}
