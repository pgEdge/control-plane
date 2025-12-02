package client

import (
	"context"
	"errors"
	"fmt"
	"slices"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

var ErrNoServers = errors.New("no servers provided")
var ErrHostNotFound = errors.New("host not found")
var ErrNoHealthyServers = errors.New("no healthy servers found")

var _ Client = (*MultiServerClient)(nil)

type MultiServerClient struct {
	servers map[string]*SingleServerClient
}

func NewMultiServerClient(servers ...ServerConfig) (*MultiServerClient, error) {
	if len(servers) == 0 {
		return nil, ErrNoServers
	}

	c := &MultiServerClient{
		servers: make(map[string]*SingleServerClient, len(servers)),
	}

	for i, server := range servers {
		s, err := NewSingleServerClient(server)
		if err != nil {
			return nil, fmt.Errorf("failed to create client for server at index %d: %w", i, err)
		}
		c.servers[server.hostID] = s
	}

	return c, nil
}

func (c *MultiServerClient) Server(hostID string) (*SingleServerClient, error) {
	server, ok := c.servers[hostID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrHostNotFound, hostID)
	}
	return server, nil
}

func (c *MultiServerClient) InitCluster(ctx context.Context, req *api.InitClusterRequest) (res *api.ClusterJoinToken, err error) {
	var uninitialized []*SingleServerClient
	var joinToken *api.ClusterJoinToken

	for hostID, server := range c.servers {
		tok, err := server.GetJoinToken(ctx)
		switch {
		case errors.Is(err, ErrClusterNotInitialized):
			uninitialized = append(uninitialized, server)
		case err != nil:
			return nil, fmt.Errorf("unexpected error from server %s: %w", hostID, err)
		default:
			joinToken = tok
		}
	}

	if len(uninitialized) == 0 {
		return joinToken, nil
	}

	if joinToken == nil {
		for i, server := range uninitialized {
			tok, err := server.InitCluster(ctx, req)
			if errors.Is(err, ErrOperationNotSupported) {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("failed to initialize cluster: %w", err)
			}

			joinToken = tok
			uninitialized = slices.Delete(uninitialized, i, i+1)
			break
		}
	}

	for _, server := range uninitialized {
		err := server.JoinCluster(ctx, joinToken)
		if err != nil {
			return nil, fmt.Errorf("failed to join host to cluster: %w", err)
		}
	}

	return joinToken, nil
}

func (c *MultiServerClient) JoinCluster(ctx context.Context, req *api.ClusterJoinToken) (err error) {
	for hostID, server := range c.servers {
		err := server.JoinCluster(ctx, req)
		if err != nil && !errors.Is(err, ErrClusterAlreadyInitialized) {
			return fmt.Errorf("unexpected error from server %s: %w", hostID, err)
		}
	}

	return nil
}

func (c *MultiServerClient) GetJoinToken(ctx context.Context) (res *api.ClusterJoinToken, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetJoinToken(ctx)
}

func (c *MultiServerClient) GetCluster(ctx context.Context) (res *api.Cluster, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetCluster(ctx)
}

func (c *MultiServerClient) ListHosts(ctx context.Context) (res *api.ListHostsResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.ListHosts(ctx)
}

func (c *MultiServerClient) GetHost(ctx context.Context, req *api.GetHostPayload) (res *api.Host, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetHost(ctx, req)
}

func (c *MultiServerClient) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) (*api.RemoveHostResponse, error) {
	for hostID, server := range c.servers {
		// Try to find a server other than the one we're trying to remove.
		if hostID == string(req.HostID) {
			continue
		}
		// Check liveness
		_, err := server.GetVersion(ctx)
		if err != nil {
			continue
		}

		return server.RemoveHost(ctx, req)
	}

	// Fallback to attempting from any live server so that the user gets the
	// server-generated error message from trying to remove a host from itself.
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.RemoveHost(ctx, req)
}

func (c *MultiServerClient) ListDatabases(ctx context.Context) (res *api.ListDatabasesResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.ListDatabases(ctx)
}

func (c *MultiServerClient) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (res *api.CreateDatabaseResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.CreateDatabase(ctx, req)
}

func (c *MultiServerClient) GetDatabase(ctx context.Context, req *api.GetDatabasePayload) (res *api.Database, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetDatabase(ctx, req)
}

func (c *MultiServerClient) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (res *api.UpdateDatabaseResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.UpdateDatabase(ctx, req)
}

func (c *MultiServerClient) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (res *api.DeleteDatabaseResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.DeleteDatabase(ctx, req)
}

func (c *MultiServerClient) BackupDatabaseNode(ctx context.Context, req *api.BackupDatabaseNodePayload) (res *api.BackupDatabaseNodeResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.BackupDatabaseNode(ctx, req)
}

func (c *MultiServerClient) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) (res *api.ListDatabaseTasksResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.ListDatabaseTasks(ctx, req)
}

func (c *MultiServerClient) GetDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (res *api.Task, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetDatabaseTask(ctx, req)
}

func (c *MultiServerClient) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (res *api.TaskLog, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetDatabaseTaskLog(ctx, req)
}

func (c *MultiServerClient) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (res *api.RestoreDatabaseResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.RestoreDatabase(ctx, req)
}

func (c *MultiServerClient) GetVersion(ctx context.Context) (res *api.VersionInfo, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.GetVersion(ctx)
}

func (c *MultiServerClient) RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (res *api.RestartInstanceResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.RestartInstance(ctx, req)
}
func (c *MultiServerClient) CancelDatabaseTask(ctx context.Context, req *api.CancelDatabaseTaskPayload) (res *api.Task, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.CancelDatabaseTask(ctx, req)
}

func (c *MultiServerClient) SwitchoverDatabaseNode(ctx context.Context, req *api.SwitchoverDatabaseNodePayload) (res *api.SwitchoverDatabaseNodeResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.SwitchoverDatabaseNode(ctx, req)
}

func (c *MultiServerClient) FailoverDatabaseNode(ctx context.Context, req *api.FailoverDatabaseNodeRequest) (res *api.FailoverDatabaseNodeResponse, err error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.FailoverDatabaseNode(ctx, req)
}

func (c *MultiServerClient) WaitForTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error) {
	server, err := c.liveServer(ctx)
	if err != nil {
		return nil, err
	}
	return server.WaitForTask(ctx, req)
}

func (c *MultiServerClient) FollowTask(ctx context.Context, req *api.GetDatabaseTaskLogPayload, handler func(e *api.TaskLogEntry)) error {
	server, err := c.liveServer(ctx)
	if err != nil {
		return err
	}
	return server.FollowTask(ctx, req, handler)
}

func (c *MultiServerClient) liveServer(ctx context.Context) (*SingleServerClient, error) {
	var errs []error
	for _, server := range c.servers {
		_, err := server.GetVersion(ctx)
		if err == nil {
			return server, nil
		} else {
			errs = append(errs, err)
		}
	}

	errs = append(errs, ErrNoHealthyServers)
	return nil, errors.Join(errs...)
}
