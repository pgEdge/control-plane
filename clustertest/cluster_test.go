package clustertest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfShort skips integration tests when running with -short.
func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

// newTestCluster is a helper to create a cluster with consistent error handling.
func newTestCluster(ctx context.Context, t *testing.T, opts ...ClusterOption) *Cluster {
	cluster, err := NewCluster(ctx, t, opts...)
	require.NoError(t, err)
	require.NotNil(t, cluster)
	return cluster
}

// Basic Cluster Lifecycle Tests

func TestClusterCreation(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(3), WithAutoInit(true))
	assert.True(t, cluster.Initialized(), "cluster should be initialized")

	hosts := cluster.Hosts()
	assert.Len(t, hosts, 3)

	for i, want := range []string{"host-1", "host-2", "host-3"} {
		assert.Equal(t, want, hosts[i].ID())
	}

	_, err := cluster.Client().GetCluster(ctx)
	require.NoError(t, err, "cluster should be accessible via API")
}

func TestUninitializedCluster(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(2), WithAutoInit(false))
	assert.False(t, cluster.Initialized(), "cluster should not be auto-initialized")

	_, err := cluster.Client().GetCluster(ctx)
	assert.Error(t, err, "API should fail before initialization")

	require.NoError(t, cluster.InitializeCluster(ctx))
	assert.True(t, cluster.Initialized())

	_, err = cluster.Client().GetCluster(ctx)
	require.NoError(t, err, "API should work after initialization")
}

// Custom Configuration

func TestCustomConfiguration(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t,
		WithHost(HostConfig{ID: "custom-1", EtcdMode: EtcdModeServer, ExtraEnv: map[string]string{"PGEDGE_LOGGING__LEVEL": "debug"}}),
		WithHost(HostConfig{ID: "custom-2", EtcdMode: EtcdModeServer}),
		WithHost(HostConfig{ID: "custom-3", EtcdMode: EtcdModeServer}),
		WithAutoInit(true),
	)

	ids := []string{"custom-1", "custom-2", "custom-3"}
	for i, host := range cluster.Hosts() {
		assert.Equal(t, ids[i], host.ID())
	}
}

// Host Lifecycle and Behavior

func TestHostLifecycle(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(2), WithAutoInit(true))
	host := cluster.Hosts()[0]

	require.NoError(t, host.Stop(ctx))
	time.Sleep(2 * time.Second)
	require.NoError(t, host.Start(ctx))
	time.Sleep(2 * time.Second)
	require.NoError(t, host.Restart(ctx))

	_, err := cluster.Client().GetCluster(ctx)
	require.NoError(t, err, "cluster should remain accessible after lifecycle ops")
}

func TestHostPauseUnpause(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(3), WithAutoInit(true))
	host := cluster.Hosts()[2]

	require.NoError(t, host.Pause(ctx))
	_, err := cluster.Client().GetCluster(ctx)
	require.NoError(t, err, "cluster should still be accessible with majority quorum")

	require.NoError(t, host.Unpause(ctx))
	time.Sleep(2 * time.Second)

	_, err = cluster.Client().GetCluster(ctx)
	require.NoError(t, err)
}

func TestHostExecution(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(1), WithAutoInit(true))
	host := cluster.Hosts()[0]

	out, err := host.Exec(ctx, []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Contains(t, out, "hello")

	out, err = host.Exec(ctx, []string{"ls", "/data"})
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

// Log Retrieval

func TestHostLogs(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(1), WithAutoInit(true))
	logs, err := cluster.Hosts()[0].Logs(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, logs)

	t.Logf("Host logs (first 500 chars): %s", truncate(logs, 500))
}

func TestClusterLogs(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()

	cluster := newTestCluster(ctx, t, WithHosts(2), WithAutoInit(true))
	logs, err := cluster.Logs(ctx)
	require.NoError(t, err)
	assert.Len(t, logs, 2)

	for id, log := range logs {
		assert.NotEmpty(t, log)
		t.Logf("Host %s logs (first 200 chars): %s", id, truncate(log, 200))
	}
}

// Etcd and Configuration Validation

func TestEtcdTopology(t *testing.T) {
	skipIfShort(t)
	tests := []struct {
		name        string
		hostCount   int
		wantServers int
	}{
		{"single host", 1, 1},
		{"three servers", 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			cluster := newTestCluster(ctx, t, WithHosts(tt.hostCount), WithAutoInit(true))
			_, err := cluster.Client().GetCluster(ctx)
			require.NoError(t, err)

			if tt.wantServers > 0 {
				server := cluster.Hosts()[0]
				captureHostDiagnostics(t, server, ctx)

				client, err := server.EtcdClient(ctx)
				require.NoError(t, err)

				resp, err := client.MemberList(ctx)
				require.NoError(t, err)
				assert.Len(t, resp.Members, tt.wantServers)
			}
		})
	}
}

func TestEtcdClientAccess(t *testing.T) {
	skipIfShort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newTestCluster(ctx, t, WithHosts(3), WithAutoInit(true))
	client, err := cluster.Hosts()[0].EtcdClient(ctx)
	require.NoError(t, err)

	resp, err := client.MemberList(ctx)
	require.NoError(t, err)
	assert.Len(t, resp.Members, 3)

	for _, m := range resp.Members {
		assert.NotEmpty(t, m.Name)
		assert.NotEmpty(t, m.ClientURLs)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *clusterConfig
		wantErr bool
	}{
		{"valid single", &clusterConfig{hosts: []HostConfig{{ID: "h1", EtcdMode: EtcdModeServer}}}, false},
		{"valid multi", &clusterConfig{hosts: []HostConfig{{ID: "h1", EtcdMode: EtcdModeServer}, {ID: "h2", EtcdMode: EtcdModeServer}}}, false},
		{"no hosts", &clusterConfig{}, true},
		{"duplicate IDs", &clusterConfig{hosts: []HostConfig{{ID: "h1", EtcdMode: EtcdModeServer}, {ID: "h1", EtcdMode: EtcdModeServer}}}, true},
		{"empty ID", &clusterConfig{hosts: []HostConfig{{ID: "", EtcdMode: EtcdModeServer}}}, true},
		{"no etcd servers", &clusterConfig{hosts: []HostConfig{{ID: "h1", EtcdMode: EtcdModeClient}}}, true},
		{"invalid mode", &clusterConfig{hosts: []HostConfig{{ID: "h1", EtcdMode: "invalid"}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Utility Helpers

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func captureHostDiagnostics(t *testing.T, h *Host, ctx context.Context) {
	t.Helper()
	t.Logf("=== Diagnostics for host %s ===", h.ID())

	if status, err := h.GetHealthStatus(ctx); err == nil {
		t.Logf("Health: %+v", status)
	}
	if logs, err := h.Logs(ctx); err == nil {
		t.Logf("Logs (last 500 chars): %s", truncate(logs, 500))
	}
	if out, err := h.Exec(ctx, []string{"ps", "aux"}); err == nil {
		t.Logf("Processes:\n%s", out)
	}
}

// DONE 28 tests, 6 skipped, 1 failure in 370.034s
