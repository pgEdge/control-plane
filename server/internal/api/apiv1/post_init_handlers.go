package apiv1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/cluster"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/version"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

var _ api.Service = (*PostInitHandlers)(nil)

type PostInitHandlers struct {
	cfg         config.Config
	logger      zerolog.Logger
	etcd        etcd.Etcd
	hostSvc     *host.Service
	dbSvc       *database.Service
	taskSvc     *task.Service
	workflowSvc *workflows.Service
	clusterSvc  *cluster.Service
}

func NewPostInitHandlers(
	cfg config.Config,
	logger zerolog.Logger,
	etcd etcd.Etcd,
	hostSvc *host.Service,
	dbSvc *database.Service,
	taskSvc *task.Service,
	workflowSvc *workflows.Service,
	clusterSvc *cluster.Service,
) *PostInitHandlers {
	return &PostInitHandlers{
		cfg:         cfg,
		logger:      logger,
		etcd:        etcd,
		hostSvc:     hostSvc,
		dbSvc:       dbSvc,
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		clusterSvc:  clusterSvc,
	}
}

func (s *PostInitHandlers) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, apiErr(err)
	}
	serverURL := GetServerURL(s.cfg)
	return &api.ClusterJoinToken{
		Token:     token,
		ServerURL: serverURL.String(),
	}, nil
}

func GetServerURL(cfg config.Config) url.URL {
	scheme := "http"
	if cfg.HTTP.ServerCert != "" && cfg.HTTP.ServerKey != "" {
		scheme = "https"
	}
	return url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", cfg.IPv4Address, cfg.HTTP.Port),
	}

}

func (s *PostInitHandlers) GetJoinOptions(ctx context.Context, req *api.ClusterJoinRequest) (*api.ClusterJoinOptions, error) {
	if err := s.etcd.VerifyJoinToken(req.Token); err != nil {
		return nil, apiErr(err)
	}

	hostID, err := hostIdentToString(req.HostID)
	if err != nil {
		return nil, apiErr(err)
	}

	creds, err := s.etcd.AddHost(ctx, etcd.HostCredentialOptions{
		HostID:              hostID,
		Hostname:            req.Hostname,
		IPv4Address:         req.Ipv4Address,
		EmbeddedEtcdEnabled: req.EmbeddedEtcdEnabled,
	})
	if err != nil {
		return nil, apiErr(err)
	}

	leader, err := s.etcd.Leader(ctx)
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.ClusterJoinOptions{
		Leader: &api.EtcdClusterMember{
			Name:       leader.Name,
			PeerUrls:   leader.PeerURLs,
			ClientUrls: leader.ClientURLs,
		},
		Credentials: &api.ClusterCredentials{
			Username:   creds.Username,
			Password:   creds.Password,
			CaCert:     base64.StdEncoding.EncodeToString(creds.CaCert),
			ClientCert: base64.StdEncoding.EncodeToString(creds.ClientCert),
			ClientKey:  base64.StdEncoding.EncodeToString(creds.ClientKey),
			ServerCert: base64.StdEncoding.EncodeToString(creds.ServerCert),
			ServerKey:  base64.StdEncoding.EncodeToString(creds.ServerKey),
		},
	}, nil
}

func (s *PostInitHandlers) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrNotImplemented
}

func (s *PostInitHandlers) GetCluster(ctx context.Context) (*api.Cluster, error) {
	hosts, err := s.hostSvc.GetAllHosts(ctx)
	if err != nil {
		return nil, apiErr(err)
	}
	apiHosts := make([]*api.Host, len(hosts))
	for idx, h := range hosts {
		apiHosts[idx] = hostToAPI(h)
	}

	storedCluster, err := s.clusterSvc.Get(ctx)
	if err != nil {
		return nil, apiErr(err)
	}
	cluster := &api.Cluster{
		ID:       api.Identifier(storedCluster.ID),
		TenantID: api.Identifier(s.cfg.TenantID),
		Hosts:    apiHosts,
		Status:   &api.ClusterStatus{State: "available"},
	}

	return cluster, nil
}

