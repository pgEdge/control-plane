package v1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	api "github.com/pgEdge/control-plane/api/v1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/version"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

var _ api.Service = (*PostInitHandlers)(nil)

type PostInitHandlers struct {
	cfg         config.Config
	logger      zerolog.Logger
	etcd        *etcd.EmbeddedEtcd
	hostSvc     *host.Service
	dbSvc       *database.Service
	taskSvc     *task.Service
	workflowSvc *workflows.Service
}

func NewPostInitHandlers(
	cfg config.Config,
	logger zerolog.Logger,
	etcd *etcd.EmbeddedEtcd,
	hostSvc *host.Service,
	dbSvc *database.Service,
	taskSvc *task.Service,
	workflowSvc *workflows.Service,
) *PostInitHandlers {
	return &PostInitHandlers{
		cfg:         cfg,
		logger:      logger,
		etcd:        etcd,
		hostSvc:     hostSvc,
		dbSvc:       dbSvc,
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
	}
}

func (s *PostInitHandlers) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, apiErr(err)
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

func (s *PostInitHandlers) GetJoinOptions(ctx context.Context, req *api.ClusterJoinRequest) (*api.ClusterJoinOptions, error) {
	if err := s.etcd.VerifyJoinToken(req.Token); err != nil {
		return nil, apiErr(err)
	}

	hostID, err := hostIdentToString(req.HostID)
	if err != nil {
		return nil, apiErr(err)
	}

	creds, err := s.etcd.AddPeerUser(ctx, etcd.HostCredentialOptions{
		HostID:      hostID,
		Hostname:    req.Hostname,
		IPv4Address: req.Ipv4Address,
	})
	if err != nil {
		return nil, apiErr(err)
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

func (s *PostInitHandlers) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrNotImplemented
}

func (s *PostInitHandlers) GetCluster(ctx context.Context) (*api.Cluster, error) {
	return nil, ErrNotImplemented
}

func (s *PostInitHandlers) ListHosts(ctx context.Context) ([]*api.Host, error) {
	hosts, err := s.hostSvc.GetAllHosts(ctx)
	if err != nil {
		return nil, apiErr(err)
	}
	apiHosts := make([]*api.Host, len(hosts))

	for idx, h := range hosts {
		apiHosts[idx] = hostToAPI(h)
	}
	return apiHosts, nil
}

func (s *PostInitHandlers) GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error) {
	return nil, ErrNotImplemented
}

func (s *PostInitHandlers) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	return ErrNotImplemented
}

// ListDatabases fetches all databases from the database service and converts them to API format.
func (s *PostInitHandlers) ListDatabases(ctx context.Context) (api.DatabaseCollection, error) {
	// Fetch databases from the database service
	databases, err := s.dbSvc.GetDatabases(ctx)
	if err != nil {
		return nil, apiErr(err)
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

func (s *PostInitHandlers) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.CreateDatabaseResponse, error) {
	spec, err := apiToDatabaseSpec(req.ID, req.TenantID, req.Spec)
	if err != nil {
		return nil, makeInvalidInputErr(err)
	}

	err = s.dbSvc.PopulateSpecDefaults(ctx, spec)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("failed to validate database spec: %w", err))
	}

	err = s.ValidateSpec(ctx, spec)
	if err != nil {
		return nil, makeInvalidInputErr(fmt.Errorf("%w", err))
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

	err = s.dbSvc.PopulateSpecDefaults(ctx, spec)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("failed to validate database spec: %w", err))
	}

	err = s.ValidateSpec(ctx, spec)
	if err != nil {
		return nil, api.MakeInvalidInput(fmt.Errorf("%w", err))
	}

	db, err := s.dbSvc.UpdateDatabase(ctx, database.DatabaseStateModifying, spec)
	if err != nil {
		return nil, apiErr(err)
	}

	t, err := s.workflowSvc.UpdateDatabase(ctx, spec, req.ForceUpdate)
	if err != nil {
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
	if !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	err = s.dbSvc.UpdateDatabaseState(ctx, db.DatabaseID, db.State, database.DatabaseStateDeleting)
	if err != nil {
		return nil, apiErr(err)
	}

	t, err := s.workflowSvc.DeleteDatabase(ctx, databaseID)
	if err != nil {
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
	if !database.DatabaseStateModifiable(db.State) {
		return nil, ErrDatabaseNotModifiable
	}

	node, err := db.Spec.Node(req.NodeName)
	if err != nil {
		return nil, apiErr(err)
	}
	instances := make([]*workflows.InstanceHost, len(node.HostIDs))
	for i, hostID := range node.HostIDs {
		instances[i] = &workflows.InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, db.DatabaseID, node.Name),
			HostID:     hostID,
		}
	}

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
		return nil, apiErr(err)
	}

	return &api.BackupDatabaseNodeResponse{
		Task: taskToAPI(t),
	}, nil
}

func (s *PostInitHandlers) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) ([]*api.Task, error) {
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

	return tasksToAPI(tasks), nil
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
	if !database.DatabaseStateModifiable(db.State) {
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

func (s *PostInitHandlers) InitCluster(ctx context.Context) (*api.ClusterJoinToken, error) {
	return nil, ErrAlreadyInitialized
}

func (s *PostInitHandlers) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	return ErrAlreadyInitialized
}

func (s *PostInitHandlers) ValidateSpec(ctx context.Context, spec *database.Spec) error {
	if spec == nil {
		return errors.New("spec cannot be nil")
	}

	output := s.workflowSvc.ValidateSpec(ctx, spec)
	if output == nil {
		return errors.New("failed to validate spec")

	}
	if !output.Valid {
		return fmt.Errorf(
			"spec validation failed. Please ensure all required fields in the provided spec are valid.\nDetails: %s",
			strings.Join(output.Errors, " "),
		)
	}
	s.logger.Info().Msg("Spec validation succeeded")

	return nil
}
