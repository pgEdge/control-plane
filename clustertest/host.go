package clustertest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.etcd.io/etcd/client/v3"
)

// Host represents a single control plane host in the test cluster.
type Host struct {
	id         string
	container  testcontainers.Container
	config     HostConfig
	apiURL     string
	apiPort    int
	certDir    string
	etcdClient *clientv3.Client
}

func (h *Host) ID() string                          { return h.id }
func (h *Host) APIURL() string                      { return h.apiURL }
func (h *Host) APIPort() int                        { return h.apiPort }
func (h *Host) Container() testcontainers.Container { return h.container }
func (h *Host) Start(ctx context.Context) error     { return h.container.Start(ctx) }
func (h *Host) Stop(ctx context.Context) error      { return h.container.Stop(ctx, nil) }
func (h *Host) Pause(ctx context.Context) error     { return h.Stop(ctx) }
func (h *Host) Unpause(ctx context.Context) error   { return h.Start(ctx) }

func (h *Host) Restart(ctx context.Context) error {
	if err := h.Stop(ctx); err != nil {
		return err
	}
	return h.Start(ctx)
}

func (h *Host) Terminate(ctx context.Context) error {
	if h.etcdClient != nil {
		_ = h.etcdClient.Close()
		h.etcdClient = nil
	}
	return h.container.Terminate(ctx)
}

func (h *Host) Logs(ctx context.Context) (string, error) {
	reader, err := h.container.Logs(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(logs), nil
}

func (h *Host) Exec(ctx context.Context, cmd []string) (string, error) {
	exitCode, reader, err := h.container.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	if exitCode != 0 {
		return string(output), fmt.Errorf("command exited with code %d", exitCode)
	}
	return string(output), nil
}

// GetHealthStatus checks various health endpoints and returns status information
func (h *Host) GetHealthStatus(ctx context.Context) (map[string]interface{}, error) {
	endpoints := []string{
		h.apiURL + "/v1/version",
		h.apiURL + "/v1/health",
		h.apiURL + "/v1/status",
		h.apiURL + "/healthz",
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range endpoints {
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			body := make(map[string]interface{})
			if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
				resp.Body.Close()
				return body, nil
			}
			resp.Body.Close()
		}
	}
	return nil, fmt.Errorf("unable to get health status from host %s", h.id)
}

