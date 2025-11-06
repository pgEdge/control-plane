package resource

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

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
	stored, err := s.store.State.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrStateNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve state: %w", err)
	}
	return stored.State, nil
}

func (s *Service) PersistState(ctx context.Context, databaseID string, state *State) error {
	err := s.store.State.Put(&StoredState{
		DatabaseID: databaseID,
		State:      state,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store state: %w", err)
	}
	return nil
}

func (s *Service) PersistPlanSummaries(ctx context.Context, databaseID string, taskID uuid.UUID, plans []PlanSummary) error {
	err := s.store.PlanSummaries.Put(&StoredPlanSummaries{
		DatabaseID: databaseID,
		TaskID:     taskID,
		Plans:      plans,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store plans: %w", err)
	}
	return nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID string) error {
	_, err := s.store.State.DeleteByKey(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete state: %w", err)
	}
	_, err = s.store.PlanSummaries.DeleteByDatabaseID(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete plans: %w", err)
	}
	return nil
}
