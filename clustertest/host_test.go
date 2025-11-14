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

	container, err := testcontainers.GenericContainer(
		t.Context(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		// Use a new context for cleanup operations since t.Context is canceled.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if t.Failed() {
			logs, err := containerLogs(ctx, t, container)
			if err != nil {
				tLogf(t, "failed to extract container logs: %s", err)
			} else {
				tLogf(t, "host %s logs: %s", id, logs)
			}
		}

		if testConfig.skipCleanup {
			tLogf(t, "skipping cleanup for %s container %s", id, container.GetContainerID()[:12])
			return
		}

		container.Terminate(ctx)
	})

	return &Host{
		id:        id,
		port:      ports[0],
		container: container,
	}
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
