package storage

import (
	"context"
	"fmt"
	"strings"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type txn struct {
	ops    []TxnOperation
	client *clientv3.Client
}

func NewTxn(client *clientv3.Client, ops ...TxnOperation) Txn {
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
	var cachedOps []CachedTxnOp

	opsByKey := map[string][]clientv3.Op{}
	for _, op := range t.ops {
		clientOp, err := op.ClientOp(ctx)
		if err != nil {
			return err
		}
		key := string(clientOp.KeyBytes())
		opsByKey[key] = append(opsByKey[key], clientOp)
		allOps = append(allOps, clientOp)
		allCmps = append(allCmps, op.Cmps()...)
		if c, ok := op.(CachedTxnOp); ok {
			cachedOps = append(cachedOps, c)
		}
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
	for _, o := range t.ops {
		o.UpdateRevision(resp.Header.Revision)
	}

	if !resp.Succeeded {
		return ErrOperationConstraintViolated
	}

	// Update the item version on any operations that support it
	prevKVs := extractPrevKVs(resp)
	for _, o := range t.ops {
		if up, ok := o.(VersionUpdater); ok && up.UpdateVersionEnabled() {
			up.UpdateVersion(prevKVs)
		}
	}
	// Update the cache for all cached operations
	for _, c := range cachedOps {
		c.UpdateCache()
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
