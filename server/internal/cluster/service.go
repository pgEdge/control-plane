package cluster

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService( /*cfg config.Config,*/ store *Store) *Service {
	return &Service{
		store: store,
	}
}

func (s *Service) Create(ctx context.Context, clusterID string) error {
	if err := s.store.Cluster.Create(&StoredCluster{ID: clusterID}).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store cluster ID: %w", err)
	}
	return nil
}

func (s *Service) Get(ctx context.Context) (*StoredCluster, error) {
	return s.store.Cluster.Get().Exec(ctx)
}
