// server/internal/migrate/store.go
package migrate

import (
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Store wraps all migration-related stores.
type Store struct {
	Revision *RevisionStore
	Result   *ResultStore
}

// NewStore creates a new composite migration store.
func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		Revision: NewRevisionStore(client, root),
		Result:   NewResultStore(client, root),
	}
}
