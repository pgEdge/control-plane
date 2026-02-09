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

var (
	ErrDatabaseAlreadyExists   = errors.New("database already exists")
	ErrDatabaseNotFound        = errors.New("database not found")
	ErrDatabaseNotModifiable   = errors.New("database not modifiable")
	ErrInstanceNotFound        = errors.New("instance not found")
	ErrInstanceStopped         = errors.New("instance stopped")
	ErrInvalidDatabaseUpdate   = errors.New("invalid database update")
	ErrInvalidSourceNode       = errors.New("invalid source node")
	ErrServiceInstanceNotFound = errors.New("service instance not found")
)

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
	if spec.DatabaseID == "" {
		spec.DatabaseID = uuid.NewString()
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

	if err := ValidateChangedSpec(currentSpec.Spec, spec); err != nil {
		return nil, err
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

	serviceInstances, err := s.GetServiceInstances(ctx, spec.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instances: %w", err)
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

	db := storedToDatabase(currentDB, currentSpec, instances, serviceInstances)

	return db, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID string) error {
	// Note: This method only deletes the database spec and database state from etcd.
	// Instances and service instances are deleted via their resource lifecycle
	// in the DeleteDatabase workflow (which calls resource.Delete() on each resource).
	// The workflow ensures proper cleanup order:
	// 1. Scale down and remove Docker containers (via resource Delete methods)
	// 2. Delete etcd state (via DeleteInstance/DeleteServiceInstance in resource Delete)
	// 3. Delete database spec and state (this method)

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

func (s *Service) GetDatabase(ctx context.Context, databaseID string) (*Database, error) {
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

	serviceInstances, err := s.GetServiceInstances(ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instances: %w", err)
	}

	return storedToDatabase(storedDb, storedSpec, instances, serviceInstances), nil
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

	serviceInstances, err := s.GetAllServiceInstances(ctx)
	if err != nil {
		return nil, err
	}

	databases := storedToDatabases(storedDbs, storedSpecs, instances, serviceInstances)

	return databases, nil
}

func (s *Service) GetDatabasesByHostId(ctx context.Context, hostID string) ([]*Database, error) {
	allDatabases, err := s.GetDatabases(ctx)
	if err != nil {
		return nil, err
	}

	var result []*Database
	for _, db := range allDatabases {
		for _, instance := range db.Instances {
			if instance.HostID == hostID {
				result = append(result, db)
				break // Found at least one instance on this host
			}
		}
	}
	return result, nil
}

func (s *Service) UpdateDatabaseState(ctx context.Context, databaseID string, from, to DatabaseState) error {
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

func (s *Service) DeleteInstance(ctx context.Context, databaseID, instanceID string) error {
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
	databaseID string,
	instanceID string,
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

func (s *Service) GetStoredInstanceState(ctx context.Context, databaseID, instanceID string) (InstanceState, error) {
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

func (s *Service) GetInstances(ctx context.Context, databaseID string) ([]*Instance, error) {
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

func (s *Service) GetInstance(ctx context.Context, databaseID, instanceID string) (*Instance, error) {
	storedInstance, err := s.store.Instance.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrInstanceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get stored instance: %w", err)
	}
	storedStatus, err := s.store.InstanceStatus.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrInstanceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get stored instance status: %w", err)
	}

	instance := storedToInstance(storedInstance, storedStatus)

	return instance, nil
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

func (s *Service) GetAllServiceInstances(ctx context.Context) ([]*ServiceInstance, error) {
	storedServiceInstances, err := s.store.ServiceInstance.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored service instances: %w", err)
	}
	storedStatuses, err := s.store.ServiceInstanceStatus.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored service instance statuses: %w", err)
	}

	serviceInstances := storedToServiceInstances(storedServiceInstances, storedStatuses)

	return serviceInstances, nil
}

func (s *Service) InstanceCountForHost(ctx context.Context, hostID string) (int, error) {
	storedInstances, err := s.store.Instance.
		GetAll().
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get stored instances: %w", err)
	}
	var count int
	for _, instance := range storedInstances {
		if instance.HostID == hostID {
			count++
		}
	}
	return count, nil
}

func (s *Service) PopulateSpecDefaults(ctx context.Context, spec *Spec) error {
	var hostIDs []string
	// First pass to build out hostID list
	for _, node := range spec.Nodes {
		hostIDs = append(hostIDs, node.HostIDs...)
	}
	for _, svc := range spec.Services {
		hostIDs = append(hostIDs, svc.HostIDs...)
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
	hostsByID := map[string]*host.Host{}
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

	// Validate that all service host IDs refer to registered hosts
	for _, svc := range spec.Services {
		for _, hostID := range svc.HostIDs {
			if _, ok := hostsByID[hostID]; !ok {
				return fmt.Errorf("service %q: host %s not found", svc.ServiceID, hostID)
			}
		}
	}

	return nil
}

func ValidateChangedSpec(current, updated *Spec) error {
	var errs []error

	// Immutable: tenant_id must not change
	if !tenantIDsMatch(current.TenantID, updated.TenantID) {
		errs = append(errs, errors.New("tenant ID cannot be changed"))
	}

	// Immutable: database_name must not change
	if current.DatabaseName != updated.DatabaseName {
		errs = append(errs, errors.New("database name cannot be changed"))
	}

	currentInstances, err := instancesByID(current)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to compute instances from current spec: %w", err))
		return fmt.Errorf("%w: %s", ErrInvalidDatabaseUpdate, errors.Join(errs...))
	}
	updatedInstances, err := instancesByID(updated)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to compute instances from updated spec: %w", err))
		return fmt.Errorf("%w: %s", ErrInvalidDatabaseUpdate, errors.Join(errs...))
	}

	for id, newInstance := range updatedInstances {
		oldInstance, ok := currentInstances[id]
		if !ok {
			// We only care about instances that have changed here. New and
			// removed instances don't need to be checked.
			continue
		}
		err := majorVersionChanged(oldInstance.PgEdgeVersion, newInstance.PgEdgeVersion)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid change for instance %s: %w", id, err))
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("%w: %s", ErrInvalidDatabaseUpdate, errors.Join(errs...))
	}

	return nil
}

func instancesByID(spec *Spec) (map[string]*InstanceSpec, error) {
	nodes, err := spec.NodeInstances()
	if err != nil {
		return nil, err
	}
	byID := map[string]*InstanceSpec{}
	for _, node := range nodes {
		for _, instance := range node.Instances {
			byID[instance.InstanceID] = instance
		}
	}
	return byID, nil
}

func majorVersionChanged(old, new *host.PgEdgeVersion) error {
	if old == nil || new == nil {
		return errors.New("expected both current and updated versions to be defined")
	}
	oldPgMajor, ok := old.PostgresVersion.Major()
	if !ok {
		return errors.New("current postgres version is missing its major component")
	}
	newPgMajor, ok := new.PostgresVersion.Major()
	if !ok {
		return errors.New("updated postgres version is missing its major component")
	}
	if oldPgMajor != newPgMajor {
		return fmt.Errorf("major version changed from %d to %d", oldPgMajor, newPgMajor)
	}
	return nil
}

func tenantIDsMatch(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a != nil && b != nil:
		return *a == *b
	default:
		return false
	}
}

// Service Instance Management Methods

func (s *Service) UpdateServiceInstance(ctx context.Context, opts *ServiceInstanceUpdateOptions) error {
	serviceInstance, err := s.store.ServiceInstance.
		GetByKey(opts.DatabaseID, opts.ServiceInstanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		serviceInstance = NewStoredServiceInstance(opts)
	} else if err != nil {
		return fmt.Errorf("failed to get stored service instance: %w", err)
	} else {
		serviceInstance.Update(opts)
	}

	err = s.store.ServiceInstance.
		Put(serviceInstance).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update stored service instance: %w", err)
	}

	return nil
}

