package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"syscall"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type Orchestrator struct {
	cfg            config.Config
	logger         zerolog.Logger
	client         *Client
	packageManager PackageManager
	cpus           int
	memBytes       uint64
}

func NewOrchestrator(
	cfg config.Config,
	loggerFactory *logging.Factory,
	client *Client,
	packageManager PackageManager,
) (*Orchestrator, error) {
	logger := loggerFactory.Logger("systemd_orchestrator")
	logger.Debug().Msg("initializing orchestrator")

	mem, err := readTotalMemory()
	if err != nil {
		return nil, err
	}
	cpu := runtime.NumCPU()

	logger.Debug().
		Uint64("mem", mem).
		Int("cpu", cpu).
		Msg("got system stats")

	return &Orchestrator{
		cfg:            cfg,
		logger:         logger,
		client:         client,
		packageManager: packageManager,
		cpus:           cpu,
		memBytes:       mem,
	}, nil
}

func (o *Orchestrator) Start(ctx context.Context) error {
	return o.client.Start(ctx)
}

func (o *Orchestrator) PopulateHost(ctx context.Context, h *host.Host) error {
	o.logger.Debug().Msg("querying installed versions")

	versions, err := o.packageManager.InstalledPostgresVersions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get installed postgres versions: %w", err)
	}

	o.logger.Debug().
		Int("version_count", len(versions)).
		Msg("got installed versions")

	var supported []*ds.PgEdgeVersion
	for _, installed := range versions {
		if len(installed.Spock) == 0 {
			o.logger.Debug().
				Str("postgres_name", installed.Postgres.Name).
				Str("postgres_major", installed.Postgres.PostgresMajor).
				Str("postgres_version", installed.Postgres.Version.String()).
				Msg("missing spock for this postgres version")
			// We need spock
			continue
		}

		o.logger.Debug().
			Str("postgres_name", installed.Postgres.Name).
			Str("postgres_major", installed.Postgres.PostgresMajor).
			Str("postgres_version", installed.Postgres.Version.String()).
			Msg("postgres version")

		for _, spock := range installed.Spock {
			o.logger.Debug().
				Str("spock_name", spock.Name).
				Str("spock_postgres_major", spock.PostgresMajor).
				Str("spock_version", spock.Version.String()).
				Msg("spock version")

			version := &ds.PgEdgeVersion{
				PostgresVersion: installed.Postgres.Version,
				SpockVersion:    spock.Version.MajorVersion(),
			}
			supported = append(supported, version)

			o.logger.Debug().
				Str("version", version.String()).
				Msg("pgedge version")
		}

	}
	if len(supported) == 0 {
		return errors.New("pgedge postgres not installed")
	}
	slices.SortFunc(supported, func(a, b *ds.PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})

	h.CPUs = int(o.cpus)
	h.MemBytes = o.memBytes
	h.DefaultPgEdgeVersion = supported[0]
	h.SupportedPgEdgeVersions = supported

	return nil
}

func (o *Orchestrator) PopulateHostStatus(ctx context.Context, h *host.HostStatus) error {
	// TODO: are there any systemd-specific components to report here?
	// We could use gosigar to query some system stats like mem or CPU usage

	return nil
}

func (o *Orchestrator) ReconcileInstanceSpec(_, _ *database.InstanceSpec) error {
	return nil
}

func (o *Orchestrator) ReconcileServiceInstanceSpec(_, _ *database.ServiceInstanceSpec) error {
	return nil
}

func (o *Orchestrator) AvailableUpgrades(_ *ds.PgEdgeVersion) []*database.AvailableUpgrade {
	return nil
}

func (o *Orchestrator) FindUpgrade(_ *ds.PgEdgeVersion, _ string) (*database.AvailableUpgrade, error) {
	return nil, fmt.Errorf("%w: install the target package via your OS package manager before applying an upgrade on systemd-managed databases", database.ErrUpgradeNotAvailable)
}

