package storage

import (
	"context"
	"fmt"
	"strings"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type txn struct {
	ops    []TxnOperation
	client EtcdClient
}

func NewTxn(client EtcdClient, ops ...TxnOperation) Txn {
	return &txn{
		client: client,
		ops:    ops,
	}
}

func (t *txn) AddOps(ops ...TxnOperation) {
	t.ops = append(t.ops, ops...)
}

func (t *txn) Commit(ctx context.Context) error {
	var allOps []clientv3.Op
	var allCmps []clientv3.Cmp

	opsByKey := map[string][]clientv3.Op{}
	for _, op := range t.ops {
		ops, err := op.Ops(ctx)
		if err != nil {
			return err
		}
		for _, o := range ops {
			key := string(o.KeyBytes())
			opsByKey[key] = append(opsByKey[key], o)
		}
		allOps = append(allOps, ops...)
		allCmps = append(allCmps, op.Cmps()...)
	}

	// Etcd will reject the transaction if there are duplicate keys, and it
	// doesn't give a helpful error message. We can produce a better error by
	// preemptively checking for duplicates ourselves.
	var duplicates []string
	for key, ops := range opsByKey {
		if len(ops) > 1 {
			for _, o := range ops {
				d := fmt.Sprintf("\t%s %s", opType(o), key)
				duplicates = append(duplicates, d)
			}
		}
	}
	if len(duplicates) > 0 {
		joined := strings.Join(duplicates, "\n")
		return fmt.Errorf("%w:\n%s", ErrDuplicateKeysInTransaction, joined)
	}

	resp, err := t.client.Txn(ctx).
		If(allCmps...).
		Then(allOps...).
		Commit()
	if err != nil {
		return fmt.Errorf("failed transaction: %w", err)
	}

	if !resp.Succeeded {
		return ErrOperationConstraintViolated
	}

	return nil
}

func opType(op clientv3.Op) string {
	switch {
	case op.IsDelete():
		return "delete"
	case op.IsGet():
		return "get"
	case op.IsPut():
		return "put"
	default:
		return "unknown_operation"
	}
}