// SetServiceInstanceState performs a targeted state update using a direct
// key lookup instead of scanning all service instances.
func (s *Service) SetServiceInstanceState(
	ctx context.Context,
	databaseID, serviceInstanceID string,
	state ServiceInstanceState,
) error {
	stored, err := s.store.ServiceInstance.
		GetByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get service instance: %w", err)
	}
	stored.State = state
	stored.Error = ""
	stored.UpdatedAt = time.Now()
	return s.store.ServiceInstance.Put(stored).Exec(ctx)
}

func (s *Service) DeleteServiceInstance(ctx context.Context, databaseID, serviceInstanceID string) error {
	_, err := s.store.ServiceInstance.
		DeleteByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete stored service instance: %w", err)
	}
	_, err = s.store.ServiceInstanceStatus.
		DeleteByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete stored service instance status: %w", err)
	}

	return nil
}

func (s *Service) UpdateServiceInstanceStatus(
	ctx context.Context,
	databaseID string,
	serviceInstanceID string,
	status *ServiceInstanceStatus,
) error {
	stored := &StoredServiceInstanceStatus{
		DatabaseID:        databaseID,
		ServiceInstanceID: serviceInstanceID,
		Status:            status,
	}
	err := s.store.ServiceInstanceStatus.
		Put(stored).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update stored service instance status: %w", err)
	}

	return nil
}