func (o *Orchestrator) GenerateInstanceResources(spec *database.InstanceSpec, scripts database.Scripts) (*database.InstanceResources, error) {
	paths, err := o.InstancePaths(spec.PgEdgeVersion.PostgresVersion, spec.InstanceID)
	if err != nil {
		return nil, err
	}
	databaseOwnerUID, databaseOwnerGID, err := o.databaseOwnerIDs()
	if err != nil {
		return nil, fmt.Errorf("failed to get database owner uid/gid: %w", err)
	}

	// directory resources
	instanceDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-instance",
		HostID:   spec.HostID,
		Path:     paths.Host.BaseDir,
		OwnerUID: databaseOwnerUID,
		OwnerGID: databaseOwnerGID,
	}
	dataDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-data",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "data",
		OwnerUID: databaseOwnerUID,
		OwnerGID: databaseOwnerGID,
	}
	configsDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-configs",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "configs",
		OwnerUID: databaseOwnerUID,
		OwnerGID: databaseOwnerGID,
	}
	certificatesDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-certificates",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "certificates",
		OwnerUID: databaseOwnerUID,
		OwnerGID: databaseOwnerGID,
	}

	// patroni resources - used to clean up etcd on deletion
	patroniCluster := &common.PatroniCluster{
		DatabaseID: spec.DatabaseID,
		NodeName:   spec.NodeName,
	}
	patroniMember := &common.PatroniMember{
		DatabaseID: spec.DatabaseID,
		NodeName:   spec.NodeName,
		InstanceID: spec.InstanceID,
	}

	// file resources
	etcdCreds := &common.EtcdCreds{
		InstanceID: spec.InstanceID,
		HostID:     spec.HostID,
		DatabaseID: spec.DatabaseID,
		NodeName:   spec.NodeName,
		ParentID:   certificatesDir.ID,
		OwnerUID:   databaseOwnerUID,
		OwnerGID:   databaseOwnerGID,
	}
	postgresCerts := &common.PostgresCerts{
		InstanceID:        spec.InstanceID,
		HostID:            spec.HostID,
		ParentID:          certificatesDir.ID,
		InstanceAddresses: o.cfg.Addresses(),
		OwnerUID:          databaseOwnerUID,
		OwnerGID:          databaseOwnerGID,
	}

	// These should be caught by `ValidateInstanceSpecs`, but just in case
	patroniPort := utils.FromPointer(spec.PatroniPort)
	if patroniPort == 0 {
		return nil, fmt.Errorf("patroni_port is required for systemd instances, missing for instance '%s'", spec.InstanceID)
	}
	postgresPort := utils.FromPointer(spec.Port)
	if postgresPort == 0 {
		return nil, fmt.Errorf("port is required for systemd instances, missing for instance '%s'", spec.InstanceID)
	}

	patroniConfig := &PatroniConfig{
		DatabaseID: spec.DatabaseID,
		AllHostIDs: spec.AllHostIDs,
		Base: &common.PatroniConfig{
			InstanceID: spec.InstanceID,
			HostID:     spec.HostID,
			NodeName:   spec.NodeName,
			Generator: common.NewPatroniConfigGenerator(common.PatroniConfigGeneratorOptions{
				Instance:        spec,
				HostCPUs:        float64(o.cpus),
				HostMemoryBytes: o.memBytes,
				PatroniPort:     patroniPort,
				PostgresPort:    postgresPort,
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,spock",
				},
				FQDN:  o.cfg.PeerAddress(),
				Paths: paths,
			}),
			ParentID: configsDir.ID,
			OwnerUID: databaseOwnerUID,
			OwnerGID: databaseOwnerGID,
		},
	}

	pgMajor, ok := spec.PgEdgeVersion.PostgresVersion.MajorString()
	if !ok {
		return nil, errors.New("got empty postgres version")
	}

	databaseOwnerUser := strconv.Itoa(databaseOwnerUID)
	databaseOwnerGroup := strconv.Itoa(databaseOwnerGID)
	patroniUnit := &UnitResource{
		DatabaseID: spec.DatabaseID,
		HostID:     spec.HostID,
		Name:       patroniServiceName(spec.InstanceID),
		Options:    PatroniUnitOptions(paths, o.packageManager.BinDir(pgMajor), spec.CPUs, spec.MemoryBytes, databaseOwnerUser, databaseOwnerGroup),
		ExtraDependencies: []resource.Identifier{
			patroniConfig.Identifier(),
			instanceDir.Identifier(),
			dataDir.Identifier(),
			configsDir.Identifier(),
			certificatesDir.Identifier(),
			etcdCreds.Identifier(),
			postgresCerts.Identifier(),
		},
	}

	instance := &database.InstanceResource{
		Spec:             spec,
		InstanceHostname: o.cfg.PeerAddress(),
		PostInit:         scripts[database.ScriptNamePostInit],
		OrchestratorDependencies: []resource.Identifier{
			patroniUnit.Identifier(),
		},
	}

	instanceDependencies := []resource.Resource{
		patroniCluster,
		patroniMember,
		instanceDir,
		dataDir,
		configsDir,
		certificatesDir,
		etcdCreds,
		postgresCerts,
		patroniConfig,
		patroniUnit,
	}

	dbDependencies := []resource.Resource{&common.PgServiceConf{
		ParentID:   configsDir.ID,
		HostID:     spec.HostID,
		InstanceID: spec.InstanceID,
		OwnerUID:   databaseOwnerUID,
		OwnerGID:   databaseOwnerGID,
	}}

	var nodeDependents []resource.Resource
	if spec.BackupConfig != nil {
		instanceDependencies = append(instanceDependencies,
			&common.PgBackRestConfig{
				InstanceID:   spec.InstanceID,
				HostID:       spec.HostID,
				DatabaseID:   spec.DatabaseID,
				NodeName:     spec.NodeName,
				Repositories: spec.BackupConfig.Repositories,
				ParentID:     configsDir.ID,
				Type:         pgbackrest.ConfigTypeBackup,
				OwnerUID:     databaseOwnerUID,
				OwnerGID:     databaseOwnerGID,
				Paths:        paths,
				Port:         postgresPort,
			},
		)
		nodeDependents = append(nodeDependents,
			&common.PgBackRestStanza{
				DatabaseID: spec.DatabaseID,
				NodeName:   spec.NodeName,
			},
		)
		for _, schedule := range spec.BackupConfig.Schedules {
			nodeDependents = append(nodeDependents, scheduler.NewScheduledJobResource(
				fmt.Sprintf("%s-%s-%s", schedule.ID, spec.DatabaseID, spec.NodeName),
				schedule.CronExpression,
				scheduler.WorkflowCreatePgBackRestBackup,
				map[string]any{
					"database_id": spec.DatabaseID,
					"node_name":   spec.NodeName,
					"type":        pgbackrest.BackupType(schedule.Type).String(),
				},
				[]resource.Identifier{common.PgBackRestStanzaIdentifier(spec.NodeName)},
			))
		}
	}

	if spec.RestoreConfig != nil {
		instanceDependencies = append(instanceDependencies, &common.PgBackRestConfig{
			InstanceID:   spec.InstanceID,
			HostID:       spec.HostID,
			DatabaseID:   spec.RestoreConfig.SourceDatabaseID,
			NodeName:     spec.RestoreConfig.SourceNodeName,
			Repositories: []*pgbackrest.Repository{spec.RestoreConfig.Repository},
			ParentID:     configsDir.ID,
			Type:         pgbackrest.ConfigTypeRestore,
			OwnerUID:     databaseOwnerUID,
			OwnerGID:     databaseOwnerGID,
			Paths:        paths,
			Port:         postgresPort,
		})
	}

	return database.NewInstanceResources(instance, instanceDependencies, dbDependencies, nodeDependents)
}

