package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/ports"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
	cfg          config.Config
	orchestrator Orchestrator
	store        *Store
	hostSvc      *host.Service
	portsSvc     *ports.Service
}

func NewService(
	cfg config.Config,
	orchestrator Orchestrator,
	store *Store,
	hostSvc *host.Service,
	portsSvc *ports.Service,
) *Service {
	return &Service{
		cfg:          cfg,
		orchestrator: orchestrator,
		store:        store,
		hostSvc:      hostSvc,
		portsSvc:     portsSvc,
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
		NotCreated: true,
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
	specs, err := s.store.InstanceSpec.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instance specs: %w", err)
	}
	for _, spec := range specs {
		if err := s.releaseInstancePorts(ctx, spec.Spec); err != nil {
			return err
		}
	}

	serviceInstanceSpecs, err := s.store.ServiceInstanceSpec.
		GetByDatabaseID(databaseID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get service instance specs: %w", err)
	}
	for _, spec := range serviceInstanceSpecs {
		if err := s.releaseServiceInstancePort(ctx, spec.Spec); err != nil {
			return err
		}
	}

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

	ops = append(ops,
		s.store.Instance.DeleteByDatabaseID(databaseID),
		s.store.InstanceSpec.DeleteByDatabaseID(databaseID),
		s.store.InstanceStatus.DeleteByDatabaseID(databaseID),
		s.store.ScriptResult.DeleteByDatabaseID(databaseID),
		s.store.ServiceInstanceSpec.DeleteByDatabaseID(databaseID),
	)

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
	// creation is considered complete the first time we set the database to
	// available.
	if to == DatabaseStateAvailable && storedDb.NotCreated {
		storedDb.NotCreated = false
	}

	storedDb.State = to
	if err := s.store.Database.Update(storedDb).Exec(ctx); err != nil {
		return fmt.Errorf("failed to update database state: %w", err)
	}

	return nil
}

func (s *Service) GetScriptResult(ctx context.Context, databaseID string, scriptName ScriptName, nodeName string) (*ScriptResult, error) {
	stored, err := s.store.ScriptResult.
		GetByKey(databaseID, scriptName, nodeName).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return NewScriptResult(databaseID, scriptName, nodeName), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get script result: %w", err)
	}

	return stored.Result, nil
}

func (s *Service) UpdateScriptResult(ctx context.Context, result *ScriptResult) error {
	if err := result.Validate(); err != nil {
		return fmt.Errorf("invalid script result: %w", err)
	}

	stored, err := s.store.ScriptResult.
		GetByKey(result.DatabaseID, result.ScriptName, result.NodeName).
		Exec(ctx)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		stored = &StoredScriptResult{Result: result}
	case err != nil:
		return fmt.Errorf("failed to get script result: %w", err)
	case stored.Result.Succeeded:
		// Avoid overwriting a successful result in the off-chance of
		// overlapping operations.
		return errors.New("script already marked as succeeded")
	default:
		stored.Result = result
	}

	if err := s.store.ScriptResult.Update(stored).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store script result: %w", err)
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