func (s *Service) GetServiceInstances(ctx context.Context, databaseID string) ([]*ServiceInstance, error) {
	storedServiceInstances, err := s.store.ServiceInstance.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored service instances: %w", err)
	}
	storedStatuses, err := s.store.ServiceInstanceStatus.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stored service instance statuses: %w", err)
	}

	serviceInstances := storedToServiceInstances(storedServiceInstances, storedStatuses)

	return serviceInstances, nil
}

func (s *Service) GetServiceInstance(ctx context.Context, databaseID, serviceInstanceID string) (*ServiceInstance, error) {
	storedServiceInstance, err := s.store.ServiceInstance.
		GetByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrServiceInstanceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get stored service instance: %w", err)
	}

	storedStatus, err := s.store.ServiceInstanceStatus.
		GetByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("failed to get stored service instance status: %w", err)
	}

	serviceInstance := storedToServiceInstance(storedServiceInstance, storedStatus)

	return serviceInstance, nil
}

type ServiceInstanceStateUpdate struct {
	DatabaseID string                 `json:"database_id,omitempty"`
	State      ServiceInstanceState   `json:"state"`
	Status     *ServiceInstanceStatus `json:"status,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

func (s *Service) UpdateServiceInstanceState(
	ctx context.Context,
	serviceInstanceID string,
	update *ServiceInstanceStateUpdate,
) error {
	var databaseID string
	var serviceID string
	var hostID string

	if update.DatabaseID != "" {
		// Use targeted lookup when DatabaseID is provided
		stored, err := s.store.ServiceInstance.
			GetByKey(update.DatabaseID, serviceInstanceID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to get service instance: %w", err)
		}
		databaseID = stored.DatabaseID
		serviceID = stored.ServiceID
		hostID = stored.HostID
	} else {
		// Fall back to full scan when DatabaseID is not provided
		serviceInstances, err := s.GetAllServiceInstances(ctx)
		if err != nil {
			return fmt.Errorf("failed to get service instances: %w", err)
		}

		for _, si := range serviceInstances {
			if si.ServiceInstanceID == serviceInstanceID {
				databaseID = si.DatabaseID
				serviceID = si.ServiceID
				hostID = si.HostID
				break
			}
		}
		if databaseID == "" {
			return fmt.Errorf("service instance %s not found", serviceInstanceID)
		}
	}

	// Update the service instance state
	err := s.UpdateServiceInstance(ctx, &ServiceInstanceUpdateOptions{
		ServiceInstanceID: serviceInstanceID,
		ServiceID:         serviceID,
		DatabaseID:        databaseID,
		HostID:            hostID,
		State:             update.State,
		Error:             update.Error,
	})
	if err != nil {
		return fmt.Errorf("failed to update service instance: %w", err)
	}

	// Update the service instance status if provided
	if update.Status != nil {
		err = s.UpdateServiceInstanceStatus(ctx, databaseID, serviceInstanceID, update.Status)
		if err != nil {
			return fmt.Errorf("failed to update service instance status: %w", err)
		}
	}

	return nil
}
