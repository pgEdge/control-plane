package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/client"
	"github.com/cschleiden/go-workflows/core"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	api "github.com/pgEdge/control-plane/api/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

var ErrNotImplemented = errors.New("endpoint not implemented")
var ErrAlreadyInitialized = api.MakeClusterAlreadyInitialized(errors.New("cluster is already initialized"))

var _ api.Service = (*Service)(nil)

type Service struct {
	cfg            config.Config
	logger         zerolog.Logger
	etcd           *etcd.EmbeddedEtcd
	hostSvc        *host.Service
	dbSvc          *database.Service
	taskSvc        *task.Service
	workflowClient *client.Client
	workflows      *workflows.Workflows
}

func NewService(
	cfg config.Config,
	logger zerolog.Logger,
	etcd *etcd.EmbeddedEtcd,
	hostSvc *host.Service,
	dbSvc *database.Service,
	taskSvc *task.Service,
	workflowClient *client.Client,
	workflows *workflows.Workflows,
) *Service {
	return &Service{
		cfg:            cfg,
		logger:         logger,
		etcd:           etcd,
		hostSvc:        hostSvc,
		dbSvc:          dbSvc,
		taskSvc:        taskSvc,
		workflowClient: workflowClient,
		workflows:      workflows,
	}
}

func (s *Service) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get join token: %w", err)
	}
	// TODO: Https support
	serverURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", s.cfg.IPv4Address, s.cfg.HTTP.Port),
	}
	return &api.ClusterJoinToken{
		Token:     token,
		ServerURL: serverURL.String(),
	}, nil
}

func (s *Service) GetJoinOptions(ctx context.Context, req *api.ClusterJoinRequest) (*api.ClusterJoinOptions, error) {
	if err := s.etcd.VerifyJoinToken(req.Token); err != nil {
		return nil, api.MakeInvalidJoinToken(fmt.Errorf("invalid join token: %w", err))
	}

	hostID, err := uuid.Parse(req.HostID)
	if err != nil {
		return nil, fmt.Errorf("invalid host ID %q: %w", req.HostID, err)
	}

	creds, err := s.etcd.AddPeerUser(ctx, etcd.HostCredentialOptions{
		HostID:      hostID,
		Hostname:    req.Hostname,
		IPv4Address: req.Ipv4Address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd user for new cluster member: %w", err)
	}

	peer := s.etcd.AsPeer()

	return &api.ClusterJoinOptions{
		Peer: &api.ClusterPeer{
			Name:      peer.Name,
			PeerURL:   peer.PeerURL,
			ClientURL: peer.ClientURL,
		},
		Credentials: &api.ClusterCredentials{
			CaCert:     base64.StdEncoding.EncodeToString(creds.CaCert),
			ClientCert: base64.StdEncoding.EncodeToString(creds.ClientCert),
			ClientKey:  base64.StdEncoding.EncodeToString(creds.ClientKey),
			ServerCert: base64.StdEncoding.EncodeToString(creds.ServerCert),
			ServerKey:  base64.StdEncoding.EncodeToString(creds.ServerKey),
		},
	}, nil
}

func (s *Service) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrNotImplemented
}

func (s *Service) InspectCluster(ctx context.Context) (*api.Cluster, error) {
	return nil, ErrNotImplemented
}

func (s *Service) ListHosts(ctx context.Context) ([]*api.Host, error) {
	hosts, err := s.hostSvc.GetAllHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
	apiHosts := make([]*api.Host, len(hosts))

	for idx, h := range hosts {
		apiHosts[idx] = hostToAPI(h)
	}
	return apiHosts, nil
}

func (s *Service) InspectHost(ctx context.Context, req *api.InspectHostPayload) (*api.Host, error) {
	return nil, ErrNotImplemented
}

func (s *Service) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	return ErrNotImplemented
}

// ListDatabases fetches all databases from the database service and converts them to API format.
func (s *Service) ListDatabases(ctx context.Context) (api.DatabaseCollection, error) {
	// Fetch databases from the database service
	databases, err := s.dbSvc.GetDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get databases: %w", err)
	}

	// Ensure we return an empty (non-nil) slice if no databases found
	if len(databases) == 0 {
		return api.DatabaseCollection{}, nil
	}

	// Preallocate the output slice with the length of the databases
	apiDatabases := make(api.DatabaseCollection, 0, len(databases))
	for _, db := range databases {
		apiDB := databaseToAPI(db)
		if apiDB != nil {
			// Only append non-nil API databases
			apiDatabases = append(apiDatabases, apiDB)
		}
	}

	return apiDatabases, nil
}

