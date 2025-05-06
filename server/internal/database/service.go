package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

var ErrDatabaseAlreadyExists = errors.New("database already exists")
var ErrDatabaseNotFound = errors.New("database not found")
var ErrDatabaseNotModifiable = errors.New("database not modifiable")

type Service struct {
	orchestrator Orchestrator
	store        *Store
	hostSvc      *host.Service
}

func NewService(orchestrator Orchestrator, store *Store, hostSvc *host.Service) *Service {
	return &Service{
		orchestrator: orchestrator,
		store:        store,
		hostSvc:      hostSvc,
	}
}

func (s *Service) CreateDatabase(ctx context.Context, spec *Spec) (*Database, error) {
	if spec.DatabaseID == uuid.Nil {
		spec.DatabaseID = uuid.New()
	}
	specExists, err := s.store.Spec.ExistsByKey(spec.DatabaseID).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if database spec exists: %w", err)
	} else if specExists {
		return nil, ErrDatabaseAlreadyExists
	}

	if err := s.populateSpecDefaults(ctx, spec); err != nil {
		return nil, fmt.Errorf("failed to validate database spec: %w", err)
	}

	now := time.Now()
	db := &Database{
		DatabaseID: spec.DatabaseID,
		TenantID:   spec.TenantID,
		CreatedAt:  now,
		UpdatedAt:  now,
		State:      DatabaseStateCreating,
		Spec:       spec,
	}

	if err := s.store.Txn(
		s.store.Spec.Create(&StoredSpec{Spec: spec}),
		s.store.Database.Create(databaseToStored(db)),
	).Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to persist database: %w", err)
	}

	return db, nil
}

func (s *Service) UpdateDatabase(ctx context.Context, spec *Spec) (*Database, error) {
	currentSpec, err := s.store.Spec.GetByKey(spec.DatabaseID).Exec(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrDatabaseNotFound
		}
		return nil, fmt.Errorf("failed to get database spec: %w", err)
	}
	if currentSpec.TenantID != spec.TenantID {
		return nil, fmt.Errorf("tenant ID cannot be changed")
	}
	currentDB, err := s.store.Database.GetByKey(spec.DatabaseID).Exec(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrDatabaseNotFound
		}
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if !DatabaseStateModifiable(currentDB.State) {
		return nil, ErrDatabaseNotModifiable
	}
	if err := s.populateSpecDefaults(ctx, spec); err != nil {
		return nil, fmt.Errorf("failed to validate database spec: %w", err)
	}

	currentSpec.Spec = spec
	currentDB.UpdatedAt = time.Now()
	currentDB.State = DatabaseStateModifying

	if err := s.store.Txn(
		s.store.Spec.Update(currentSpec),
		s.store.Database.Update(currentDB),
	).Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to persist database: %w", err)
	}

	db := storedToDatabase(currentDB, nil)
	db.Spec = spec

	return db, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID uuid.UUID) error {
	var ops []storage.TxnOperation

	spec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("failed to get database spec: %w", err)
	} else if err == nil {
		ops = append(ops, s.store.Spec.Delete(spec))
	}

	db, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("failed to get database: %w", err)
	} else if err == nil {
		ops = append(ops, s.store.Database.Delete(db))
	}

	if len(ops) == 0 {
		return ErrDatabaseNotFound
	}

	if err := s.store.Txn(ops...).Commit(ctx); err != nil {
		return fmt.Errorf("failed to delete database: %w", err)
	}
	return nil
}

func (s *Service) GetDatabase(ctx context.Context, databaseID uuid.UUID) (*Database, error) {
	storedDb, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrDatabaseNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	storedSpec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrDatabaseNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database spec: %w", err)
	}

	return storedToDatabase(storedDb, storedSpec.Spec), nil
}

func (s *Service) GetDatabases(ctx context.Context) ([]*Database, error) {
	storedDbs, err := s.store.Database.GetAll().Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get databases: %w", err)
	}

	databases := make([]*Database, len(storedDbs))
	for i, storedDb := range storedDbs {
		databases[i] = storedToDatabase(storedDb, nil)
	}

	return databases, nil
}

func (s *Service) UpdateDatabaseState(ctx context.Context, databaseID uuid.UUID, state DatabaseState) error {
	storedDb, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrDatabaseNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	storedDb.State = state
	if err := s.store.Database.Update(storedDb).Exec(ctx); err != nil {
		return fmt.Errorf("failed to update database state: %w", err)
	}

	return nil
}

func (s *Service) populateSpecDefaults(ctx context.Context, spec *Spec) error {
	var hostIDs []uuid.UUID
	// First pass to build out hostID list
	for _, node := range spec.Nodes {
		hostIDs = append(hostIDs, node.HostIDs...)
	}
	hosts, err := s.hostSvc.GetHosts(ctx, hostIDs)
	if err != nil {
		return fmt.Errorf("failed to get hosts: %w", err)
	}
	defaultVersion, err := host.GreatestCommonDefaultVersion(hosts...)
	if err != nil {
		return fmt.Errorf("unable to find greatest common default version among specified hosts: %w", err)
	}
	if spec.PostgresVersion == "" {
		spec.PostgresVersion = defaultVersion.PostgresVersion.String()
	}
	if spec.SpockVersion == "" {
		spec.SpockVersion = defaultVersion.SpockVersion.String()
	}
	specVersion, err := host.NewPgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
	if err != nil {
		return fmt.Errorf("failed to parse versions from spec: %w", err)
	}
	// Validate spec version and build up hosts by ID map for node validation
	hostsByID := map[uuid.UUID]*host.Host{}
	for _, h := range hosts {
		hostsByID[h.ID] = h
		if !h.Supports(specVersion) {
			return fmt.Errorf("host %s does not support version combination: postgres=%s, spock=%s", h.ID, specVersion.PostgresVersion, specVersion.SpockVersion)
		}
	}
	// Second pass on nodes to validate node-level overrides
	for idx, node := range spec.Nodes {
		for _, hostID := range node.HostIDs {
			h, ok := hostsByID[hostID]
			if !ok {
				return fmt.Errorf("host %s not found in host list", hostID)
			}
			if node.PostgresVersion != "" {
				nodeVersion, err := host.NewPgEdgeVersion(node.PostgresVersion, spec.SpockVersion)
				if err != nil {
					return fmt.Errorf("failed to parse versions from nodes[%d] spec: %w", idx, err)
				}
				if !h.Supports(nodeVersion) {
					return fmt.Errorf("host %s does not support version combination: postgres=%s, spock=%s", h.ID, nodeVersion.PostgresVersion, nodeVersion.SpockVersion)
				}
			}
		}
	}

	return nil
}

// func (s *Service) GetDatabase(ctx context.Context, databaseID uuid.UUID) (*Database, error) {

// 	storedSpec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get database spec: %w", err)
// 	}

// 	return storedSpec.Spec, nil
// }