func (s *PostInitHandlers) ListHosts(ctx context.Context) (*api.ListHostsResponse, error) {
	hosts, err := s.hostSvc.GetAllHosts(ctx)
	if err != nil {
		return nil, apiErr(err)
	}
	apiHosts := make([]*api.Host, len(hosts))

	for idx, h := range hosts {
		apiHosts[idx] = hostToAPI(h)
	}
	return &api.ListHostsResponse{Hosts: apiHosts}, nil
}

func (s *PostInitHandlers) GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error) {
	hostID, err := hostIdentToString(req.HostID)
	if err != nil {
		return nil, apiErr(err)
	}
	host, err := s.hostSvc.GetHost(ctx, hostID)
	if err != nil {
		return nil, apiErr(err)
	}
	return hostToAPI(host), nil
}

func (s *PostInitHandlers) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	hostID, err := hostIdentToString(req.HostID)
	if err != nil {
		return apiErr(err)
	}
	if hostID == s.cfg.HostID {
		return makeInvalidInputErr(errors.New("a host cannot remove itself from the cluster"))
	}
	_, err = s.hostSvc.GetHost(ctx, hostID)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrHostNotFound
	} else if err != nil {
		return apiErr(err)
	}
	count, err := s.dbSvc.InstanceCountForHost(ctx, hostID)
	if err != nil {
		return apiErr(err)
	}
	if count != 0 {
		return makeInvalidInputErr(errors.New("cannot remove host with running instances"))
	}
	err = s.etcd.RemoveHost(ctx, hostID)
	if err != nil {
		return apiErr(err)
	}
	err = s.hostSvc.RemoveHost(ctx, hostID)
	if err != nil {
		return apiErr(err)
	}

	return nil
}

// ListDatabases fetches all databases from the database service and converts them to API format.
func (s *PostInitHandlers) ListDatabases(ctx context.Context) (*api.ListDatabasesResponse, error) {
	// Fetch databases from the database service
	databases, err := s.dbSvc.GetDatabases(ctx)
	if err != nil {
		return nil, apiErr(err)
	}

	// Ensure we return an empty (non-nil) slice if no databases found
	if len(databases) == 0 {
		return &api.ListDatabasesResponse{
			Databases: []*api.Database{},
		}, nil
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

	return &api.ListDatabasesResponse{
		Databases: apiDatabases,
	}, nil
}

func (s *PostInitHandlers) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.CreateDatabaseResponse, error) {
	spec, err := apiToDatabaseSpec(req.ID, req.TenantID, req.Spec)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	err = s.dbSvc.PopulateSpecDefaults(ctx, spec)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("failed to validate database spec: %w", err))
	}

	// Full validation on create (no PreviousSpec, not delta mode).
	input := &workflows.ValidateSpecInput{
		DatabaseID:   spec.DatabaseID,
		Spec:         spec,
		PreviousSpec: nil,
	}
	output, err := s.workflowSvc.ValidateSpec(ctx, input)
	if err != nil {
		return nil, apiErr(err)
	}
	if !output.Valid {
		return nil, makeInvalidInputErr(errors.New(strings.Join(output.Errors, "\n")))
	}

	db, err := s.dbSvc.CreateDatabase(ctx, spec)
	if err != nil {
		return nil, apiErr(err)
	}

	t, err := s.workflowSvc.CreateDatabase(ctx, spec)
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.CreateDatabaseResponse{
		Database: databaseToAPI(db),
		Task:     taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) GetDatabase(ctx context.Context, req *api.GetDatabasePayload) (*api.Database, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	return databaseToAPI(db), nil
}

