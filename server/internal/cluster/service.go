package cluster

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Service struct {
	//cfg   config.Config
	store *Store
}

func NewService( /*cfg config.Config,*/ store *Store) *Service {
	fmt.Printf(">>>>> in cluster.service.NewService\n")

	return &Service{
		//cfg:   cfg,
		store: store,
	}
}

func (s *Service) Create(ctx context.Context, clusterID string) error {
	fmt.Printf(">>>>> in cluster.service.Create\n")
	if s == nil {
		return fmt.Errorf("service is nil")
	}
	if s.store == nil {
		return fmt.Errorf("store is nil")
	}
	if s.store.Cluster == nil {
		return fmt.Errorf("cluster store is nil")
	}
	if err := s.store.Cluster.Create(&StoredCluster{
		StoredValue: storage.StoredValue{}, // TODO: set explicitly?
		ID:          clusterID,
	}).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store cluster ID: %w", err)
	}
	return nil
}
