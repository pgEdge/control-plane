package client

import (
	"context"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

type Client interface {
	// API methods

	InitCluster(ctx context.Context) (*api.ClusterJoinToken, error)
	JoinCluster(ctx context.Context, req *api.ClusterJoinToken) error
	GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error)
	GetCluster(ctx context.Context) (*api.Cluster, error)
	ListHosts(ctx context.Context) ([]*api.Host, error)
	GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error)
	RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error
	ListDatabases(ctx context.Context) (*api.ListDatabasesResponse, error)
	CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.CreateDatabaseResponse, error)
	GetDatabase(ctx context.Context, req *api.GetDatabasePayload) (*api.Database, error)
	UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.UpdateDatabaseResponse, error)
	DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (*api.DeleteDatabaseResponse, error)
	BackupDatabaseNode(ctx context.Context, req *api.BackupDatabaseNodePayload) (*api.BackupDatabaseNodeResponse, error)
	ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) (*api.ListDatabaseTasksResponse, error)
	GetDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error)
	GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error)
	RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (*api.RestoreDatabaseResponse, error)
	GetVersion(ctx context.Context) (*api.VersionInfo, error)
	RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (*api.Task, error)
	CancelDatabaseTask(ctx context.Context, req *api.CancelDatabaseTaskPayload) (res *api.Task, err error)
	SwitchoverDatabaseNode(ctx context.Context, req *api.SwitchoverDatabaseNodePayload) (*api.SwitchoverDatabaseNodeResponse, error)
	FailoverDatabaseNode(ctx context.Context, req *api.FailoverDatabaseNodeRequest) (*api.FailoverDatabaseNodeResponse, error)
	// Helper methods

	WaitForTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error)
	FollowTask(ctx context.Context, req *api.GetDatabaseTaskLogPayload, handler func(e *api.TaskLogEntry)) error
}