func (s *Service) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.Database, error) {
	spec, err := apiToDatabaseSpec(req.ID, req.TenantID, req.Spec)
	if err != nil {
		return nil, api.MakeInvalidInput(err)
	}

	db, err := s.dbSvc.CreateDatabase(ctx, spec)
	if errors.Is(err, database.ErrDatabaseAlreadyExists) {
		return nil, api.MakeDatabaseAlreadyExists(err)
	} else if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	_, err = s.workflowClient.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: db.DatabaseID.String(), // Using a stable ID functions as a locking mechanism
	}, s.workflows.UpdateDatabase, &workflows.UpdateDatabaseInput{
		Spec: spec,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow instance: %w", err)
	}

	return databaseToAPI(db), nil
}

func (s *Service) InspectDatabase(ctx context.Context, req *api.InspectDatabasePayload) (*api.Database, error) {
	return nil, ErrNotImplemented
}

func (s *Service) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.Database, error) {
	spec, err := apiToDatabaseSpec(req.DatabaseID, req.Request.TenantID, req.Request.Spec)
	if err != nil {
		return nil, api.MakeInvalidInput(err)
	}

	db, err := s.dbSvc.UpdateDatabase(ctx, database.DatabaseStateModifying, spec)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found", *req.DatabaseID))
	} else if errors.Is(err, database.ErrDatabaseNotModifiable) {
		return nil, api.MakeInvalidInput(fmt.Errorf("database %s is not modifiable", *req.DatabaseID))
	} else if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	var forceUpdate bool
	if req.ForceUpdate != nil {
		forceUpdate = *req.ForceUpdate
	}

	_, err = s.workflowClient.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: db.DatabaseID.String(), // Using a stable ID functions as a locking mechanism
	}, s.workflows.UpdateDatabase, &workflows.UpdateDatabaseInput{
		Spec:        spec,
		ForceUpdate: forceUpdate,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow instance: %w", err)
	}

	return databaseToAPI(db), nil
}

func (s *Service) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) error {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	if !database.DatabaseStateModifiable(db.State) {
		return api.MakeDatabaseNotModifiable(fmt.Errorf("database %s is not in a modifiable state", databaseID))
	}

	err = s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, db.State, database.DatabaseStateDeleting)
	if err != nil {
		return fmt.Errorf("failed to update database state: %w", err)
	}

	_, err = s.workflowClient.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: db.DatabaseID.String(), // Using a stable ID functions as a locking mechanism
	}, s.workflows.DeleteDatabase, &workflows.DeleteDatabaseInput{
		DatabaseID: db.DatabaseID,
	})
	if err != nil {
		return fmt.Errorf("failed to create workflow instance: %w", err)
	}

	return nil
}

func (s *Service) InitiateDatabaseBackup(ctx context.Context, req *api.InitiateDatabaseBackupPayload) (*api.Task, error) {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if !database.DatabaseStateModifiable(db.State) {
		return nil, api.MakeDatabaseNotModifiable(fmt.Errorf("database %s is not in a modifiable state", databaseID))
	}

	node, err := db.Spec.Node(req.NodeName)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid node name %q: %w", req.NodeName, err))
	}
	instances := make([]*workflows.InstanceHost, len(node.HostIDs))
	for i, hostID := range node.HostIDs {
		instances[i] = &workflows.InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, db.DatabaseID, node.Name),
			HostID:     hostID,
		}
	}

	t, err := task.NewTask(db.DatabaseID, task.TypeBackup)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}
	t.NodeName = node.Name
	err = s.taskSvc.CreateTask(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("failed to persist task: %w", err)
	}

	_, err = s.workflowClient.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: db.DatabaseID.String() + "-" + node.Name,
	}, s.workflows.CreatePgBackRestBackup, &workflows.CreatePgBackRestBackupInput{
		Task:      t,
		Instances: instances,
		Options: &pgbackrest.BackupOptions{
			Type:         pgbackrest.BackupType(req.Options.Type),
			Annotations:  req.Options.Annotations,
			ExtraOptions: req.Options.ExtraOptions,
		},
	})
	if err != nil {
		if errors.Is(err, backend.ErrInstanceAlreadyExists) {
			err = api.MakeBackupAlreadyInProgress(fmt.Errorf("a backup is already in progress for node %s", node.Name))
		} else {
			err = fmt.Errorf("failed to create workflow instance: %w", err)
		}
		if tErr := s.taskSvc.DeleteTask(ctx, db.DatabaseID, t.TaskID); tErr != nil {
			s.logger.Error().Err(tErr).Msg("failed to clean up task after workflow creation failure")
		}
		return nil, err
	}

	return taskToAPI(t), nil
}