func (s *PostInitHandlers) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.UpdateDatabaseResponse, error) {
	spec, err := apiToDatabaseSpec(&req.DatabaseID, req.Request.TenantID, req.Request.Spec)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	existing, err := s.dbSvc.GetDatabase(ctx, spec.DatabaseID)
	if err != nil {
		return nil, apiErr(err)
	}
	// API-level update validation:
	// ensure that for any newly added nodes, source_node (if set) refers
	// only to nodes that exist in the old spec.
	if err := validateDatabaseUpdate(existing.Spec, req.Request.Spec); err != nil {
		return nil, makeInvalidInputErr(err)
	}
	// Copy optional fields from the previous spec to the current spec if they
	// are unset.
	spec.DefaultOptionalFieldsFrom(existing.Spec)

	err = s.dbSvc.PopulateSpecDefaults(ctx, spec)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("failed to validate database spec: %w", err))
	}

	input := &workflows.ValidateSpecInput{
		DatabaseID:   spec.DatabaseID,
		Spec:         spec,
		PreviousSpec: existing.Spec,
	}
	output, err := s.workflowSvc.ValidateSpec(ctx, input)
	if err != nil {
		return nil, apiErr(err)
	}
	if !output.Valid {
		return nil, makeInvalidInputErr(errors.New(strings.Join(output.Errors, "\n")))
	}

	db, err := s.dbSvc.UpdateDatabase(ctx, database.DatabaseStateModifying, spec)
	if err != nil {
		return nil, apiErr(err)
	}

	prevState := db.State
	t, err := s.workflowSvc.UpdateDatabase(ctx, spec, req.ForceUpdate)
	if err != nil {
		restorationErr := s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, db.State, prevState)
		if restorationErr != nil {
			s.logger.Err(restorationErr).Msg("failed to roll back database state change")
		}
		return nil, apiErr(err)
	}

	return &api.UpdateDatabaseResponse{
		Database: databaseToAPI(db),
		Task:     taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (*api.DeleteDatabaseResponse, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}
	if !req.Force && !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	prevState := db.State
	err = s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, prevState, database.DatabaseStateDeleting)
	if err != nil {
		return nil, apiErr(err)
	}

	t, err := s.workflowSvc.DeleteDatabase(ctx, databaseID)

	if err != nil {
		restorationErr := s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateDeleting, prevState)
		if restorationErr != nil {
			s.logger.Err(restorationErr).Msg("failed to roll back database state change")
		}
		return nil, apiErr(err)
	}

	return &api.DeleteDatabaseResponse{
		Task: taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) BackupDatabaseNode(ctx context.Context, req *api.BackupDatabaseNodePayload) (*api.BackupDatabaseNodeResponse, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	_, err = IsNodeAvailable(db, req.NodeName)
	if err != nil {
		return nil, err
	}

	if !req.Force && !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	node, err := db.Spec.Node(req.NodeName)
	if err != nil {
		return nil, apiErr(err)
	}

	if err := validateBackupOptions(req.Options); err != nil {
		return nil, apiErr(err)
	}

	instances := make([]*workflows.InstanceHost, len(node.HostIDs))
	for i, hostID := range node.HostIDs {
		instances[i] = &workflows.InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, db.DatabaseID, node.Name),
			HostID:     hostID,
		}
	}
	prevState := db.State
	t, err := s.workflowSvc.CreatePgBackRestBackup(ctx,
		db.DatabaseID,
		node.Name,
		false,
		instances,
		&pgbackrest.BackupOptions{
			Type:          pgbackrest.BackupType(req.Options.Type),
			Annotations:   req.Options.Annotations,
			BackupOptions: req.Options.BackupOptions,
		},
	)
	if err != nil {
		restorationErr := s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateBackingUp, prevState)
		if restorationErr != nil {
			s.logger.Err(restorationErr).Msg("failed to roll back database state change")
		}
		return nil, apiErr(err)
	}

	return &api.BackupDatabaseNodeResponse{
		Task: taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) SwitchoverDatabaseNode(ctx context.Context, req *api.SwitchoverDatabaseNodePayload) (*api.SwitchoverDatabaseNodeResponse, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	if !hasPrimaryInstance(db.Instances) {
		return nil, ErrNoPrimaryInstance
	}

	if !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	node, err := db.Spec.Node(req.NodeName)
	if err != nil {
		return nil, apiErr(err)
	}

	instances := make([]*activities.InstanceHost, len(node.HostIDs))
	for i, hostID := range node.HostIDs {
		instances[i] = &activities.InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, db.DatabaseID, node.Name),
			HostID:     hostID,
		}
	}

	input := &workflows.SwitchoverInput{
		DatabaseID: databaseID,
		NodeName:   req.NodeName,
		Instances:  instances,
	}

	if req.CandidateInstanceID != nil && string(*req.CandidateInstanceID) != "" {
		cand := string(*req.CandidateInstanceID)
		storedInst, err := s.dbSvc.GetInstance(ctx, databaseID, cand)
		if err != nil {
			return nil, apiErr(err)
		}

		if storedInst.NodeName != req.NodeName {
			return nil, makeInvalidInputErr(fmt.Errorf("candidate instance %s does not belong to node %s", cand, req.NodeName))
		}

		if storedInst.State != database.InstanceStateAvailable {
			return nil, makeInvalidInputErr(fmt.Errorf("candidate instance %s is not available (state=%s)", cand, storedInst.State))
		}

		input.CandidateInstanceID = cand
	}

	if req.ScheduledAt != nil && *req.ScheduledAt != "" {
		tm, err := time.Parse(time.RFC3339, *req.ScheduledAt)
		if err != nil {
			return nil, makeInvalidInputErr(fmt.Errorf("invalid scheduled_at value: %w", err))
		}
		now := time.Now().UTC()
		if !tm.After(now) {
			return nil, makeInvalidInputErr(fmt.Errorf("scheduled_at must be in the future"))
		}
		input.ScheduledAt = tm.UTC()

		s.logger.Info().
			Str("database_id", databaseID).
			Str("scheduled_at", input.ScheduledAt.String()).
			Msg("parsed scheduled_at")
	}

	opts := task.TaskListOptions{
		Type:     task.TypeSwitchover,
		NodeName: string(req.NodeName),
		Statuses: []task.Status{
			task.StatusPending,
			task.StatusRunning,
			task.StatusCanceling,
		},
		Limit: 1,
	}

	activeTasks, err := s.taskSvc.GetTasks(ctx, databaseID, opts)
	if err != nil {
		return nil, apiErr(err)
	}
	if len(activeTasks) > 0 {
		return nil, apiErr(fmt.Errorf("switchover already in progress for database %s node %s", databaseID, req.NodeName))
	}

	t, err := s.workflowSvc.SwitchoverDatabaseNode(ctx, input)
	if err != nil {
		return nil, apiErr(fmt.Errorf("failed to start switchover workflow: %w", err))
	}

	s.logger.Info().
		Str("database_id", databaseID).
		Str("node_name", req.NodeName).
		Str("task_id", t.TaskID.String()).
		Msg("switchover workflow initiated")

	return &api.SwitchoverDatabaseNodeResponse{
		Task: taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) FailoverDatabaseNode(ctx context.Context, req *api.FailoverDatabaseNodeRequest) (*api.FailoverDatabaseNodeResponse, error) {

	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	if !hasPrimaryInstance(db.Instances) {
		return nil, ErrNoPrimaryInstance
	}

	if !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	node, err := db.Spec.Node(req.NodeName)
	if err != nil {
		return nil, apiErr(err)
	}

	instances := make([]*activities.InstanceHost, len(node.HostIDs))
	for i, hostID := range node.HostIDs {
		instances[i] = &activities.InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, db.DatabaseID, node.Name),
			HostID:     hostID,
		}
	}

	input := &workflows.FailoverInput{
		DatabaseID:     databaseID,
		NodeName:       req.NodeName,
		Instances:      instances,
		SkipValidation: req.SkipValidation,
	}

	if req.CandidateInstanceID != nil && string(*req.CandidateInstanceID) != "" {
		cand := string(*req.CandidateInstanceID)
		storedInst, err := s.dbSvc.GetInstance(ctx, databaseID, cand)
		if err != nil {
			return nil, apiErr(err)
		}

		if storedInst.NodeName != req.NodeName {
			return nil, makeInvalidInputErr(fmt.Errorf("candidate instance %s does not belong to node %s", cand, req.NodeName))
		}

		if storedInst.State != database.InstanceStateAvailable {
			return nil, makeInvalidInputErr(fmt.Errorf("candidate instance %s is not available (state=%s)", cand, storedInst.State))
		}

		input.CandidateInstanceID = cand
	}

	opts := task.TaskListOptions{
		Type:     task.TypeFailover,
		NodeName: string(req.NodeName),
		Statuses: []task.Status{
			task.StatusPending,
			task.StatusRunning,
			task.StatusCanceling,
		},
		Limit: 1,
	}

	activeTasks, err := s.taskSvc.GetTasks(ctx, databaseID, opts)
	if err != nil {
		return nil, apiErr(err)
	}
	if len(activeTasks) > 0 {
		return nil, apiErr(fmt.Errorf("failover already in progress for database %s node %s", databaseID, req.NodeName))
	}

	t, err := s.workflowSvc.FailoverDatabaseNode(ctx, input)
	if err != nil {
		// map errors to API errors similar to other handlers, e.g. operation already in progress etc.
		return nil, apiErr(err)
	}

	resp := &api.FailoverDatabaseNodeResponse{
		Task: taskToAPI(t),
	}
	return resp, nil
}

