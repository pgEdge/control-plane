//go:build cluster_test

package clustertest

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/pgEdge/control-plane/client"
)

// EtcdMode defines the etcd mode for a host.
type EtcdMode string

const (
	EtcdModeServer EtcdMode = "server"
	EtcdModeClient EtcdMode = "client"
)

type HostConfig struct {
	ID       string
	EtcdMode EtcdMode
	ExtraEnv map[string]string
}

type Host struct {
	id        string
	port      int
	dataDir   string
	container testcontainers.Container
}

func NewHost(t testing.TB, config HostConfig) *Host {
	t.Helper()

	id := uuid.NewString()
	if config.ID != "" {
		id = config.ID
	}

	dataDir := hostDataDir(t, id)

	etcdMode := EtcdModeServer
	if config.EtcdMode != "" {
		etcdMode = config.EtcdMode
	}

	env := map[string]string{
		"PGEDGE_HOST_ID":  id,
		"PGEDGE_DATA_DIR": dataDir,
	}

	var ports []int

	switch etcdMode {
	case EtcdModeClient:
		ports = allocatePorts(t, 1)
		env["PGEDGE_ETCD_MODE"] = "client"
	case EtcdModeServer:
		ports = allocatePorts(t, 3)
		env["PGEDGE_ETCD_MODE"] = "server"
		env["PGEDGE_ETCD_SERVER__PEER_PORT"] = strconv.Itoa(ports[1])
		env["PGEDGE_ETCD_SERVER__CLIENT_PORT"] = strconv.Itoa(ports[2])
	default:
		t.Fatalf("unrecognized etcd mode '%s'", etcdMode)
	}

	env["PGEDGE_HTTP__PORT"] = strconv.Itoa(ports[0])

	// Apply env overrides last
	maps.Copy(env, config.ExtraEnv)

	req := testcontainers.ContainerRequest{
		AlwaysPullImage: true,
		Image:           testConfig.imageTag,
		Env:             env,
		Cmd:             []string{"run"},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.NetworkMode = "host"
			hc.Mounts = []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: dataDir,
					Target: dataDir,
				},
				{
					Type:   mount.TypeBind,
					Source: "/var/run/docker.sock",
					Target: "/var/run/docker.sock",
				},
			}
		},
		WaitingFor: wait.ForHTTP("/v1/version").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", ports[0]))).
			WithStartupTimeout(10 * time.Second),
	}

	tLogf(t, "creating host %s", id)

	ctr, err := testcontainers.GenericContainer(
		t.Context(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	if err != nil {
		printContainerLogs(t.Context(), t, id, ctr)
		t.Fatal(err)
	}

	h := &Host{
		id:        id,
		port:      ports[0],
		dataDir:   dataDir,
		container: ctr,
	}

	t.Cleanup(func() {
		// Use a new context for cleanup operations since t.Context is canceled.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if t.Failed() {

			printContainerLogs(ctx, t, id, h.container)
		}

		if testConfig.skipCleanup {
			tLogf(t, "skipping cleanup for %s container %s", id, h.container.GetContainerID()[:12])
			return
		}

		h.container.Terminate(ctx)
	})

	return h
}

func (h *Host) Stop(t testing.TB) {
	t.Helper()

	tLogf(t, "stopping host %s", h.id)

	require.NoError(t, h.container.Stop(t.Context(), nil))
}

func (h *Host) Start(t testing.TB) {
	t.Helper()

	tLogf(t, "starting host %s", h.id)

	require.NoError(t, h.container.Start(t.Context()))
}

func (h *Host) ClientConfig() client.ServerConfig {
	return client.NewHTTPServerConfig(h.id, &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", h.port),
	})
}

// GetEtcdMode retrieves the etcd mode for this host from the API.
// It accepts an optional client parameter. If nil, it creates a client from the host's config.
// When querying a host that was just recreated and may not be initialized yet,
// pass a cluster client that can reach other initialized hosts.
func (h *Host) GetEtcdMode(t testing.TB, cli client.Client) string {
	t.Helper()

	var err error
	if cli == nil {
		cli, err = client.NewMultiServerClient(h.ClientConfig())
		require.NoError(t, err)
	}

	resp, err := cli.ListHosts(t.Context())
	require.NoError(t, err)

	for _, host := range resp.Hosts {
		if string(host.ID) == h.id {
			if host.EtcdMode == nil {
				return ""
			}
			return *host.EtcdMode
		}
	}

	t.Fatalf("host %s not found in API response", h.id)
	return ""
}

// RecreateWithMode stops the current container and recreates it with a new etcd mode.
// This simulates changing the PGEDGE_ETCD_MODE environment variable and restarting.
// The new container will be cleaned up by the original cleanup registered in NewHost.
func (h *Host) RecreateWithMode(t testing.TB, newMode EtcdMode) {
	t.Helper()

	tLogf(t, "recreating host %s with etcd mode %s", h.id, newMode)

	// Stop the current container
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := h.container.Terminate(ctx)
	require.NoError(t, err)

	// Reuse the original data directory to preserve cluster state
	dataDir := h.dataDir

	// Build the new environment
	env := map[string]string{
		"PGEDGE_HOST_ID":  h.id,
		"PGEDGE_DATA_DIR": dataDir,
	}

	var ports []int

	switch newMode {
	case EtcdModeClient:
		ports = allocatePorts(t, 1)
		env["PGEDGE_ETCD_MODE"] = "client"
	case EtcdModeServer:
		ports = allocatePorts(t, 3)
		env["PGEDGE_ETCD_MODE"] = "server"
		env["PGEDGE_ETCD_SERVER__PEER_PORT"] = strconv.Itoa(ports[1])
		env["PGEDGE_ETCD_SERVER__CLIENT_PORT"] = strconv.Itoa(ports[2])
	default:
		t.Fatalf("unrecognized etcd mode '%s'", newMode)
	}

	env["PGEDGE_HTTP__PORT"] = strconv.Itoa(ports[0])

	req := testcontainers.ContainerRequest{
		AlwaysPullImage: true,
		Image:           testConfig.imageTag,
		Env:             env,
		Cmd:             []string{"run"},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.NetworkMode = "host"
			hc.Mounts = []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: dataDir,
					Target: dataDir,
				},
				{
					Type:   mount.TypeBind,
					Source: "/var/run/docker.sock",
					Target: "/var/run/docker.sock",
				},
			}
		},
		WaitingFor: wait.ForHTTP("/v1/version").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", ports[0]))).
			WithStartupTimeout(60 * time.Second),
	}

	tLogf(t, "starting host %s with new mode %s", h.id, newMode)

	newContainer, err := testcontainers.GenericContainer(
		t.Context(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	require.NoError(t, err)

	// Update the host's container reference and port.
	// The cleanup registered in NewHost will terminate h.container,
	// which now points to the new container.
	h.container = newContainer
	h.port = ports[0]
}

func printContainerLogs(ctx context.Context, t testing.TB, hostID string, container testcontainers.Container) {
	t.Helper()

	if container == nil {
		tLog(t, "container is nil")
		return
	}
	logs, err := containerLogs(t.Context(), t, container)
	if err != nil {
		tLogf(t, "failed to extract container logs: %s", err)
	} else {
		tLogf(t, "host %s logs: %s", hostID, logs)
	}
}

func containerLogs(ctx context.Context, t testing.TB, container testcontainers.Container) (string, error) {
	t.Helper()

	r, err := container.Logs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get container logs reader: %w", err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read container logs: %w", err)
	}

	return string(out), nil
}
