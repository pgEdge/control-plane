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
var ErrInstanceNotFound = errors.New("instance not found")

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

func (s *Service) UpdateDatabase(ctx context.Context, state DatabaseState, spec *Spec) (*Database, error) {
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

	instances, err := s.GetInstances(ctx, spec.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get database instances: %w", err)
	}

	currentSpec.Spec = spec
	currentDB.UpdatedAt = time.Now()
	currentDB.State = state

	if err := s.store.Txn(
		s.store.Spec.Update(currentSpec),
		s.store.Database.Update(currentDB),
	).Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to persist database: %w", err)
	}

	db := storedToDatabase(currentDB, currentSpec, instances)

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

	instances, err := s.GetInstances(ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get database instances: %w", err)
	}

	return storedToDatabase(storedDb, storedSpec, instances), nil
}

func (s *Service) GetDatabases(ctx context.Context) ([]*Database, error) {
	storedDbs, err := s.store.Database.GetAll().Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get databases: %w", err)
	}

	storedSpecs, err := s.store.Spec.GetAll().Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database specs: %w", err)
	}

	instances, err := s.GetAllInstances(ctx)
	if err != nil {
		return nil, err
	}

	databases := storedToDatabases(storedDbs, storedSpecs, instances)

	return databases, nil
}

func (s *Service) UpdateDatabaseState(ctx context.Context, databaseID uuid.UUID, from, to DatabaseState) error {
	storedDb, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrDatabaseNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	// from is an optional guard to ensure that the state is only updated if the
	// database is in the expected state
	if from != "" && storedDb.State != from {
		return fmt.Errorf("database state is not in expected state %s, but %s", from, storedDb.State)
	}

	storedDb.State = to
	if err := s.store.Database.Update(storedDb).Exec(ctx); err != nil {
		return fmt.Errorf("failed to update database state: %w", err)
	}

	return nil
}

func (s *Service) UpdateInstance(ctx context.Context, opts *InstanceUpdateOptions) error {
	instance, err := s.store.Instance.
		GetByKey(opts.DatabaseID, opts.InstanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		instance = NewStoredInstance(opts)
	} else if err != nil {
		return fmt.Errorf("failed to get stored instance: %w", err)
	} else {
		instance.Update(opts)
	}

	err = s.store.Instance.
		Put(instance).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update stored instance: %w", err)
	}

	return nil
}

func (s *Service) DeleteInstance(ctx context.Context, databaseID, instanceID uuid.UUID) error {
	_, err := s.store.Instance.
		DeleteByKey(databaseID, instanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete stored instance: %w", err)
	}
	_, err = s.store.InstanceStatus.
		DeleteByKey(databaseID, instanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete stored instance status: %w", err)
	}

	return nil
}

func (s *Service) UpdateInstanceStatus(
	ctx context.Context,
	databaseID uuid.UUID,
	instanceID uuid.UUID,
	status *InstanceStatus,
) error {
	stored := &StoredInstanceStatus{
		DatabaseID: databaseID,
		InstanceID: instanceID,
		Status:     status,
	}
	err := s.store.InstanceStatus.
		Put(stored).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update stored instance status: %w", err)
	}

	return nil
}

func (s *Service) GetStoredInstanceState(ctx context.Context, databaseID, instanceID uuid.UUID) (InstanceState, error) {
	storedInstance, err := s.store.Instance.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return InstanceStateUnknown, ErrInstanceNotFound
	} else if err != nil {
		return InstanceStateUnknown, fmt.Errorf("failed to get stored instance: %w", err)
	}

	return storedInstance.State, nil
}

func (s *Service) GetInstances(ctx context.Context, databaseID uuid.UUID) ([]*Instance, error) {
	storedInstances, err := s.store.Instance.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored instances: %w", err)
	}
	storedStatuses, err := s.store.InstanceStatus.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored instance statuses: %w", err)
	}

	instances := storedToInstances(storedInstances, storedStatuses)

	return instances, nil
}

func (s *Service) GetAllInstances(ctx context.Context) ([]*Instance, error) {
	storedInstances, err := s.store.Instance.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored instances: %w", err)
	}
	storedStatuses, err := s.store.InstanceStatus.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored instance statuses: %w", err)
	}

	instances := storedToInstances(storedInstances, storedStatuses)

	return instances, nil
}

func (s *Service) PopulateSpecDefaults(ctx context.Context, spec *Spec) error {
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