func (o *Orchestrator) GenerateServiceInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	return nil, errors.New("unimplemented")
}

func (o *Orchestrator) GenerateInstanceRestoreResources(spec *database.InstanceSpec, taskID uuid.UUID) (*database.InstanceResources, error) {
	if spec.RestoreConfig == nil {
		return nil, fmt.Errorf("missing restore config for node %s instance %s", spec.NodeName, spec.InstanceID)
	}
	paths, err := o.InstancePaths(spec.PgEdgeVersion.PostgresVersion, spec.InstanceID)
	if err != nil {
		return nil, err
	}

	restoreSpec := *spec
	restoreSpec.InPlaceRestore = true

	instance, err := o.GenerateInstanceResources(&restoreSpec, nil)
	if err != nil {
		return nil, err
	}

	err = instance.AddInstanceDependencies(&PgBackRestRestore{
		DatabaseID:     spec.DatabaseID,
		HostID:         spec.HostID,
		InstanceID:     spec.InstanceID,
		TaskID:         taskID,
		NodeName:       spec.NodeName,
		RestoreOptions: spec.RestoreConfig.RestoreOptions,
		Paths:          paths,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add restore resource to instance resources: %w", err)
	}

	return instance, nil
}

func (o *Orchestrator) GetInstanceConnectionInfo(ctx context.Context,
	databaseID, instanceID string,
	postgresPort, patroniPort *int,
	pgEdgeVersion *ds.PgEdgeVersion,
) (*database.ConnectionInfo, error) {
	if postgresPort == nil {
		return nil, fmt.Errorf("postgres port is not yet recorded for this instance")
	}
	if patroniPort == nil {
		return nil, fmt.Errorf("patroni port is not yet recorded for this instance")
	}
	if pgEdgeVersion == nil {
		return nil, fmt.Errorf("postgres version is not yet recorded for this instance")
	}

	paths, err := o.InstancePaths(pgEdgeVersion.PostgresVersion, instanceID)
	if err != nil {
		return nil, err
	}

	postgresPortInt := utils.FromPointer(postgresPort)
	patroniPortInt := utils.FromPointer(patroniPort)

	return &database.ConnectionInfo{
		AdminHost:        "localhost",
		AdminPort:        postgresPortInt,
		PeerHost:         o.cfg.PeerAddress(),
		PeerPort:         postgresPortInt,
		PeerSSLCert:      paths.Instance.PostgresSuperuserCert(),
		PeerSSLKey:       paths.Instance.PostgresSuperuserKey(),
		PeerSSLRootCert:  paths.Instance.PostgresCaCert(),
		PatroniPort:      patroniPortInt,
		ClientAddresses:  o.cfg.ClientAddresses,
		ClientPort:       postgresPortInt,
		InstanceHostname: o.cfg.PeerAddress(),
	}, nil
}

func (o *Orchestrator) GetServiceInstanceStatus(ctx context.Context, serviceInstanceID string) (*database.ServiceInstanceStatus, error) {
	return nil, errors.New("unimplemented")
}

func (o *Orchestrator) ExecuteInstanceCommand(ctx context.Context, w io.Writer, databaseID, instanceID string, args ...string) error {
	if len(args) == 0 {
		return errors.New("got empty args")
	}
	databaseOwnerUID, databaseOwnerGID, err := o.databaseOwnerIDs()
	if err != nil {
		return fmt.Errorf("failed to get database owner uid/gid: %w", err)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(databaseOwnerUID),
			Gid: uint32(databaseOwnerGID),
		},
	}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("instance '%s' command '%s' failed: %w", instanceID, args[0], err)
	}
	return nil
}