func (s *Service) UpdateInstanceState(ctx context.Context, opts *InstanceStateUpdateOptions) error {
	instance, err := s.store.Instance.
		GetByKey(opts.DatabaseID, opts.InstanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		instance = NewStoredInstance(&InstanceUpdateOptions{
			InstanceID: opts.InstanceID,
			DatabaseID: opts.DatabaseID,
			HostID:     opts.HostID,
			NodeName:   opts.NodeName,
			State:      opts.State,
		})
	} else if err != nil {
		return fmt.Errorf("failed to get stored instance: %w", err)
	} else {
		instance.UpdateState(opts)
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

	return s.DeleteInstanceSpec(ctx, databaseID, instanceID)
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

func (s *Service) GetStoredDatabaseState(ctx context.Context, databaseID string) (DatabaseState, error) {
	stored, err := s.store.Database.
		GetByKey(databaseID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return DatabaseStateUnknown, ErrDatabaseNotFound
	} else if err != nil {
		return DatabaseStateUnknown, fmt.Errorf("failed to get stored database: %w", err)
	}

	return stored.State, nil
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
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
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

func (s *Service) CreatePgBackRestBackup(ctx context.Context, w io.Writer, databaseID, instanceID string, options *pgbackrest.BackupOptions) error {
	instance, err := s.store.InstanceSpec.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrInstanceNotFound
	} else if err != nil {
		return err
	}
	return s.orchestrator.CreatePgBackRestBackup(ctx, w, instance.Spec, options)
}

func (s *Service) GetInstanceConnectionInfo(ctx context.Context, databaseID, instanceID string) (*ConnectionInfo, error) {
	storedInstance, err := s.store.Instance.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrInstanceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get stored instance: %w", err)
	}
	return s.orchestrator.GetInstanceConnectionInfo(ctx,
		storedInstance.DatabaseID, storedInstance.InstanceID,
		storedInstance.Port, storedInstance.PatroniPort,
		storedInstance.PgEdgeVersion)
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

func validateHostIDs(hostIDs ds.Set[string], hosts []*host.Host) error {
	found := ds.NewSet[string]()
	for _, host := range hosts {
		found.Add(host.ID)
	}
	notFound := hostIDs.Difference(found).ToSortedSlice(strings.Compare)
	if len(notFound) != 0 {
		return fmt.Errorf("got invalid host ids: %s", strings.Join(notFound, ", "))
	}

	return nil
}

func (s *Service) PopulateSpecDefaults(ctx context.Context, spec *Spec) error {
	hostIDs := ds.NewSet[string]()
	// First pass to build out hostID list
	for _, node := range spec.Nodes {
		hostIDs.Add(node.HostIDs...)
	}
	for _, svc := range spec.Services {
		hostIDs.Add(svc.HostIDs...)
	}
	hosts, err := s.hostSvc.GetHosts(ctx, hostIDs.ToSlice())
	if err != nil {
		return fmt.Errorf("failed to get hosts: %w", err)
	}
	if err := validateHostIDs(hostIDs, hosts); err != nil {
		return err
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
	specVersion, err := ds.NewPgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
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
				nodeVersion, err := ds.NewPgEdgeVersion(node.PostgresVersion, spec.SpockVersion)
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

func (s *Service) ReconcileInstanceSpec(ctx context.Context, spec *InstanceSpec) (*InstanceSpec, error) {
	if s.cfg.HostID != spec.HostID {
		return nil, fmt.Errorf("this instance belongs to another host - this host='%s', instance host='%s'", s.cfg.HostID, spec.HostID)
	}

	var previous *InstanceSpec
	stored, err := s.store.InstanceSpec.
		GetByKey(spec.DatabaseID, spec.InstanceID).
		Exec(ctx)
	switch {
	case err == nil:
		previous = stored.Spec
		spec.CopySettingsFrom(previous)
	case errors.Is(err, storage.ErrNotFound):
		stored = &StoredInstanceSpec{}
	default:
		return nil, fmt.Errorf("failed to get current spec for instance '%s': %w", spec.InstanceID, err)
	}

	var allocated []int
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		return errors.Join(cause, s.portsSvc.ReleasePort(rollbackCtx, spec.HostID, allocated...))
	}

	if spec.Port != nil && *spec.Port == 0 {
		port, err := s.portsSvc.AllocatePort(ctx, spec.HostID)
		if err != nil {
			return nil, fmt.Errorf("failed to allocate port: %w", err)
		}
		allocated = append(allocated, port)
		spec.Port = utils.PointerTo(port)
	}

	if spec.PatroniPort != nil && *spec.PatroniPort == 0 {
		port, err := s.portsSvc.AllocatePort(ctx, spec.HostID)
		if err != nil {
			return nil, rollback(fmt.Errorf("failed to allocate patroni port: %w", err))
		}
		allocated = append(allocated, port)
		spec.PatroniPort = utils.PointerTo(port)
	}

	stored.Spec = spec
	err = s.store.InstanceSpec.
		Update(stored).
		Exec(ctx)
	if err != nil {
		return nil, rollback(fmt.Errorf("failed to persist updated instance spec: %w", err))
	}

	if err := s.releasePreviousSpecPorts(ctx, previous, spec); err != nil {
		return nil, err
	}

	return spec, nil
}

func (s *Service) DeleteInstanceSpec(ctx context.Context, databaseID, instanceID string) error {
	spec, err := s.store.InstanceSpec.
		GetByKey(databaseID, instanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		// Spec has already been deleted
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check if instance spec exists: %w", err)
	}

	if err := s.releaseInstancePorts(ctx, spec.Spec); err != nil {
		return err
	}

	_, err = s.store.InstanceSpec.
		DeleteByKey(databaseID, instanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete instance spec: %w", err)
	}

	return nil
}

func (s *Service) ReconcileServiceInstanceSpec(ctx context.Context, spec *ServiceInstanceSpec) (*ServiceInstanceSpec, error) {
	if s.cfg.HostID != spec.HostID {
		return nil, fmt.Errorf("this service instance belongs to another host - this host='%s', service instance host='%s'", s.cfg.HostID, spec.HostID)
	}

	var previous *ServiceInstanceSpec
	stored, err := s.store.ServiceInstanceSpec.
		GetByKey(spec.DatabaseID, spec.ServiceInstanceID).
		Exec(ctx)
	switch {
	case err == nil:
		previous = stored.Spec
		spec.CopyPortFrom(previous)
	case errors.Is(err, storage.ErrNotFound):
		stored = &StoredServiceInstanceSpec{}
	default:
		return nil, fmt.Errorf("failed to get current spec for service instance '%s': %w", spec.ServiceInstanceID, err)
	}

	var allocated []int
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		return errors.Join(cause, s.portsSvc.ReleasePort(rollbackCtx, spec.HostID, allocated...))
	}

	if spec.Port != nil && *spec.Port == 0 {
		port, err := s.portsSvc.AllocatePort(ctx, spec.HostID)
		if err != nil {
			return nil, fmt.Errorf("failed to allocate port for service instance: %w", err)
		}
		allocated = append(allocated, port)
		spec.Port = utils.PointerTo(port)
	}

	stored.Spec = spec
	err = s.store.ServiceInstanceSpec.
		Update(stored).
		Exec(ctx)
	if err != nil {
		return nil, rollback(fmt.Errorf("failed to persist updated service instance spec: %w", err))
	}

	if err := s.releasePreviousServiceInstancePort(ctx, previous, spec); err != nil {
		return nil, err
	}

	return spec, nil
}

func (s *Service) DeleteServiceInstanceSpec(ctx context.Context, databaseID, serviceInstanceID string) error {
	spec, err := s.store.ServiceInstanceSpec.
		GetByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check if service instance spec exists: %w", err)
	}

	if err := s.releaseServiceInstancePort(ctx, spec.Spec); err != nil {
		return err
	}

	_, err = s.store.ServiceInstanceSpec.
		DeleteByKey(databaseID, serviceInstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete service instance spec: %w", err)
	}

	return nil
}

func (s *Service) releaseServiceInstancePort(ctx context.Context, spec *ServiceInstanceSpec) error {
	err := s.portsSvc.ReleasePortIfDefined(ctx, spec.HostID, spec.Port)
	if err != nil {
		return fmt.Errorf("failed to release port for service instance '%s': %w", spec.ServiceInstanceID, err)
	}

	return nil
}

func (s *Service) releasePreviousServiceInstancePort(ctx context.Context, previous, new *ServiceInstanceSpec) error {
	if previous == nil {
		return nil
	}
	if portShouldBeReleased(previous.Port, new.Port) {
		err := s.portsSvc.ReleasePortIfDefined(ctx, previous.HostID, previous.Port)
		if err != nil {
			return fmt.Errorf("failed to release previous service instance port: %w", err)
		}
	}
	return nil
}

func (s *Service) releaseInstancePorts(ctx context.Context, spec *InstanceSpec) error {
	err := s.portsSvc.ReleasePortIfDefined(ctx, spec.HostID, spec.Port, spec.PatroniPort)
	if err != nil {
		return fmt.Errorf("failed to release ports for instance '%s': %w", spec.InstanceID, err)
	}

	return nil
}

func (s *Service) releasePreviousSpecPorts(ctx context.Context, previous, new *InstanceSpec) error {
	if previous == nil {
		return nil
	}
	if portShouldBeReleased(previous.Port, new.Port) {
		err := s.portsSvc.ReleasePortIfDefined(ctx, previous.HostID, previous.Port)
		if err != nil {
			return fmt.Errorf("failed to release previous port: %w", err)
		}
	}
	if portShouldBeReleased(previous.PatroniPort, new.PatroniPort) {
		err := s.portsSvc.ReleasePortIfDefined(ctx, previous.HostID, previous.PatroniPort)
		if err != nil {
			return fmt.Errorf("failed to release previous patroni port: %w", err)
		}
	}
	return nil
}

func portShouldBeReleased(current *int, new *int) bool {
	if current == nil || *current == 0 {
		// we didn't previously have an assigned port
		return false
	}
	if new == nil || *current != *new {
		// we had a previously assigned port and now the port is either nil or
		// different
		return true
	}

	// the current and new ports are equal, so it should not be released.
	return false
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

func majorVersionChanged(old, new *ds.PgEdgeVersion) error {
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
		if errors.Is(err, storage.ErrNotFound) {
			return ErrServiceInstanceNotFound
		}
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

	return s.DeleteServiceInstanceSpec(ctx, databaseID, serviceInstanceID)
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
			if errors.Is(err, storage.ErrNotFound) {
				return ErrServiceInstanceNotFound
			}
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
			return ErrServiceInstanceNotFound
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