func (s *PostInitHandlers) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) (*api.ListDatabaseTasksResponse, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}

	options, err := taskListOptions(req)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	tasks, err := s.taskSvc.GetTasks(ctx, databaseID, options)
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.ListDatabaseTasksResponse{
		Tasks: tasksToAPI(tasks),
	}, nil
}

func (s *PostInitHandlers) GetDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}
	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		return nil, ErrInvalidTaskID
	}

	t, err := s.taskSvc.GetTask(ctx, databaseID, taskID)
	if err != nil {
		return nil, apiErr(err)
	}

	return taskToAPI(t), nil
}

func (s *PostInitHandlers) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}
	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		return nil, ErrInvalidTaskID
	}

	t, err := s.taskSvc.GetTask(ctx, databaseID, taskID)
	if err != nil {
		return nil, apiErr(err)
	}

	options, err := taskLogOptions(req)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	log, err := s.taskSvc.GetTaskLog(ctx, databaseID, taskID, options)
	if err != nil {
		return nil, apiErr(err)
	}

	return taskLogToAPI(log, t.Status), nil
}

func (s *PostInitHandlers) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (res *api.RestoreDatabaseResponse, err error) {
	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, err
	}
	restoreConfig, err := apiToRestoreConfig(req.Request.RestoreConfig)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	if !req.Force && !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	targetNodes := req.Request.TargetNodes
	if len(targetNodes) == 0 {
		targetNodes = db.Spec.NodeNames()
	}

	err = db.Spec.ValidateNodeNames(targetNodes...)
	if err != nil {
		return nil, apiErr(err)
	}

	// Remove backup configuration from nodes that are being restored and
	// persist the updated spec.
	db.Spec.RemoveBackupConfigFrom(targetNodes...)

	err = s.dbSvc.PopulateSpecDefaults(ctx, db.Spec)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("failed to validate database spec: %w", err))
	}

	db, err = s.dbSvc.UpdateDatabase(ctx, database.DatabaseStateRestoring, db.Spec)
	if err != nil {
		return nil, apiErr(err)
	}

	handleError := func(cause error) error {
		err := s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateRestoring, db.State)
		if err != nil {
			s.logger.Err(err).Msg("failed to roll back database state change")
		}
		return apiErr(cause)
	}

	t, nodeTasks, err := s.workflowSvc.PgBackRestRestore(ctx, db.Spec, targetNodes, restoreConfig)
	if err != nil {
		return nil, handleError(err)
	}

	return &api.RestoreDatabaseResponse{
		Database:  databaseToAPI(db),
		Task:      taskToAPI(t),
		NodeTasks: tasksToAPI(nodeTasks),
	}, nil
}

