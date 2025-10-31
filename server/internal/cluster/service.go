package cluster

import (
	"context"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{
		store: store,
	}
}

func (s *Service) Get(ctx context.Context) (*StoredCluster, error) {
	return s.store.Cluster.Get().Exec(ctx)
}
