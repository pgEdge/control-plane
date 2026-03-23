package resource

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

var ErrStateNotFound = errors.New("state not found")

type Service struct {
	store      *Store
	migrations *StateMigrations
	logger     zerolog.Logger
}

func NewService(store *Store, migrations *StateMigrations, loggerFactory *logging.Factory) *Service {
	return &Service{
		store:      store,
		migrations: migrations,
		logger:     loggerFactory.Logger("resource_service"),
	}
}

func (s *Service) Start(ctx context.Context) error {
	all, err := s.store.State.GetAll().Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve states: %w", err)
	}
	// These errors are considered non-fatal so that we don't block startup. We
	// have another check in GetState that will error if the user tries to
	// modify or otherwise use a state that we can't migrate.
	for _, stored := range all {
		logger := s.logger.With().
			Str("database_id", stored.DatabaseID).
			Logger()

		err := stored.State.ValidateVersion()
		if err == nil {
			// This state is already at the target version
			continue
		}
		if errors.Is(err, ErrControlPlaneNeedsUpgrade) {
			logger.Warn().Err(err).Msg("unable to use this resource state")
			continue
		}
		if err := s.migrations.Run(stored.State); err != nil {
			logger.Warn().Err(err).Msg("failed to upgrade resource state")
			continue
		}

		err = s.store.State.Update(stored).Exec(ctx)
		if errors.Is(err, storage.ErrValueVersionMismatch) {
			// This happens when multiple processes try to update the state
			// simultaneously. We expect this under normal conditions.
			logger.Debug().Msg("failed to store upgraded resource state due to contention")
		} else if err != nil {
			logger.Warn().Err(err).Msg("failed to persist upgraded resource state")
		} else {
			logger.Info().
				Stringer("to_version", stored.State.Version).
				Msg("upgraded state")
		}
	}

	return nil
}

func (s *Service) GetState(ctx context.Context, databaseID string) (*State, error) {
	stored, err := s.store.State.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrStateNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve state: %w", err)
	}

	err = stored.State.ValidateVersion()
	if errors.Is(err, ErrStateNeedsUpgrade) {
		if err := s.migrations.Run(stored.State); err != nil {
			return nil, fmt.Errorf("failed to upgrade resource state: %w", err)
		}
		s.logger.Info().
			Str("database_id", databaseID).
			Stringer("to_version", stored.State.Version).
			Msg("upgraded state")
	} else if err != nil {
		return nil, err
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

type StateMigration interface {
	Version() *ds.Version
	Run(state *State) error
}

type StateMigrations struct {
	migrations []StateMigration
}

func NewStateMigrations(migrations []StateMigration) *StateMigrations {
	// Ensure that migrations run in version order
	sorted := slices.SortedFunc(slices.Values(migrations), func(a, b StateMigration) int {
		return a.Version().Compare(b.Version())
	})
	return &StateMigrations{
		migrations: sorted,
	}
}

func (m *StateMigrations) Run(state *State) error {
	for _, migration := range m.migrations {
		version := migration.Version()

		if state.Version.Compare(version) >= 0 {
			// Skip migrations for older or current versions
			continue
		}
		if err := migration.Run(state); err != nil {
			return fmt.Errorf("failed to upgrade database state to version '%s': %w", version, err)
		}
		state.Version = version
	}

	return nil
}

func (m *StateMigrations) Validate() error {
	seen := make(ds.Set[string], len(m.migrations))
	for _, migration := range m.migrations {
		version := migration.Version().String()

		if seen.Has(version) {
			return fmt.Errorf("found duplicate migration version '%s'", version)
		}

		seen.Add(version)
	}

	return nil
}