func (s *PostInitHandlers) GetVersion(context.Context) (res *api.VersionInfo, err error) {
	info, err := version.GetInfo()
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.VersionInfo{
		Version:      info.Version,
		Revision:     info.Revision,
		RevisionTime: info.RevisionTime,
		Arch:         info.Arch,
	}, nil
}

func (s *PostInitHandlers) InitCluster(ctx context.Context, req *api.InitClusterRequest) (*api.ClusterJoinToken, error) {
	return nil, ErrAlreadyInitialized
}

func (s *PostInitHandlers) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	return ErrAlreadyInitialized
}

func (s *PostInitHandlers) RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (*api.RestartInstanceResponse, error) {
	if req == nil {
		return nil, makeInvalidInputErr(errors.New("request cannot be nil"))
	}
	databaseID, instanceID := string(req.DatabaseID), string(req.InstanceID)
	storedInstance, err := s.dbSvc.GetInstance(ctx, databaseID, instanceID)
	if err != nil {
		return nil, apiErr(err)
	}
	if storedInstance.State != database.InstanceStateAvailable {
		return nil, makeInvalidInputErr(fmt.Errorf("instance %s is not restartable, it is in %s state",
			req.InstanceID, storedInstance.State))
	}
	input := &workflows.RestartInstanceInput{
		HostID:     storedInstance.HostID,
		DatabaseID: databaseID,
		InstanceID: instanceID,
	}

	if req.RestartOptions != nil && req.RestartOptions.ScheduledAt != nil {
		scheduleTime, err := time.Parse(time.RFC3339, *req.RestartOptions.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("invalid scheduled_at value: %w", err)
		}
		input.ScheduledAt = scheduleTime
	}

	t, err := s.workflowSvc.RestartInstance(ctx, input)
	if err != nil {
		return nil, apiErr(fmt.Errorf("failed to start restart instance workflow: %w", err))
	}

	s.logger.Info().
		Str("database_id", string(req.DatabaseID)).
		Str("instance_id", string(req.InstanceID)).
		Str("task_id", t.TaskID.String()).
		Msg("restart instance workflow initiated")

	return &api.RestartInstanceResponse{Task: taskToAPI(t)}, nil
}

