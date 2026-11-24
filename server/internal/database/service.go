package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/ports"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/validation"
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
	logger       zerolog.Logger
}

func NewService(
	cfg config.Config,
	orchestrator Orchestrator,
	store *Store,
	hostSvc *host.Service,
	portsSvc *ports.Service,
	loggerFactory *logging.Factory,
) *Service {
	return &Service{
		cfg:          cfg,
		orchestrator: orchestrator,
		store:        store,
		hostSvc:      hostSvc,
		portsSvc:     portsSvc,
		logger:       loggerFactory.Logger(logging.ComponentDatabaseService),
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
		RawState:   DatabaseStateCreating,
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
	if err := s.validateSpockMajorUpgrade(ctx, currentSpec.Spec, spec); err != nil {
		return nil, err
	}

	return s.applySpecUpdate(ctx, currentSpec, currentDB, spec, state)
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

// PopulateSpecDefaults fills in the spec's omitted versions and validates the
// result against the specified hosts. current is the database's stored spec,
// or nil if the database is being created.
func (s *Service) PopulateSpecDefaults(ctx context.Context, spec, current *Spec) error {
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
	spec.DefaultVersions(defaultVersion, current)
	specVersion, err := ds.ParsePgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
	if err != nil {
		return fmt.Errorf("failed to parse versions from spec: %w", err)
	}
	// Validate spec version and build up hosts by ID map for node validation
	hostsByID := map[string]*host.Host{}
	for _, h := range hosts {
		hostsByID[h.ID] = h
		if !h.Supports(specVersion) {
			return fmt.Errorf("host %s does not support version combination: %s", h.ID, describeVersion(specVersion))
		}
	}
	// Second pass on nodes to validate node-level overrides
	for idx, node := range spec.Nodes {
		// A node-level image override similarly implies its own Postgres
		// version if one isn't otherwise specified for the node.
		if node.PostgresVersion == "" {
			if pgVer, _, ok := versionsFromImage(node.OrchestratorOpts.swarmImage()); ok {
				node.PostgresVersion = pgVer
			}
		}
		nodeVersion := specVersion
		if node.PostgresVersion != "" {
			nodeVersion, err = ds.ParsePgEdgeVersion(node.PostgresVersion, spec.SpockVersion)
			if err != nil {
				return fmt.Errorf("failed to parse versions from nodes[%d] spec: %w", idx, err)
			}
		}
		var hostErrs []error
		for _, hostID := range node.HostIDs {
			h, ok := hostsByID[hostID]
			if !ok {
				hostErrs = append(hostErrs, fmt.Errorf("host %s not found in host list", hostID))
				continue
			}
			hostVersion := h.SupportedVersion(nodeVersion)
			if hostVersion == nil {
				hostErrs = append(hostErrs, fmt.Errorf("host %s does not support version combination: %s", h.ID, describeVersion(nodeVersion)))
				continue
			}
			if h.Orchestrator == config.OrchestratorSystemD {
				// Systemd hosts can have any set of extensions installed, so
				// we validate that the preloaded libraries are available.
				if err := validatePreloadedLibraries(spec, idx, h.ID, hostVersion); err != nil {
					hostErrs = append(hostErrs, err)
				}
			}
		}
		if err := errors.Join(hostErrs...); err != nil {
			return err
		}
	}

	// This must run after the version defaults above, since an omitted
	// version can resolve to one that doesn't support the locale provider.
	if err := validateLocaleVersions(spec); err != nil {
		return err
	}

	// This must also run after the version defaults above, since an omitted
	// version can resolve to one with or without Spock.
	if err := validateSpockRequirements(spec, specVersion, hostsByID); err != nil {
		return err
	}

	if err := validateServiceHosts(spec.Services, hostsByID); err != nil {
		return err
	}

	return nil
}

// validateServiceHosts validates that every service's host IDs refer to
// registered hosts that can run the requested service version.
func validateServiceHosts(services []*ServiceSpec, hostsByID map[string]*host.Host) error {
	for _, svc := range services {
		var knownHosts []*host.Host
		for _, hostID := range svc.HostIDs {
			h, ok := hostsByID[hostID]
			if !ok {
				return fmt.Errorf("service %q: host %s not found", svc.ServiceID, hostID)
			}
			if h.Orchestrator == config.OrchestratorSystemD && svc.Port == nil {
				// systemd service instances listen on the host directly, so
				// they're unreachable without a port.
				return fmt.Errorf("service %q: port is required for host %s, which uses the systemd orchestrator. set it to 0 to assign a random port", svc.ServiceID, h.ID)
			}
			if h.SupportedServices == nil {
				// This host's record was written by a Control Plane version
				// that didn't report its supported services, so we can't
				// validate against it.
				continue
			}
			if h.Orchestrator != config.OrchestratorSystemD && svc.swarmImageOverride() != "" {
				// A user-specified image bypasses the version manifest.
				continue
			}
			if !h.SupportsService(svc.ServiceType, svc.Version) {
				return fmt.Errorf("service %q: host %s does not support %s version %s", svc.ServiceID, h.ID, svc.ServiceType, svc.Version)
			}
			knownHosts = append(knownHosts, h)
		}
		if svc.Version == host.ServiceVersionLatest && len(knownHosts) > 0 {
			// "latest" is stored as-is so that it continues to follow the
			// hosts' defaults, but each host resolves it to its own default,
			// so they must agree for every instance to run the same version.
			if _, err := host.CommonDefaultServiceVersion(svc.ServiceType, knownHosts...); err != nil {
				return fmt.Errorf("service %q: unable to resolve version %s: %w", svc.ServiceID, svc.Version, err)
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
		if err := s.orchestrator.ReconcileInstanceSpec(previous, spec); err != nil {
			return nil, fmt.Errorf("failed to reconcile instance spec: %w", err)
		}
	case errors.Is(err, storage.ErrNotFound):
		if err := s.orchestrator.ReconcileInstanceSpec(nil, spec); err != nil {
			return nil, fmt.Errorf("failed to reconcile instance spec: %w", err)
		}
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

// AvailableUpgrades returns newer stable manifest entries in the same
// (postgres_major, spock_major) bucket as the given version.
func (s *Service) AvailableUpgrades(current *ds.PgEdgeVersion) []*AvailableUpgrade {
	return s.orchestrator.AvailableUpgrades(current)
}

// ApplyUpgradeResult is returned by ApplyUpgrade. It carries the information
// needed to roll back the upgrade if the workflow fails to start.
type ApplyUpgradeResult struct {
	Database  *Database
	PrevState DatabaseState
	prevSpec  *Spec
}

// ApplyUpgrade validates targetImage against the manifest, updates the
// database spec's PostgresVersion to the new version, and sets the database
// state to modifying. The workflow is responsible for updating instance specs
// and pinning ResolvedImage in etcd via ReconcileInstanceSpec.
func (s *Service) ApplyUpgrade(ctx context.Context, databaseID, targetImage string) (*ApplyUpgradeResult, error) {
	currentSpec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrDatabaseNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database spec: %w", err)
	}

	currentDB, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrDatabaseNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if !DatabaseStateModifiable(currentDB.State) {
		return nil, ErrDatabaseNotModifiable
	}

	currentVersion, err := ds.ParsePgEdgeVersion(currentSpec.PostgresVersion, currentSpec.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version: %w", err)
	}

	upgrade, err := s.orchestrator.FindUpgrade(currentVersion, targetImage)
	if err != nil {
		return nil, err
	}

	// Capture previous state and spec before mutation for rollback.
	prevState := currentDB.State
	prevSpec := currentSpec.Spec

	// Clone and update the spec with the new postgres version.
	newSpec := currentSpec.Clone()
	currentPG := currentSpec.PostgresVersion
	newSpec.PostgresVersion = upgrade.PostgresVersion
	// Clear per-node version overrides that match the old top-level version so
	// all nodes pick up the new version uniformly.
	for _, node := range newSpec.Nodes {
		nodeVersion := node.PostgresVersion
		if nodeVersion == "" {
			nodeVersion = currentPG
		}
		if nodeVersion == currentPG {
			node.PostgresVersion = ""
		}
	}
	newSpec.NormalizePostgresVersions()
	// The orchestrator only accepts a target one Spock major version ahead,
	// in which case the spec moves to that major version.
	if IsSpockMajorUpgrade(currentSpec.SpockVersion, upgrade.SpockVersion) {
		newSpec.SpockVersion = upgrade.SpockVersion
		if err := spockMajorUpgradeTopologyChanged(currentSpec.Spec, newSpec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidDatabaseUpdate, err)
		}
		if err := s.validateSpockMajorUpgrade(ctx, currentSpec.Spec, newSpec); err != nil {
			return nil, err
		}
	}

	db, err := s.applySpecUpdate(ctx, currentSpec, currentDB, newSpec, DatabaseStateModifying)
	if err != nil {
		return nil, err
	}

	return &ApplyUpgradeResult{
		Database:  db,
		PrevState: prevState,
		prevSpec:  prevSpec,
	}, nil
}

// applySpecUpdate persists newSpec and newState atomically and returns the
// resulting Database. currentSpec and currentDB must already be fetched and
// validated by the caller.
func (s *Service) applySpecUpdate(
	ctx context.Context,
	currentSpec *StoredSpec,
	currentDB *StoredDatabase,
	newSpec *Spec,
	newState DatabaseState,
) (*Database, error) {
	instances, err := s.GetInstances(ctx, newSpec.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}
	serviceInstances, err := s.GetServiceInstances(ctx, newSpec.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instances: %w", err)
	}

	currentSpec.Spec = newSpec
	currentDB.UpdatedAt = time.Now()
	currentDB.State = newState

	if err := s.store.Txn(
		s.store.Spec.Update(currentSpec),
		s.store.Database.Update(currentDB),
	).Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to persist database: %w", err)
	}

	return storedToDatabase(currentDB, currentSpec, instances, serviceInstances), nil
}

// RollbackApplyUpgrade atomically restores the database spec and state to
// their values before ApplyUpgrade was called. It is intended to be called
// when the workflow fails to start after ApplyUpgrade has already committed.
func (s *Service) RollbackApplyUpgrade(ctx context.Context, result *ApplyUpgradeResult) error {
	databaseID := result.Database.DatabaseID

	currentStoredSpec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrDatabaseNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get database spec for rollback: %w", err)
	}

	currentDB, err := s.store.Database.GetByKey(databaseID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrDatabaseNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get database for rollback: %w", err)
	}

	currentStoredSpec.Spec = result.prevSpec
	currentDB.State = result.PrevState
	currentDB.UpdatedAt = time.Now()

	if err := s.store.Txn(
		s.store.Spec.Update(currentStoredSpec),
		s.store.Database.Update(currentDB),
	).Commit(ctx); err != nil {
		return fmt.Errorf("failed to roll back upgrade: %w", err)
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

	if err := s.orchestrator.ReconcileServiceInstanceSpec(previous, spec); err != nil {
		return nil, fmt.Errorf("failed to reconcile service instance spec: %w", err)
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

	// Immutable: locale is only applied by initdb when an instance is first
	// bootstrapped, so a change would silently diverge from the actual
	// database locale.
	if !localesMatch(current.Locale, updated.Locale) {
		errs = append(errs, errors.New("locale cannot be changed"))
	}

	// Spock version is a database-wide property (every node inherits
	// Spec.SpockVersion; there's no per-node override), so it's checked once
	// here rather than per-instance. Checking it per-instance would miss a
	// major version change submitted alongside a HostIDs change, since that
	// changes the instance ID and would no longer match between current and
	// updated.
	if err := spockMajorVersionChanged(current.SpockVersion, updated.SpockVersion); err != nil {
		errs = append(errs, err)
	}
	if err := spockMajorUpgradeTopologyChanged(current, updated); err != nil {
		errs = append(errs, err)
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

// majorVersionChanged rejects a Postgres major version change for a matched
// instance. Unlike Spock, Postgres supports a per-node major-version override
// (Node.PostgresVersion), so this intentionally only checks instances that
// persist across the update: replacing an instance via a HostIDs change
// (which produces a new instance ID) is exempt, since that's the supported
// path for moving a node to a new Postgres major version during a rolling
// upgrade.
func majorVersionChanged(old, new *ds.PgEdgeVersion) error {
	if old == nil || new == nil {
		return errors.New("expected both current and updated versions to be defined")
	}
	return checkMajorUnchanged("postgres", old.PostgresVersion, new.PostgresVersion, "")
}

// spockMajorVersionChanged rejects unsupported Spock major version changes on
// an existing database. Upgrading by exactly one major version (5 to 6) is
// allowed: Spock 6 negotiates the replication protocol with 5.0.x peers, so
// nodes can be upgraded one at a time while the others still run 5.0.x. The
// live-version precondition for that upgrade (every node on 5.0.14 or later)
// is checked separately by Service.validateSpockMajorUpgrade, since it needs
// the observed instance versions. Downgrades, skipping a major version, and
// removing Spock are rejected. Adding Spock to an existing database is
// allowed, since its existing tables are added to the replication sets when
// the Spock node is created.
func spockMajorVersionChanged(current, updated string) error {
	switch {
	case current == "":
		return nil
	case updated == "":
		return fmt.Errorf("spock version changed from %s to none: removing spock from an existing database is not supported", current)
	}
	oldMajor, newMajor, err := spockMajors(current, updated)
	if err != nil {
		return err
	}
	switch {
	case newMajor == oldMajor, newMajor == oldMajor+1:
		return nil
	case newMajor < oldMajor:
		return fmt.Errorf("spock major version changed from %d to %d: downgrading the spock major version is not supported", oldMajor, newMajor)
	default:
		return fmt.Errorf("spock major version changed from %d to %d: upgrade one spock major version at a time", oldMajor, newMajor)
	}
}

// IsSpockMajorUpgrade reports whether updated is a later Spock major version
// than current. It's false when either version is empty or unparseable.
func IsSpockMajorUpgrade(current, updated string) bool {
	if current == "" || updated == "" {
		return false
	}
	oldMajor, newMajor, err := spockMajors(current, updated)
	return err == nil && newMajor > oldMajor
}

func spockMajors(current, updated string) (uint64, uint64, error) {
	oldVersion, err := ds.ParseVersion(current)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse current spock version: %w", err)
	}
	newVersion, err := ds.ParseVersion(updated)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse updated spock version: %w", err)
	}
	oldMajor, ok := oldVersion.Major()
	if !ok {
		return 0, 0, errors.New("current spock version is missing its major component")
	}
	newMajor, ok := newVersion.Major()
	if !ok {
		return 0, 0, errors.New("updated spock version is missing its major component")
	}
	return oldMajor, newMajor, nil
}

// spockMajorUpgradeTopologyChanged rejects a Spock major upgrade that also
// adds, removes, or moves nodes. The upgrade rolls through the existing
// instances one node at a time, and new instances would be created on the new
// major while some peers still run the old one.
func spockMajorUpgradeTopologyChanged(current, updated *Spec) error {
	if !IsSpockMajorUpgrade(current.SpockVersion, updated.SpockVersion) {
		return nil
	}
	return checkNodesUnchanged(current, updated)
}

// checkNodesUnchanged returns an error if updated adds, removes, or moves
// nodes compared to current.
func checkNodesUnchanged(current, updated *Spec) error {
	if len(current.Nodes) != len(updated.Nodes) {
		return errSpockUpgradeTopology
	}
	return checkNodesNotAddedOrMoved(current, updated)
}

var errSpockUpgradeTopology = errors.New("a spock major version upgrade cannot be combined with adding, removing, or moving nodes; upgrade spock first, then change the nodes in a separate update")

// checkNodesNotAddedOrMoved returns an error if updated adds a node or moves
// an existing node to different hosts. Removing nodes is allowed.
func checkNodesNotAddedOrMoved(current, updated *Spec) error {
	hostsByNode := make(map[string][]string, len(current.Nodes))
	for _, node := range current.Nodes {
		hostsByNode[node.Name] = node.HostIDs
	}
	for _, node := range updated.Nodes {
		hosts, ok := hostsByNode[node.Name]
		if !ok || !ds.NewSet(hosts...).Equal(ds.NewSet(node.HostIDs...)) {
			return errSpockUpgradeTopology
		}
	}
	return nil
}

// minSpockVersionForMajorUpgrade is the oldest Spock version that can be
// upgraded to Spock 6. The Spock extension's only upgrade path to 6.0.0
// starts at 5.0.14.
var minSpockVersionForMajorUpgrade = ds.MustParseVersion("5.0.14")

// validateSpockMajorUpgrade checks that every instance of the database is
// observed running Spock 5.0.14 or later before a Spock major upgrade starts.
// The spec only records the Spock major version, so this uses each instance's
// last reported status.
//
// When the stored spec already has the new major version but some instance
// still runs the old one, an update that failed partway through the upgrade
// is being submitted again. The versions were checked when the upgrade
// started, so they aren't checked again; that also lets an operator recover
// when an instance is down and isn't reporting status. Nodes can't be added
// or moved until the upgrade finishes, but a broken node can be removed.
func (s *Service) validateSpockMajorUpgrade(ctx context.Context, current, updated *Spec) error {
	if updated.SpockVersion == "" {
		return nil
	}
	var err error
	switch {
	case IsSpockMajorUpgrade(current.SpockVersion, updated.SpockVersion):
		instances, getErr := s.GetInstances(ctx, current.DatabaseID)
		if getErr != nil {
			return fmt.Errorf("failed to get instances: %w", getErr)
		}
		err = errors.Join(checkSpockUpgradeVersions(instances)...)
	case current.SpockVersion == updated.SpockVersion:
		instances, getErr := s.GetInstances(ctx, current.DatabaseID)
		if getErr != nil {
			return fmt.Errorf("failed to get instances: %w", getErr)
		}
		if spockUpgradeInProgress(instances, updated.SpockVersion) {
			err = checkNodesNotAddedOrMoved(current, updated)
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidDatabaseUpdate, err)
	}
	return nil
}

// spockUpgradeInProgress reports whether any instance is still recorded on a
// lower Spock major version than target. It uses each instance's stored
// version, which the Control Plane sets when it updates the instance, rather
// than its reported status, so an instance that is down or not reporting
// status still counts.
func spockUpgradeInProgress(instances []*Instance, target string) bool {
	for _, instance := range instances {
		version := instance.PgEdgeVersion
		if version == nil || !version.HasSpock() {
			continue
		}
		if IsSpockMajorUpgrade(version.SpockVersion.String(), target) {
			return true
		}
	}
	return false
}

// checkSpockUpgradeVersions returns an error for every instance whose observed
// Spock version is unknown or older than minSpockVersionForMajorUpgrade.
func checkSpockUpgradeVersions(instances []*Instance) []error {
	if len(instances) == 0 {
		return []error{errors.New("cannot upgrade spock: the database has no instances")}
	}
	var errs []error
	for _, instance := range instances {
		var observed string
		if instance.Status != nil && !instance.Status.IsStale() {
			observed = utils.FromPointer(instance.Status.SpockVersion)
		}
		if observed == "" {
			errs = append(errs, fmt.Errorf("cannot upgrade spock: the spock version of instance %s is unknown; make sure every instance is running and reporting status", instance.InstanceID))
			continue
		}
		version, err := ds.ParseVersion(observed)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot upgrade spock: instance %s reports an unrecognized spock version %q", instance.InstanceID, observed))
			continue
		}
		if version.Compare(minSpockVersionForMajorUpgrade) < 0 {
			errs = append(errs, fmt.Errorf("cannot upgrade spock: instance %s runs spock %s, but upgrading to the next spock major version requires %s or later; update the database to an image with spock %s first", instance.InstanceID, observed, minSpockVersionForMajorUpgrade, minSpockVersionForMajorUpgrade))
		}
	}
	return errs
}

// checkMajorUnchanged returns an error if old and new have different major
// version components. label identifies the version kind (e.g. "postgres",
// "spock") in every error message, and detail is appended verbatim to the
// mismatch error so callers can add version-kind-specific context.
func checkMajorUnchanged(label string, old, new *ds.Version, detail string) error {
	oldMajor, ok := old.Major()
	if !ok {
		return fmt.Errorf("current %s version is missing its major component", label)
	}
	newMajor, ok := new.Major()
	if !ok {
		return fmt.Errorf("updated %s version is missing its major component", label)
	}
	if oldMajor != newMajor {
		return fmt.Errorf("%s major version changed from %d to %d%s", label, oldMajor, newMajor, detail)
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

// describeVersion formats the given version for use in error messages.
func describeVersion(v *ds.PgEdgeVersion) string {
	if !v.HasSpock() {
		return fmt.Sprintf("postgres=%s", v.PostgresVersion)
	}
	return fmt.Sprintf("postgres=%s, spock=%s", v.PostgresVersion, v.SpockVersion)
}

// validateSpockRequirements validates the parts of the spec that depend on
// whether the database uses Spock. Databases without Spock are limited to a
// single node, since there is no replication between nodes. Databases with
// Spock must keep spock in shared_preload_libraries if it's overridden.
func validateSpockRequirements(spec *Spec, version *ds.PgEdgeVersion, hostsByID map[string]*host.Host) error {
	if !version.HasSpock() {
		if len(spec.Nodes) <= 1 {
			return nil
		}
		err := errors.New("databases without spock are limited to a single node")
		if spockVersion := availableSpockVersion(spec, hostsByID); spockVersion != "" {
			// Spock is only defaulted from the hosts' default version when a
			// database is created, so the hosts can support Spock for this
			// Postgres version even though the database doesn't use it, e.g.
			// when Spock was installed after the database was created.
			return fmt.Errorf("%w: set spock_version to %q to add spock to this database", err, spockVersion)
		}
		return err
	}

	var errs []error
	errs = append(errs, validateSpockPreloaded(spec.PostgreSQLConf, validation.NewPath("postgresql_conf"))...)
	for i, node := range spec.Nodes {
		path := validation.NewPath("nodes", validation.ArrayIndexElement(i), "postgresql_conf")
		errs = append(errs, validateSpockPreloaded(node.PostgreSQLConf, path)...)
	}

	return errors.Join(errs...)
}

// availableSpockVersion returns the greatest Spock version that every host in
// the spec's nodes supports with its node's Postgres version, or "" if there
// is no such version.
func availableSpockVersion(spec *Spec, hostsByID map[string]*host.Host) string {
	var common ds.Set[string]
	spockVersions := map[string]*ds.Version{}
	for _, node := range spec.Nodes {
		pgVersionStr := spec.PostgresVersion
		if node.PostgresVersion != "" {
			pgVersionStr = node.PostgresVersion
		}
		pgVersion, err := ds.ParseVersion(pgVersionStr)
		if err != nil {
			return ""
		}
		for _, hostID := range node.HostIDs {
			h, ok := hostsByID[hostID]
			if !ok {
				return ""
			}
			supported := ds.NewSet[string]()
			for _, v := range h.SupportedPgEdgeVersions {
				if v.HasSpock() && v.PostgresVersion.Compare(pgVersion) == 0 {
					spockVersion := v.SpockVersion.String()
					supported.Add(spockVersion)
					spockVersions[spockVersion] = v.SpockVersion
				}
			}
			if common == nil {
				common = supported
			} else {
				common = common.Intersection(supported)
			}
		}
	}

	var greatest *ds.Version
	for spockVersion := range common {
		if v := spockVersions[spockVersion]; greatest == nil || v.Compare(greatest) > 0 {
			greatest = v
		}
	}
	if greatest == nil {
		return ""
	}
	return greatest.String()
}

func validateSpockPreloaded(conf map[string]any, path validation.Path) []error {
	key, libs, ok := sharedPreloadLibrariesParam(conf)
	// Postgres appends the ".so" extension to names that don't have it, so
	// spock is preloaded with or without it.
	if !ok || slices.Contains(libs, "spock") || slices.Contains(libs, "spock.so") {
		// An absent key means that the defaults will be applied, including
		// spock.
		return nil
	}
	err := errors.New(`'spock' must be included in shared_preload_libraries`)
	return []error{validation.NewError(err, path.AppendMapKey(key))}
}

// validatePreloadedLibraries validates that every library in the node's
// shared_preload_libraries is an available library in the given host version.
// The node-level parameter takes precedence over the spec-level parameter.
// Path-style entries and hosts that don't report their available libraries
// are not validated.
func validatePreloadedLibraries(spec *Spec, nodeIdx int, hostID string, version *ds.PgEdgeVersion) error {
	if version.AvailableLibraries == nil {
		return nil
	}

	path := validation.NewPath("nodes", validation.ArrayIndexElement(nodeIdx), "postgresql_conf")
	key, libs, ok := sharedPreloadLibrariesParam(spec.Nodes[nodeIdx].PostgreSQLConf)
	if !ok {
		path = validation.NewPath("postgresql_conf")
		key, libs, ok = sharedPreloadLibrariesParam(spec.PostgreSQLConf)
	}
	if !ok {
		// The defaults will be applied, which are always available.
		return nil
	}

	var errs []error
	for _, lib := range libs {
		// Postgres appends the ".so" extension to bare names that don't have
		// it, so names are valid with or without it.
		name := strings.TrimSuffix(lib, ".so")
		switch {
		case strings.Contains(lib, "/"):
			// Postgres loads names that contain a slash as file paths, so
			// they can refer to libraries outside of the library directory.
		case !slices.Contains(version.AvailableLibraries, name):
			err := fmt.Errorf("'%s' is not installed on host %s for %s", lib, hostID, describeVersion(version))
			errs = append(errs, validation.NewError(err, path.AppendMapKey(key)))
		}
	}

	return errors.Join(errs...)
}

// sharedPreloadLibrariesParam returns the key and the library names of the
// shared_preload_libraries parameter in the given conf. It returns false if
// the parameter is absent, is not a string, or is not a valid list.
func sharedPreloadLibrariesParam(conf map[string]any) (string, []string, bool) {
	for key, val := range conf {
		if postgres.NormalizeParameterName(key) != "shared_preload_libraries" {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			// The type is validated by the API.
			return "", nil, false
		}
		libs, err := postgres.SplitLibraryList(strVal)
		if err != nil {
			// The syntax is validated by the API.
			return "", nil, false
		}
		return key, libs, true
	}
	return "", nil, false
}

// validateLocaleVersions rejects the builtin locale provider when the spec or
// any node uses a Postgres version older than 17, where it does not exist.
func validateLocaleVersions(spec *Spec) error {
	if spec.Locale == nil || spec.Locale.Provider != "builtin" {
		return nil
	}

	versions := []string{spec.PostgresVersion}
	for _, node := range spec.Nodes {
		if node.PostgresVersion != "" {
			versions = append(versions, node.PostgresVersion)
		}
	}

	for _, v := range versions {
		version, err := ds.ParseVersion(v)
		if err != nil {
			return fmt.Errorf("failed to parse postgres version %q: %w", v, err)
		}
		if major, ok := version.Major(); ok && major < 17 {
			return fmt.Errorf("the 'builtin' locale provider requires Postgres 17 or later, got %s", v)
		}
	}
	return nil
}

func localesMatch(a, b *Locale) bool {
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
