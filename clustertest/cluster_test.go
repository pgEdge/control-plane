//go:build cluster_test

package clustertest

import (
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

type ClusterConfig struct {
	ID    string
	Hosts []HostConfig
}

type Cluster struct {
	id     string
	hosts  map[string]*Host
	client client.Client
}

func NewCluster(t testing.TB, config ClusterConfig) *Cluster {
	t.Helper()

	hosts := make(map[string]*Host, len(config.Hosts))
	for _, hostCfg := range config.Hosts {
		host := NewHost(t, hostCfg)
		hosts[host.id] = host
	}

	id := uuid.NewString()
	if config.ID != "" {
		id = config.ID
	}

	return &Cluster{
		id:     id,
		hosts:  hosts,
		client: hostsClient(t, hosts),
	}
}

func (c *Cluster) Host(hostID string) *Host {
	return c.hosts[hostID]
}

func (c *Cluster) Client() client.Client {
	return c.client
}

func (c *Cluster) Init(t testing.TB) {
	t.Helper()

	tLogf(t, "initializing cluster %s", c.id)

	_, err := c.client.InitCluster(t.Context(), &controlplane.InitClusterRequest{
		ClusterID: pointerTo(controlplane.Identifier(c.id)),
	})
	require.NoError(t, err)

	c.AssertHealthy(t)
}

func (c *Cluster) AssertHealthy(t testing.TB) {
	t.Helper()

	tLogf(t, "asserting that all hosts in cluster %s are healthy", c.id)

	resp, err := c.client.ListHosts(t.Context())
	require.NoError(t, err)

	foundHosts := map[string]bool{}

	for _, host := range resp.Hosts {
		assert.Equal(t, "healthy", host.Status.State)
		foundHosts[string(host.ID)] = true
	}
	for hostID := range c.hosts {
		assert.Contains(t, foundHosts, hostID, "expected host %s not found in list hosts output", hostID)
	}
}

func (c *Cluster) Add(t testing.TB, hostCfg HostConfig) {
	t.Helper()

	host := NewHost(t, hostCfg)
	c.hosts[host.id] = host
	c.client = hostsClient(t, c.hosts)
}

func (c *Cluster) Remove(t testing.TB, hostID string) {
	t.Helper()

	resp, err := c.client.RemoveHost(t.Context(), &controlplane.RemoveHostPayload{
		HostID: controlplane.Identifier(hostID),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Task)
	require.NotNil(t, resp.UpdateDatabaseTasks)

	_, err = c.client.WaitForHostTask(t.Context(), &controlplane.GetHostTaskPayload{
		HostID: controlplane.Identifier(hostID),
		TaskID: resp.Task.TaskID,
	})
	require.NoError(t, err)

	delete(c.hosts, hostID)
	c.client = hostsClient(t, c.hosts)
}

// RefreshClient recreates the client with updated host configurations.
// This is useful after a host has been recreated with new settings (e.g., port change).
func (c *Cluster) RefreshClient(t testing.TB) {
	t.Helper()

	tLogf(t, "refreshing client for cluster %s", c.id)
	c.client = hostsClient(t, c.hosts)
}

func hostsClient(t testing.TB, hosts map[string]*Host) client.Client {
	t.Helper()

	servers := make([]client.ServerConfig, 0, len(hosts))

	for host := range maps.Values(hosts) {
		servers = append(servers, host.ClientConfig())
	}

	cli, err := client.NewMultiServerClient(servers...)
	require.NoError(t, err)

	return cli
}