func (s *PostInitHandlers) StopInstance(ctx context.Context, req *api.StopInstancePayload) (*api.StopInstanceResponse, error) {
	if req == nil {
		return nil, makeInvalidInputErr(errors.New("request cannot be nil"))
	}

	databaseID := string(req.DatabaseID)
	instanceID := string(req.InstanceID)

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}
	if !req.Force && !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	storedInstance, err := s.dbSvc.GetInstance(ctx, databaseID, instanceID)
	if err != nil {
		return nil, apiErr(err)
	}

	if storedInstance.State != database.InstanceStateAvailable &&
		storedInstance.State != database.InstanceStateDegraded {
		return nil, apiErr(fmt.Errorf("instance %s is not stoppable, it is in %s state",
			req.InstanceID, storedInstance.State))
	}

	storedHost, err := s.hostSvc.GetHost(ctx, storedInstance.HostID)
	if err != nil {
		return nil, apiErr(err)
	}

	input := &workflows.StopInstanceInput{
		DatabaseID: databaseID,
		InstanceID: instanceID,
		HostID:     storedHost.ID,
		Cohort:     storedHost.Cohort,
	}

	t, err := s.workflowSvc.StopInstance(ctx, input)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("failed to start stop-instance workflow: %w", err))
	}

	s.logger.Info().
		Str("database_id", string(req.DatabaseID)).
		Str("instance_id", string(req.InstanceID)).
		Str("task_id", t.TaskID.String()).
		Msg("stop instance workflow initiated")

	return &api.StopInstanceResponse{Task: taskToAPI(t)}, nil
}