func (s *Service) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) ([]*api.Task, error) {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}

	_, err = s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	options, err := taskListOptions(req)
	if err != nil {
		return nil, api.MakeInvalidInput(err)
	}

	tasks, err := s.taskSvc.GetTasks(ctx, databaseID, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	return tasksToAPI(tasks), nil
}

func (s *Service) InspectDatabaseTask(ctx context.Context, req *api.InspectDatabaseTaskPayload) (*api.Task, error) {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}
	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid task ID %q: %w", req.TaskID, err))
	}

	_, err = s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	t, err := s.taskSvc.GetTask(ctx, databaseID, taskID)
	if errors.Is(err, task.ErrTaskNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("task %s not found: %w", taskID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return taskToAPI(t), nil
}

func (s *Service) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error) {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}
	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid task ID %q: %w", req.TaskID, err))
	}

	_, err = s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	t, err := s.taskSvc.GetTask(ctx, databaseID, taskID)
	if errors.Is(err, task.ErrTaskNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("task %s not found: %w", taskID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	options, err := taskLogOptions(req)
	if err != nil {
		return nil, api.MakeInvalidInput(err)
	}

	log, err := s.taskSvc.GetTaskLog(ctx, databaseID, taskID, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get task log: %w", err)
	}

	return taskLogToAPI(log, t.Status), nil
}

func (s *Service) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (res *api.RestoreDatabaseResponse, err error) {
	databaseID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid database ID %q: %w", req.DatabaseID, err))
	}
	restoreConfig, err := apiToRestoreConfig(req.Request.RestoreConfig)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid restore config: %w", err))
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, api.MakeNotFound(fmt.Errorf("database %s not found: %w", databaseID, err))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if !database.DatabaseStateModifiable(db.State) {
		return nil, api.MakeDatabaseNotModifiable(fmt.Errorf("database %s is not in a modifiable state", databaseID))
	}

	targetNodes := req.Request.TargetNodes
	if len(targetNodes) == 0 {
		targetNodes = db.Spec.NodeNames()
	}

	existingNodes := ds.NewSet(db.Spec.NodeNames()...)
	requestedNodes := ds.NewSet(targetNodes...)

	if diff := requestedNodes.Difference(existingNodes); diff.Size() > 0 {
		return nil, api.MakeInvalidInput(fmt.Errorf("invalid target nodes %v: %w", diff.ToSlice(), err))
	}

	var cleanupTasks []func() error
	handleError := func(cause error) error {
		for _, cleanup := range cleanupTasks {
			if err := cleanup(); err != nil {
				s.logger.Error().Err(err).Msg("failed cleanup task after failing to start restore")
			}
		}
		return cause
	}

	// Remove backup configuration from nodes that are being restored and
	// persist the updated spec.
	db.Spec.RemoveBackupConfigFrom(targetNodes...)
	db, err = s.dbSvc.UpdateDatabase(ctx, database.DatabaseStateRestoring, db.Spec)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to persist db spec updates: %w", err))
	}
	cleanupTasks = append(cleanupTasks, func() error {
		return s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateRestoring, db.State)
	})

	nodeTaskIDs := map[string]uuid.UUID{}
	tasks := make([]*task.Task, len(targetNodes))
	for i, node := range targetNodes {
		t, err := task.NewTask(db.DatabaseID, task.TypeRestore)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to create task: %w", err))
		}
		t.NodeName = node
		err = s.taskSvc.CreateTask(ctx, t)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to persist task: %w", err))
		}
		nodeTaskIDs[node] = t.TaskID
		cleanupTasks = append(cleanupTasks, func() error {
			return s.taskSvc.DeleteTask(ctx, db.DatabaseID, t.TaskID)
		})
		tasks[i] = t
	}

	_, err = s.workflowClient.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: db.DatabaseID.String(), // Using a stable ID functions as a locking mechanism
	}, s.workflows.PgBackRestRestore, &workflows.PgBackRestRestoreInput{
		Spec:          db.Spec,
		TargetNodes:   targetNodes,
		RestoreConfig: restoreConfig,
		NodeTaskIDs:   nodeTaskIDs,
	})
	if err != nil {
		if errors.Is(err, backend.ErrInstanceAlreadyExists) {
			err = api.MakeBackupAlreadyInProgress(fmt.Errorf("an operation is already in progress for this database"))
		} else {
			err = fmt.Errorf("failed to create workflow instance: %w", err)
		}
		return nil, handleError(err)
	}

	return &api.RestoreDatabaseResponse{
		Database: databaseToAPI(db),
		Tasks:    tasksToAPI(tasks),
	}, nil
}

func (s *Service) InitCluster(ctx context.Context) (*api.ClusterJoinToken, error) {
	return nil, ErrAlreadyInitialized
}

func (s *Service) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	return ErrAlreadyInitialized
}
