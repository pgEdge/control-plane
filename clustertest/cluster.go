package clustertest

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	cp "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
)

// Cluster represents a test cluster of control-plane hosts.
type Cluster struct {
	t             *testing.T
	hosts         map[string]*Host
	hostList      []*Host
	network       *testcontainers.DockerNetwork
	client        client.Client
	initialized   bool
	keepOnFailure bool
	logCapture    bool
}

// NewCluster creates and starts a test cluster with the given configuration.
func NewCluster(ctx context.Context, t *testing.T, opts ...ClusterOption) (*Cluster, error) {
	t.Helper()

	// Parse and validate configuration.
	cfg := defaultClusterConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid cluster configuration: %w", err)
	}

	// Determine control-plane image (build if unspecified).
	image := cfg.image
	if image == "" {
		var err error
		if image, err = buildControlPlaneImage(ctx, t); err != nil {
			return nil, fmt.Errorf("failed to build control plane image: %w", err)
		}
	}

	// Create Docker network for isolation.
	net, err := network.New(ctx, network.WithCheckDuplicate(), network.WithDriver("bridge"))
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	cluster := &Cluster{
		t:             t,
		hosts:         make(map[string]*Host),
		hostList:      make([]*Host, 0, len(cfg.hosts)),
		network:       net,
		keepOnFailure: cfg.keepOnFailure,
		logCapture:    cfg.logCapture,
	}

	// Start etcd servers first, then clients.
	etcdServers := cfg.getEtcdServers()
	for _, hostCfg := range cfg.hosts {
		if hostCfg.EtcdMode == EtcdModeServer {
			if err := cluster.addHost(ctx, hostCfg, image, etcdServers); err != nil {
				cluster.cleanup(ctx, false)
				return nil, err
			}
		}
	}
	if err := cluster.waitForHosts(ctx, 30*time.Second); err != nil {
		cluster.cleanup(ctx, false)
		return nil, fmt.Errorf("etcd servers failed to become ready: %w", err)
	}

	for _, hostCfg := range cfg.hosts {
		if hostCfg.EtcdMode == EtcdModeClient {
			if err := cluster.addHost(ctx, hostCfg, image, etcdServers); err != nil {
				cluster.cleanup(ctx, false)
				return nil, err
			}
		}
	}
	if err := cluster.waitForHosts(ctx, 30*time.Second); err != nil {
		cluster.cleanup(ctx, false)
		return nil, fmt.Errorf("hosts failed to become ready: %w", err)
	}

	// Create multi-server client.
	if err := cluster.initClient(); err != nil {
		cluster.cleanup(ctx, false)
		return nil, err
	}

	// Optionally initialize cluster.
	if cfg.autoInit {
		if err := cluster.initializeCluster(ctx); err != nil {
			cluster.cleanup(ctx, false)
			return nil, fmt.Errorf("failed to initialize cluster: %w", err)
		}
		cluster.initialized = true
	}

	// Register automatic cleanup with the test framework.
	t.Cleanup(func() {
		// Allow more time for cleanup, especially for hung containers.
		// This needs to be longer than the container stop timeout (currently 30s per container).
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := cluster.cleanup(cleanupCtx, !cluster.keepOnFailure || !t.Failed()); err != nil {
			t.Errorf("Failed to cleanup cluster: %v", err)
		}
	})

	return cluster, nil
}

// addHost creates and starts a new host container.
func (c *Cluster) addHost(ctx context.Context, cfg HostConfig, image string, etcdServers []string) error {
	host, err := createHost(ctx, cfg, c.network, image, etcdServers)
	if err != nil {
		return fmt.Errorf("failed to create host %s: %w", cfg.ID, err)
	}
	c.hosts[cfg.ID] = host
	c.hostList = append(c.hostList, host)
	return nil
}

