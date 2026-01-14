package client

import (
	"context"
	"errors"
	"time"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
)

var _ Client = (*SingleServerClient)(nil)

var ErrInvalidServerConfig = errors.New("server configuration is empty")

const taskPollInterval = 500 * time.Millisecond

var taskEnded = map[string]bool{
	TaskStatusCompleted: true,
	TaskStatusCanceled:  true,
	TaskStatusFailed:    true,
}

type SingleServerClient struct {
	api *api.Client
}

type ServerConfig struct {
	hostID string
	http   *HTTPServerConfig
	mqtt   *MQTTServerConfig
}

func NewSingleServerClient(server ServerConfig) (*SingleServerClient, error) {
	var cli *client.Client
	switch {
	case server.http != nil:
		cli = server.http.newClient()
	case server.mqtt != nil:
		var err error
		cli, err = server.mqtt.newClient()
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidServerConfig
	}
	return &SingleServerClient{
		api: &api.Client{
			InitClusterEndpoint:            cli.InitCluster(),
			JoinClusterEndpoint:            cli.JoinCluster(),
			GetJoinTokenEndpoint:           cli.GetJoinToken(),
			GetClusterEndpoint:             cli.GetCluster(),
			ListHostsEndpoint:              cli.ListHosts(),
			GetHostEndpoint:                cli.GetHost(),
			RemoveHostEndpoint:             cli.RemoveHost(),
			ListDatabasesEndpoint:          cli.ListDatabases(),
			CreateDatabaseEndpoint:         cli.CreateDatabase(),
			GetDatabaseEndpoint:            cli.GetDatabase(),
			UpdateDatabaseEndpoint:         cli.UpdateDatabase(),
			DeleteDatabaseEndpoint:         cli.DeleteDatabase(),
			BackupDatabaseNodeEndpoint:     cli.BackupDatabaseNode(),
			ListDatabaseTasksEndpoint:      cli.ListDatabaseTasks(),
			GetDatabaseTaskEndpoint:        cli.GetDatabaseTask(),
			GetDatabaseTaskLogEndpoint:     cli.GetDatabaseTaskLog(),
			RestoreDatabaseEndpoint:        cli.RestoreDatabase(),
			GetVersionEndpoint:             cli.GetVersion(),
			RestartInstanceEndpoint:        cli.RestartInstance(),
			CancelDatabaseTaskEndpoint:     cli.CancelDatabaseTask(),
			SwitchoverDatabaseNodeEndpoint: cli.SwitchoverDatabaseNode(),
			FailoverDatabaseNodeEndpoint:   cli.FailoverDatabaseNode(),
			ListHostTasksEndpoint:          cli.ListHostTasks(),
			GetHostTaskEndpoint:            cli.GetHostTask(),
			GetHostTaskLogEndpoint:         cli.GetHostTaskLog(),
			ListTasksEndpoint:              cli.ListTasks(),
			StopInstanceEndpoint:           cli.StopInstance(),
			StartInstanceEndpoint:          cli.StartInstance(),
		},
	}, nil
}