func (o *Orchestrator) CreatePgBackRestBackup(ctx context.Context, w io.Writer, spec *database.InstanceSpec, options *pgbackrest.BackupOptions) error {
	paths, err := o.InstancePaths(spec.PgEdgeVersion.PostgresVersion, spec.InstanceID)
	if err != nil {
		return err
	}

	cmd := paths.PgBackRestBackupCmd("backup", options.StringSlice()...)
	return o.ExecuteInstanceCommand(ctx, w, spec.DatabaseID, spec.InstanceID, cmd.StringSlice()...)
}

func (o *Orchestrator) ValidateInstanceSpecs(_ context.Context, changes []*database.InstanceSpecChange) ([]*database.ValidationResult, error) {
	// TODO: validate posix backup and restore repository directories
	results := make([]*database.ValidationResult, 0)

	for _, ch := range changes {
		result := &database.ValidationResult{
			Valid:    true,
			NodeName: ch.Current.NodeName,
			HostID:   ch.Current.HostID,
		}
		var prevPort *int
		var prevPatroniPort *int
		if ch.Previous != nil {
			prevPort = ch.Previous.Port
			prevPatroniPort = ch.Previous.PatroniPort
		}
		if err := validatePort(prevPort, ch.Current.Port); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("postgres port: %v", err))
		}
		if err := validatePort(prevPatroniPort, ch.Current.PatroniPort); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("patroni port: %v", err))
		}

		results = append(results, result)
	}

	return results, nil
}