// initClient creates a multi-server client for interacting with all hosts.
func (c *Cluster) initClient() error {
	serverConfigs := make([]client.ServerConfig, 0, len(c.hostList))
	for _, host := range c.hostList {
		u, err := url.Parse(host.APIURL())
		if err != nil {
			return fmt.Errorf("invalid API URL for host %s: %w", host.ID(), err)
		}
		serverConfigs = append(serverConfigs, client.NewHTTPServerConfig(host.ID(), u))
	}
	cli, err := client.NewMultiServerClient(serverConfigs...)
	if err != nil {
		return fmt.Errorf("failed to create multi-server client: %w", err)
	}
	c.client = cli
	return nil
}

// waitForHosts verifies all hosts are alive and have active containers.
func (c *Cluster) waitForHosts(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, host := range c.hostList {
		select {
		case <-ctx.Done():
			if logs, err := host.Logs(ctx); err == nil {
				c.t.Logf("=== Container logs for %s on timeout ===\n%s\n", host.ID(), logs)
			}
			return fmt.Errorf("timeout waiting for host %s: %w", host.ID(), ctx.Err())
		default:
			if host.container == nil {
				return fmt.Errorf("host %s has no container", host.ID())
			}
		}
	}
	return nil
}

// initializeCluster runs the control-plane InitCluster API sequence.
func (c *Cluster) initializeCluster(ctx context.Context) error {
	req := &cp.InitClusterRequest{ClusterID: nil}
	if _, err := c.client.InitCluster(ctx, req); err != nil {
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}
	return nil
}

// cleanup releases all cluster resources (containers, network, logs, etc.).
func (c *Cluster) cleanup(ctx context.Context, shouldCleanup bool) error {
	var cleanupErr error

	// Capture logs if requested
	if c.logCapture {
		if logs, err := c.Logs(ctx); err == nil {
			for id, log := range logs {
				c.t.Logf("=== Logs for host %s ===\n%s\n", id, log)
			}
		} else {
			c.t.Logf("Failed to capture logs: %v", err)
		}
	}

	// Skip cleanup if requested (e.g., on test failure)
	if !shouldCleanup {
		c.t.Logf("Keeping cluster resources for debugging (test failed)")
		for id, host := range c.hosts {
			if name, err := host.Container().Name(ctx); err == nil {
				c.t.Logf("Host %s container name: %s", id, name)
			}
		}
		return nil
	}

	// Terminate hosts
	for id, host := range c.hosts {
		if err := host.Terminate(ctx); err != nil {
			c.t.Logf("Failed to terminate host %s: %v", id, err)
			if cleanupErr == nil {
				cleanupErr = err
			}
		}
	}

	// Remove network
	if c.network != nil {
		if err := c.network.Remove(ctx); err != nil {
			c.t.Logf("Failed to remove network: %v", err)
			if cleanupErr == nil {
				cleanupErr = err
			}
		}
	}
	return cleanupErr
}

// Client returns the multi-server client.
func (c *Cluster) Client() client.Client { return c.client }

// Host returns the host with the given ID.
func (c *Cluster) Host(id string) *Host { return c.hosts[id] }

// Hosts returns all hosts in creation order.
func (c *Cluster) Hosts() []*Host { return c.hostList }

// Initialized reports whether the cluster has been initialized.
func (c *Cluster) Initialized() bool { return c.initialized }

// InitializeCluster manually initializes the cluster.
func (c *Cluster) InitializeCluster(ctx context.Context) error {
	if c.initialized {
		return fmt.Errorf("cluster already initialized")
	}
	if err := c.initializeCluster(ctx); err != nil {
		return err
	}
	c.initialized = true
	return nil
}

// Cleanup manually releases cluster resources.
func (c *Cluster) Cleanup(ctx context.Context) error { return c.cleanup(ctx, true) }

// Logs returns logs from all cluster hosts.
func (c *Cluster) Logs(ctx context.Context) (map[string]string, error) {
	logs := make(map[string]string)
	for id, host := range c.hosts {
		out, err := host.Logs(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get logs for host %s: %w", id, err)
		}
		logs[id] = out
	}
	return logs, nil
}

// DONE 28 tests, 6 skipped, 1 failure in 350.700s
