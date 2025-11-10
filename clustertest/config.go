package clustertest

import "fmt"

// EtcdMode defines the etcd mode for a host.
type EtcdMode string

const (
	EtcdModeServer EtcdMode = "server"
	EtcdModeClient EtcdMode = "client"
)

// HostConfig defines configuration for a single host.
type HostConfig struct {
	ID       string
	EtcdMode EtcdMode
	ExtraEnv map[string]string
	DataDir  string
}

// clusterConfig holds internal cluster settings.
type clusterConfig struct {
	hosts         []HostConfig
	autoInit      bool
	keepOnFailure bool
	logCapture    bool
	image         string
}

// ClusterOption configures a cluster.
type ClusterOption func(*clusterConfig)

// WithHosts creates a cluster with a given number of hosts and sensible etcd topology.
func WithHosts(count int) ClusterOption {
	return func(c *clusterConfig) {
		c.hosts = make([]HostConfig, count)
		for i := 0; i < count; i++ {
			mode := EtcdModeServer
			if count >= 4 && i >= 3 {
				mode = EtcdModeClient
			}
			c.hosts[i] = HostConfig{
				ID:       fmt.Sprintf("host-%d", i+1),
				EtcdMode: mode,
				ExtraEnv: make(map[string]string),
			}
		}
	}
}

// WithHost adds a single custom host.
func WithHost(cfg HostConfig) ClusterOption {
	return func(c *clusterConfig) {
		if cfg.ExtraEnv == nil {
			cfg.ExtraEnv = make(map[string]string)
		}
		c.hosts = append(c.hosts, cfg)
	}
}

// WithAutoInit enables or disables automatic cluster initialization.
func WithAutoInit(enable bool) ClusterOption {
	return func(c *clusterConfig) { c.autoInit = enable }
}

// WithKeepOnFailure preserves containers on test failure.
func WithKeepOnFailure(enable bool) ClusterOption {
	return func(c *clusterConfig) { c.keepOnFailure = enable }
}

// WithLogCapture enables log collection from containers.
func WithLogCapture(enable bool) ClusterOption {
	return func(c *clusterConfig) { c.logCapture = enable }
}

// WithImage sets a specific Docker image for the control plane.
func WithImage(image string) ClusterOption {
	return func(c *clusterConfig) { c.image = image }
}

// defaultClusterConfig returns a clusterConfig with default settings.
func defaultClusterConfig() *clusterConfig {
	return &clusterConfig{}
}

// validate checks the cluster configuration for correctness.
func (c *clusterConfig) validate() error {
	if len(c.hosts) == 0 {
		return fmt.Errorf("cluster must have at least one host")
	}

	seen := make(map[string]bool)
	hasServer := false

	for _, h := range c.hosts {
		if h.ID == "" {
			return fmt.Errorf("host ID cannot be empty")
		}
		if seen[h.ID] {
			return fmt.Errorf("duplicate host ID: %s", h.ID)
		}
		seen[h.ID] = true

		if h.EtcdMode != EtcdModeServer && h.EtcdMode != EtcdModeClient {
			return fmt.Errorf("invalid etcd mode for host %s: %s", h.ID, h.EtcdMode)
		}
		if h.EtcdMode == EtcdModeServer {
			hasServer = true
		}
	}

	if !hasServer {
		return fmt.Errorf("cluster must have at least one etcd server")
	}

	return nil
}

// getEtcdServers returns IDs of hosts running as etcd servers.
func (c *clusterConfig) getEtcdServers() []string {
	var servers []string
	for _, h := range c.hosts {
		if h.EtcdMode == EtcdModeServer {
			servers = append(servers, h.ID)
		}
	}
	return servers
}

// DONE 28 tests, 6 skipped, 1 failure in 374.472s