// EtcdClient returns a direct etcd client for advanced testing scenarios
func (h *Host) EtcdClient(ctx context.Context) (*clientv3.Client, error) {
	if h.etcdClient != nil {
		return h.etcdClient, nil
	}

	etcdPort, err := h.container.MappedPort(ctx, "2379/tcp")
	if err != nil {
		return nil, err
	}

	host, err := h.container.Host(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("https://%s:%d", host, etcdPort.Int())

	caCert, err := os.ReadFile(filepath.Join(h.certDir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(h.certDir, "etcd-user.crt"),
		filepath.Join(h.certDir, "etcd-user.key"),
	)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{clientCert},
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 10 * time.Second,
		TLS:         tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client (endpoint: %s): %w", endpoint, err)
	}

	h.etcdClient = cli
	return h.etcdClient, nil
}

// createHost creates and starts a new host container
func createHost(ctx context.Context, config HostConfig, network *testcontainers.DockerNetwork, image string, initialPeers []string) (*Host, error) {
	env := map[string]string{
		"PGEDGE_HOST_ID":                             config.ID,
		"PGEDGE_HTTP__PORT":                          "3000",
		"PGEDGE_TENANT_ID":                           "default",
		"PGEDGE_DOCKER_SWARM__IMAGE_REPOSITORY_HOST": "ghcr.io/pgedge",
		"PGEDGE_LOGGING__LEVEL":                      "info",
	}

	// Configure etcd mode
	if config.EtcdMode == EtcdModeServer {
		env["PGEDGE_ETCD_MODE"] = "server"
		env["PGEDGE_ETCD_SERVER__CLIENT_PORT"] = "2379"
		env["PGEDGE_ETCD_SERVER__PEER_PORT"] = "2380"
		if len(initialPeers) > 0 {
			env["PGEDGE_ETCD_SERVER__INITIAL_CLUSTER"] = buildInitialCluster(initialPeers)
		}
	} else {
		env["PGEDGE_ETCD_MODE"] = "client"
		if len(initialPeers) > 0 {
			env["PGEDGE_ETCD_CLIENT__ENDPOINTS"] = buildClientEndpoints(initialPeers)
		}
	}

	for k, v := range config.ExtraEnv {
		env[k] = v
	}

	exposedPorts := []string{"3000/tcp", "2379/tcp"}
	if config.EtcdMode == EtcdModeServer {
		exposedPorts = append(exposedPorts, "2380/tcp")
	}

	// HTTP API startup indicates control plane is running, but etcd may still be initializing
	req := testcontainers.ContainerRequest{
		Image:        image,
		Env:          env,
		ExposedPorts: exposedPorts,
		Networks:     []string{network.Name},
		NetworkAliases: map[string][]string{
			network.Name: {config.ID},
		},
		// No Cmd needed - the image ENTRYPOINT handles starting the control plane
		// HTTP API startup indicates the control plane binary is running and responding,
		// but etcd may still be initializing in the background. The 180-second timeout
		// gives etcd plenty of time to initialize, especially during first-time certificate
		// generation and cluster formation.
		WaitingFor: wait.ForHTTP("/v1/version").
			WithPort("3000/tcp").
			WithStartupTimeout(180 * time.Second),
	}

	configPath, err := filepath.Abs(filepath.Join("..", "docker/control-plane-dev/config.json"))
	if err != nil {
		return nil, err
	}

	mounts := []testcontainers.ContainerMount{
		testcontainers.BindMount(configPath, "/config.json"),
		testcontainers.BindMount("/var/run/docker.sock", "/var/run/docker.sock"),
	}

	var certDir string
	if config.DataDir != "" {
		// Custom data directory - not supported for bind mounts with Docker Swarm
		return nil, fmt.Errorf("custom DataDir not supported in test environment (use default)")
	}

	// Use the default data directory structure
	dataPath, err := filepath.Abs(filepath.Join("..", "docker/control-plane-dev/data", config.ID))
	if err != nil {
		return nil, err
	}
	certPath := filepath.Join(dataPath, "certificates")
	if _, err := os.Stat(certPath); err != nil {
		return nil, fmt.Errorf("pre-generated certificates not found for host %s at %s (run: make -C docker/control-plane-dev gen-certs)", config.ID, certPath)
	}

	// Clean up stale etcd data and instances from previous test runs
	etcdPath := filepath.Join(dataPath, "etcd")
	if err := os.RemoveAll(etcdPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to clean etcd directory: %w", err)
	}
	instancesPath := filepath.Join(dataPath, "instances")
	if err := os.RemoveAll(instancesPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to clean instances directory: %w", err)
	}

	// Set PGEDGE_DATA_DIR to the absolute path on the Docker host
	// This is critical for Docker Swarm services to mount the correct paths
	// Note: We don't mount the data directory - the control plane accesses it directly from the host
	env["PGEDGE_DATA_DIR"] = dataPath
	certDir = certPath

	req.Mounts = mounts

	container, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
	if err != nil {
		if container != nil {
			if logs, logErr := container.Logs(ctx); logErr == nil {
				defer logs.Close()
				if logBytes, _ := io.ReadAll(logs); logBytes != nil {
					fmt.Println("=== Container logs ===")
					fmt.Println(string(logBytes))
				}
			}
		}
		return nil, fmt.Errorf("failed to create container for host %s: %w", config.ID, err)
	}

	apiPort, err := container.MappedPort(ctx, "3000/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	containerHost, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	return &Host{
		id:        config.ID,
		container: container,
		config:    config,
		apiURL:    fmt.Sprintf("http://%s:%d", containerHost, apiPort.Int()),
		apiPort:   apiPort.Int(),
		certDir:   certDir,
	}, nil
}

func buildInitialCluster(peers []string) string {
	var cluster string
	for i, peer := range peers {
		if i > 0 {
			cluster += ","
		}
		cluster += fmt.Sprintf("%s=http://%s:2380", peer, peer)
	}
	return cluster
}

func buildClientEndpoints(peers []string) string {
	var endpoints string
	for i, peer := range peers {
		if i > 0 {
			endpoints += ","
		}
		endpoints += fmt.Sprintf("http://%s:2379", peer)
	}
	return endpoints
}