func (c *SingleServerClient) InitCluster(ctx context.Context, req *api.InitClusterRequest) (*api.ClusterJoinToken, error) {
	resp, err := c.api.InitCluster(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) JoinCluster(ctx context.Context, req *api.ClusterJoinToken) error {
	err := c.api.JoinCluster(ctx, req)
	return translateErr(err)
}

func (c *SingleServerClient) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	resp, err := c.api.GetJoinToken(ctx)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetCluster(ctx context.Context) (*api.Cluster, error) {
	resp, err := c.api.GetCluster(ctx)
	return resp, translateErr(err)
}

func (c *SingleServerClient) ListHosts(ctx context.Context) (*api.ListHostsResponse, error) {
	resp, err := c.api.ListHosts(ctx)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error) {
	resp, err := c.api.GetHost(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) (*api.RemoveHostResponse, error) {
	resp, err := c.api.RemoveHost(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) ListDatabases(ctx context.Context) (*api.ListDatabasesResponse, error) {
	resp, err := c.api.ListDatabases(ctx)
	return resp, translateErr(err)
}

func (c *SingleServerClient) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.CreateDatabaseResponse, error) {
	resp, err := c.api.CreateDatabase(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetDatabase(ctx context.Context, req *api.GetDatabasePayload) (*api.Database, error) {
	resp, err := c.api.GetDatabase(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.UpdateDatabaseResponse, error) {
	resp, err := c.api.UpdateDatabase(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (*api.DeleteDatabaseResponse, error) {
	resp, err := c.api.DeleteDatabase(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) BackupDatabaseNode(ctx context.Context, req *api.BackupDatabaseNodePayload) (*api.BackupDatabaseNodeResponse, error) {
	resp, err := c.api.BackupDatabaseNode(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) ListTasks(ctx context.Context, req *api.ListTasksPayload) (*api.ListTasksResponse, error) {
	resp, err := c.api.ListTasks(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) (*api.ListDatabaseTasksResponse, error) {
	resp, err := c.api.ListDatabaseTasks(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error) {
	resp, err := c.api.GetDatabaseTask(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error) {
	resp, err := c.api.GetDatabaseTaskLog(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) ListHostTasks(ctx context.Context, req *api.ListHostTasksPayload) (*api.ListHostTasksResponse, error) {
	resp, err := c.api.ListHostTasks(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetHostTask(ctx context.Context, req *api.GetHostTaskPayload) (*api.Task, error) {
	resp, err := c.api.GetHostTask(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetHostTaskLog(ctx context.Context, req *api.GetHostTaskLogPayload) (*api.TaskLog, error) {
	resp, err := c.api.GetHostTaskLog(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (*api.RestoreDatabaseResponse, error) {
	resp, err := c.api.RestoreDatabase(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) GetVersion(ctx context.Context) (*api.VersionInfo, error) {
	resp, err := c.api.GetVersion(ctx)
	return resp, translateErr(err)
}

func (c *SingleServerClient) StopInstance(ctx context.Context, req *api.StopInstancePayload) (*api.StopInstanceResponse, error) {
	resp, err := c.api.StopInstance(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) StartInstance(ctx context.Context, req *api.StartInstancePayload) (*api.StartInstanceResponse, error) {
	resp, err := c.api.StartInstance(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (*api.RestartInstanceResponse, error) {
	resp, err := c.api.RestartInstance(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) WaitForDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error) {
	return c.waitForTask(ctx, func(ctx context.Context) (*api.Task, error) {
		return c.GetDatabaseTask(ctx, req)
	})
}

func (c *SingleServerClient) WaitForHostTask(ctx context.Context, req *api.GetHostTaskPayload) (*api.Task, error) {
	return c.waitForTask(ctx, func(ctx context.Context) (*api.Task, error) {
		return c.GetHostTask(ctx, req)
	})
}

func (c *SingleServerClient) waitForTask(ctx context.Context, getTask func(context.Context) (*api.Task, error)) (*api.Task, error) {
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	task, err := getTask(ctx)
	if err != nil {
		return nil, err
	}

	for !taskEnded[task.Status] {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			task, err = getTask(ctx)
			if err != nil {
				return nil, err
			}
		}
	}

	return task, nil
}

func (c *SingleServerClient) CancelDatabaseTask(ctx context.Context, req *api.CancelDatabaseTaskPayload) (*api.Task, error) {
	resp, err := c.api.CancelDatabaseTask(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) SwitchoverDatabaseNode(ctx context.Context, req *api.SwitchoverDatabaseNodePayload) (*api.SwitchoverDatabaseNodeResponse, error) {
	resp, err := c.api.SwitchoverDatabaseNode(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) FailoverDatabaseNode(ctx context.Context, req *api.FailoverDatabaseNodeRequest) (*api.FailoverDatabaseNodeResponse, error) {
	resp, err := c.api.FailoverDatabaseNode(ctx, req)
	return resp, translateErr(err)
}

func (c *SingleServerClient) FollowDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskLogPayload, handler func(e *api.TaskLogEntry)) error {
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	curr := &api.GetDatabaseTaskLogPayload{
		DatabaseID:   req.DatabaseID,
		TaskID:       req.TaskID,
		AfterEntryID: req.AfterEntryID,
		Limit:        req.Limit,
	}

	taskLog, err := c.GetDatabaseTaskLog(ctx, curr)
	if err != nil {
		return err
	}
	for _, entry := range taskLog.Entries {
		handler(entry)
	}

	for !taskEnded[taskLog.TaskStatus] {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			curr.AfterEntryID = taskLog.LastEntryID

			taskLog, err = c.GetDatabaseTaskLog(ctx, curr)
			if err != nil {
				return err
			}
			for _, entry := range taskLog.Entries {
				handler(entry)
			}
		}
	}

	return nil
}

func (c *SingleServerClient) FollowHostTask(ctx context.Context, req *api.GetHostTaskLogPayload, handler func(e *api.TaskLogEntry)) error {
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	curr := &api.GetHostTaskLogPayload{
		HostID:       req.HostID,
		TaskID:       req.TaskID,
		AfterEntryID: req.AfterEntryID,
		Limit:        req.Limit,
	}

	taskLog, err := c.GetHostTaskLog(ctx, curr)
	if err != nil {
		return err
	}
	for _, entry := range taskLog.Entries {
		handler(entry)
	}

	for !taskEnded[taskLog.TaskStatus] {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			curr.AfterEntryID = taskLog.LastEntryID

			taskLog, err = c.GetHostTaskLog(ctx, curr)
			if err != nil {
				return err
			}
			for _, entry := range taskLog.Entries {
				handler(entry)
			}
		}
	}

	return nil
}