func (s *PostInitHandlers) StartInstance(ctx context.Context, req *api.StartInstancePayload) (*api.StartInstanceResponse, error) {
	if req == nil {
		return nil, makeInvalidInputErr(errors.New("request cannot be nil"))
	}

	databaseID := string(req.DatabaseID)
	instanceID := string(req.InstanceID)

	db, err := s.dbSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return nil, apiErr(err)
	}
	if !req.Force && !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	storedInstance, err := s.dbSvc.GetInstance(ctx, databaseID, instanceID)
	if err != nil {
		return nil, err
	}

	if storedInstance.State != database.InstanceStateUnknown && storedInstance.State != database.InstanceStateStopped {
		return nil, makeInvalidInputErr(fmt.Errorf("instance %s is not startable, it is in %s state",
			req.InstanceID, storedInstance.State))
	}

	storedHost, err := s.hostSvc.GetHost(ctx, storedInstance.HostID)
	if err != nil {
		return nil, apiErr(err)
	}

	input := &workflows.StartInstanceInput{
		DatabaseID: databaseID,
		InstanceID: instanceID,
		HostID:     storedHost.ID,
		Cohort:     storedHost.Cohort,
	}

	t, err := s.workflowSvc.StartInstance(ctx, input)
	if err != nil {
		return nil, apiErr(fmt.Errorf("failed to start start-instance workflow: %w", err))
	}

	s.logger.Info().
		Str("database_id", string(req.DatabaseID)).
		Str("instance_id", string(req.InstanceID)).
		Str("task_id", t.TaskID.String()).
		Msg("start instance workflow initiated")

	return &api.StartInstanceResponse{Task: taskToAPI(t)}, nil
}

func (s *PostInitHandlers) CancelDatabaseTask(ctx context.Context, req *api.CancelDatabaseTaskPayload) (*api.Task, error) {
	if req == nil {
		return nil, makeInvalidInputErr(errors.New("request cannot be nil"))
	}

	databaseID, err := dbIdentToString(req.DatabaseID)
	if err != nil {
		return nil, apiErr(err)
	}

	taskID, err := uuid.Parse(string(req.TaskID))
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("invalid task ID: %w", err))
	}

	t, err := s.taskSvc.GetTask(ctx, databaseID, taskID)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("task is not associated with database "))
	}

	if t.Status != task.StatusPending && t.Status != task.StatusRunning {
		return nil, makeInvalidInputErr(fmt.Errorf("task must be running or pending to be cancelled"))
	}
	t, err = s.workflowSvc.CancelDatabaseTask(ctx, databaseID, taskID)
	if err != nil {
		return nil, apiErr(fmt.Errorf("failed to cancel task: %w", err))
	}
	s.logger.Info().
		Str("database_id", databaseID).
		Str("task_id", taskID.String()).
		Msg("task cancellation initiated")

	return taskToAPI(t), nil
}

func IsNodeAvailable(db *database.Database, nodeName string) (bool, error) {
	for _, instance := range db.Instances {
		if instance.NodeName != nodeName {
			continue
		}
		if IsInstanceAvailable(instance) && instance.Status.IsPrimary() {
			return true, nil
		}
		return false, makeInvalidInputErr(fmt.Errorf(
			"node %s is in an unsupported state (%s)", instance.NodeName, instance.State))
	}
	return false, makeInvalidInputErr(fmt.Errorf("node %s not found", nodeName))
}

func IsInstanceAvailable(inst *database.Instance) bool {
	switch inst.State {
	case database.InstanceStateAvailable:
		return true
	default:
		// All other states are considered unavailable, including
		// InstanceStateUnknown and InstanceStateStopped.
		return false
	}
}

func hasPrimaryInstance(instances []*database.Instance) bool {
	for _, inst := range instances {
		if IsInstanceAvailable(inst) && inst.Status.IsPrimary() {
			return true
		}
	}
	return false
}
