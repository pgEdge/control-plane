package resource

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

var ErrStateNotFound = errors.New("state not found")

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{
		store: store,
	}
}

func (s *Service) GetState(ctx context.Context, databaseID string) (*State, error) {
	stored, err := s.store.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrStateNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve state: %w", err)
	}
	return stored.State, nil
}

func (s *Service) PersistState(ctx context.Context, databaseID string, state *State) error {
	err := s.store.Put(&StoredState{
		DatabaseID: databaseID,
		State:      state,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store state: %w", err)
	}
	return nil
}

func (s *Service) DeleteState(ctx context.Context, databaseID string) error {
	_, err := s.store.DeleteByKey(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete state: %w", err)
	}
	return nil
}
