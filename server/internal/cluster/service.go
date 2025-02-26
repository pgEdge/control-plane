package cluster

import (
	"context"
	"fmt"
	"time"
)

type Service interface {
	IsInitialized() (bool, error)
	Initialize() error
	Delete() error
}

type service struct {
	store *Store
}

func (s *service) IsInitialized(ctx context.Context) (bool, error) {
	exists, err := s.store.Exists().Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if cluster exists: %w", err)
	}
	return exists, nil
}

func (s *service) Initialize(ctx context.Context) error {
	err := s.store.Create(&StoredCluster{
		CreatedAt: time.Now(),
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}
	return nil
}

func (s *service) Delete(ctx context.Context) error {
	_, err := s.store.Delete().Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}
	return nil
}