func validatePort(previous, current *int) error {
	if current == nil {
		return errors.New("port must be defined")
	}
	if *current == 0 {
		// When port is 0, we'll allocate a free port at deploy time
		return nil
	}
	if *current > 65535 {
		return fmt.Errorf("port %d is out of range", *current)
	}
	if ptrEqual(previous, current) {
		return nil
	}
	return checkPortAvailable(*current)
}

func checkPortAvailable(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("cannot bind port %d: %w", port, err)
	}
	defer l.Close()
	return nil
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (o *Orchestrator) StopInstance(ctx context.Context, instanceID string) error {
	if err := o.client.StopUnit(ctx, patroniServiceName(instanceID), true); err != nil {
		return fmt.Errorf("failed to stop patroni unit: %w", err)
	}
	return nil
}

func (o *Orchestrator) StartInstance(ctx context.Context, instanceID string) error {
	if err := o.client.StartUnit(ctx, patroniServiceName(instanceID)); err != nil {
		return fmt.Errorf("failed to start patroni unit: %w", err)
	}
	return nil
}

func (o *Orchestrator) WorkerQueues() ([]workflow.Queue, error) {
	return []workflow.Queue{
		utils.AnyQueue(),
		utils.HostQueue(o.cfg.HostID),
	}, nil
}

func (o *Orchestrator) NodeDSN(ctx context.Context, rc *resource.Context, nodeName string, fromInstanceID string, dbName string) (*postgres.DSN, error) {
	return &postgres.DSN{
		Service: nodeName,
		DBName:  dbName,
	}, nil
}

func (o *Orchestrator) InstancePaths(pgVersion *ds.Version, instanceID string) (database.InstancePaths, error) {
	pgMajor, ok := pgVersion.MajorString()
	if !ok {
		return database.InstancePaths{}, errors.New("got empty postgres version")
	}

	var baseDir string
	if o.cfg.SystemD.InstanceDataDir != "" {
		baseDir = filepath.Join(o.cfg.SystemD.InstanceDataDir, pgMajor, instanceID)
	} else {
		baseDir = filepath.Join(o.packageManager.InstanceDataBaseDir(pgMajor), instanceID)
	}

	return database.InstancePaths{
		Instance:       database.Paths{BaseDir: baseDir},
		Host:           database.Paths{BaseDir: baseDir},
		PgBackRestPath: o.cfg.SystemD.PgBackRestPath,
		PatroniPath:    o.cfg.SystemD.PatroniPath,
	}, nil
}

func (o *Orchestrator) databaseOwnerIDs() (int, int, error) {
	u, err := user.Lookup("postgres")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to lookup 'postgres' user: %w", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse 'postgres' uid: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse 'postgres' gid: %w", err)
	}
	if o.cfg.DatabaseOwnerUID > 0 {
		uid = o.cfg.DatabaseOwnerUID
	}
	if o.cfg.DatabaseOwnerGID > 0 {
		gid = o.cfg.DatabaseOwnerGID
	}

	return uid, gid, nil
}

func patroniServiceName(instanceID string) string {
	return fmt.Sprintf("patroni-%s.service", instanceID)
}
