package etcd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	clientv3 "go.etcd.io/etcd/client/v3"
	goahttp "goa.design/goa/v3/http"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/server/internal/config"
)

// DecodeJoinCredentials decodes base64-encoded credentials from the API join response
// and converts them to etcd JoinOptions. This is a shared utility used by both
// automatic reconfiguration and the JoinCluster API.
func DecodeJoinCredentials(joinOpts *api.ClusterJoinOptions) (*JoinOptions, error) {
	// Decode all certificate and key data
	caCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.CaCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA certificate: %w", err)
	}

	clientCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client certificate: %w", err)
	}

	clientKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client key: %w", err)
	}

	serverCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server certificate: %w", err)
	}

	serverKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server key: %w", err)
	}

	// Create JoinOptions for embedded etcd
	return &JoinOptions{
		Leader: &ClusterMember{
			Name:       joinOpts.Leader.Name,
			PeerURLs:   joinOpts.Leader.PeerUrls,
			ClientURLs: joinOpts.Leader.ClientUrls,
		},
		Credentials: &HostCredentials{
			Username:   joinOpts.Credentials.Username,
			Password:   joinOpts.Credentials.Password,
			CaCert:     caCert,
			ClientCert: clientCert,
			ClientKey:  clientKey,
			ServerCert: serverCert,
			ServerKey:  serverKey,
		},
	}, nil
}

// CreateAPIClient creates a goa API client for the Control Plane API.
// This is a shared utility used by both automatic reconfiguration and the JoinCluster API.
func CreateAPIClient(serverURL *url.URL, httpClient *http.Client) *api.Client {
	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	c := client.NewClient(serverURL.Scheme, serverURL.Host, httpClient, enc, dec, false)

	return &api.Client{
		GetJoinTokenEndpoint:   c.GetJoinToken(),
		GetJoinOptionsEndpoint: c.GetJoinOptions(),
	}
}

// reconfigureServerToClient handles the transition from server mode to client mode.
// It removes the host from the etcd cluster membership and configures it as a remote client.
func reconfigureServerToClient(
	ctx context.Context,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	appCfg := cfg.Config()
	generated := cfg.GeneratedConfig()

	logger.Info().Msg("starting server->client reconfiguration")

	// Check if embedded etcd was ever initialized
	embedded := NewEmbeddedEtcd(cfg, logger)
	initialized, err := embedded.IsInitialized()
	if err != nil {
		return nil, fmt.Errorf("failed to check embedded etcd initialization during server->client transition: %w", err)
	}

	// If etcd was never initialized, there's nothing to demote – just persist
	// the new mode and come up as a client.
	if !initialized {
		logger.Info().Msg("embedded etcd not initialized, skipping server->client demotion")

		generated.EtcdMode = appCfg.EtcdMode
		if err := cfg.UpdateGeneratedConfig(generated); err != nil {
			return nil, fmt.Errorf("failed to update generated config for server->client (uninitialized) transition: %w", err)
		}

		return NewRemoteEtcd(cfg, logger), nil
	}

	// Connect to cluster using existing credentials
	logger.Info().Msg("getting cluster member list to find remote endpoints")

	// Create a temporary client connection using the existing server credentials
	localClientURLs := []string{fmt.Sprintf("https://%s:%d", appCfg.IPv4Address, appCfg.EtcdServer.ClientPort)}

	clientCfg, err := clientConfig(appCfg, logger, localClientURLs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create client config for server->client transition: %w", err)
	}

	client, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local etcd for server->client transition: %w", err)
	}
	defer client.Close()

	// Get the full member list before removing this host
	resp, err := client.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd members for server->client transition: %w", err)
	}

	var endpoints []string
	for _, m := range resp.Members {
		// Skip this host's member; we are about to remove it.
		if m.Name == appCfg.HostID {
			continue
		}
		endpoints = append(endpoints, m.ClientURLs...)
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("cannot demote etcd server on host %s: no remaining cluster members with client URLs", appCfg.HostID)
	}

	// Remove this host's etcd member from the cluster
	logger.Info().Msg("removing this host from etcd cluster")
	if err := RemoveMember(ctx, client, appCfg.HostID); err != nil {
		logger.Warn().Err(err).Msg("failed to remove this host via direct etcd API")
		// Continue anyway - when we shut down the etcd server, the cluster will detect it
		logger.Warn().Msg("continuing with server->client transition - cluster will detect member loss")
	}

	// Remove the etcd data directory since we're no longer a server
	etcdDataDir := filepath.Join(appCfg.DataDir, "etcd")
	logger.Info().
		Str("etcd_data_dir", etcdDataDir).
		Msg("removing etcd data directory for server->client transition")

	if err := os.RemoveAll(etcdDataDir); err != nil {
		logger.Warn().
			Err(err).
			Str("etcd_data_dir", etcdDataDir).
			Msg("failed to remove etcd data directory - continuing anyway")
	}

	// Persist new mode + remote endpoints
	generated.EtcdMode = appCfg.EtcdMode
	generated.EtcdClient.Endpoints = endpoints
	if err := cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to update generated config after server->client transition: %w", err)
	}

	logger.Info().
		Strs("endpoints", endpoints).
		Msg("completed etcd server->client transition; using remaining cluster members as remote endpoints")

	// Return a new RemoteEtcd client for the demoted host
	return NewRemoteEtcd(cfg, logger), nil
}

// reconfigureClientToServer handles the transition from client mode to server mode.
// It joins the host to the etcd cluster as an embedded server member.
func reconfigureClientToServer(
	ctx context.Context,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	appCfg := cfg.Config()

	logger.Info().
		Str("host_id", appCfg.HostID).
		Msg("starting client->server reconfiguration")

	// Connect to the existing cluster as a remote client using persisted credentials.
	remote := NewRemoteEtcd(cfg, logger)

	logger.Info().Msg("starting remote etcd client with existing credentials")
	if err := remote.Start(ctx); err != nil {
		// Authentication failed - credentials are invalid
		logger.Error().
			Err(err).
			Msg("failed to authenticate with existing credentials - cannot automatically recover without valid credentials")

		// Clear the invalid config
		if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
			logger.Error().Err(clearErr).Msg("failed to clear generated config")
		}

		// Return to pre-init mode - manual intervention required
		logger.Info().Msg("returning to pre-initialization mode - use JoinCluster API to rejoin")
		return NewRemoteEtcd(cfg, logger), nil
	}

	logger.Info().Msg("remote etcd client started, querying cluster information")

	// Get the etcd client for cluster queries
	client, err := remote.GetClient()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get etcd client")
		if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
			logger.Error().Err(clearErr).Msg("failed to clear generated config")
		}
		return NewRemoteEtcd(cfg, logger), nil
	}

	// Collect HTTP endpoints for potential automatic rejoin
	httpEndpoints := collectHTTPEndpointsFromHosts(ctx, client, appCfg.HostID, logger)

	logger.Info().Msg("requesting server credentials via AddHost")

	// Request server credentials from the cluster
	creds, err := remote.AddHost(ctx, HostCredentialOptions{
		HostID:              appCfg.HostID,
		Hostname:            appCfg.Hostname,
		IPv4Address:         appCfg.IPv4Address,
		EmbeddedEtcdEnabled: true,
	})
	if err != nil {
		// AddHost failed - attempt automatic rejoin via HTTP
		logger.Warn().
			Err(err).
			Msg("failed to create server credentials - attempting automatic rejoin via HTTP")

		// Shutdown the remote client
		if shutdownErr := remote.Shutdown(); shutdownErr != nil {
			logger.Warn().Err(shutdownErr).Msg("failed to shutdown remote client during credential failure")
		}

		// Attempt automatic rejoin
		embedded, rejoinErr := attemptAutomaticRejoin(ctx, httpEndpoints, cfg, logger)
		if rejoinErr != nil {
			logger.Error().
				Err(rejoinErr).
				Msg("automatic rejoin failed - clearing config and returning to pre-init mode")

			if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
				logger.Error().Err(clearErr).Msg("failed to clear generated config")
			}

			logger.Info().Msg("returning to pre-initialization mode - use JoinCluster API with valid join token to rejoin")
			return NewRemoteEtcd(cfg, logger), nil
		}

		logger.Info().Msg("automatic rejoin successful - joined cluster as embedded etcd server")
		return embedded, nil
	}

	logger.Info().Msg("server credentials obtained, discovering cluster leader")

	// Get the current cluster leader
	leader, err := remote.Leader(ctx)
	if err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to discover etcd leader for client->server transition: %w", err)
	}

	logger.Info().
		Str("leader", leader.Name).
		Msg("leader discovered, joining as embedded etcd server")

	// Join the cluster as an embedded server
	embedded := NewEmbeddedEtcd(cfg, logger)
	if err := embedded.Join(ctx, JoinOptions{
		Leader:      leader,
		Credentials: creds,
	}); err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to join etcd cluster as embedded server during client->server transition: %w", err)
	}

	// Shutdown the remote client - we're now running as an embedded server
	if err := remote.Shutdown(); err != nil {
		logger.Warn().Err(err).Msg("failed to shutdown temporary remote etcd after client->server transition")
	}

	logger.Info().Msg("completed etcd client->server transition; embedded etcd has joined the cluster")

	return embedded, nil
}

// clearGeneratedConfig clears the generated configuration file, resetting the host
// to a pre-initialized state.
func clearGeneratedConfig(cfg *config.Manager, logger zerolog.Logger) error {
	appCfg := cfg.Config()
	desiredMode := appCfg.EtcdMode

	emptyConfig := config.Config{
		EtcdMode: desiredMode,
		EtcdClient: config.EtcdClient{
			Endpoints: nil,
		},
		EtcdUsername: "",
		EtcdPassword: "",
	}

	logger.Info().
		Str("desired_mode", string(desiredMode)).
		Msg("clearing generated config but preserving desired etcd mode")

	if err := cfg.UpdateGeneratedConfig(emptyConfig); err != nil {
		return fmt.Errorf("failed to update generated config: %w", err)
	}

	logger.Info().Msg("generated config cleared - host is now in pre-initialized state")
	return nil
}

// collectHTTPEndpointsFromHosts queries the etcd cluster for host information
// and constructs HTTP endpoints from stored host data.
func collectHTTPEndpointsFromHosts(ctx context.Context, client *clientv3.Client, thisHostID string, logger zerolog.Logger) []string {
	hostsResp, err := client.Get(ctx, "/hosts/", clientv3.WithPrefix())
	if err != nil {
		logger.Warn().Err(err).Msg("failed to query hosts for HTTP endpoints")
		return nil
	}

	if hostsResp.Count == 0 {
		logger.Warn().Msg("no hosts found in cluster for HTTP endpoint discovery")
		return nil
	}

	var httpEndpoints []string
	for _, kv := range hostsResp.Kvs {
		hostData, err := decompressData(kv.Value)
		if err != nil {
			logger.Warn().Err(err).Str("key", string(kv.Key)).Msg("failed to decompress host data")
			continue
		}

		// Extract host ID from key
		key := string(kv.Key)
		parts := strings.Split(key, "/")
		if len(parts) < 3 {
			continue
		}
		hostID := parts[2]

		// Skip this host
		if hostID == thisHostID {
			continue
		}

		// Parse host data
		var hostInfo struct {
			IPv4Address string `json:"ipv4_address"`
			HTTPPort    int    `json:"http_port"`
		}

		if err := json.Unmarshal(hostData, &hostInfo); err != nil {
			logger.Warn().Err(err).Str("host_id", hostID).Msg("failed to parse host data")
			continue
		}

		// Construct HTTP endpoint
		if hostInfo.IPv4Address != "" && hostInfo.HTTPPort > 0 {
			httpEndpoint := fmt.Sprintf("http://%s:%d", hostInfo.IPv4Address, hostInfo.HTTPPort)
			httpEndpoints = append(httpEndpoints, httpEndpoint)
			logger.Info().
				Str("host_id", hostID).
				Str("http_endpoint", httpEndpoint).
				Msg("constructed HTTP endpoint from host data")
		}
	}

	return httpEndpoints
}

// decompressData handles both compressed and uncompressed data.
func decompressData(in []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(in))
	if errors.Is(err, gzip.ErrHeader) {
		// Not compressed
		return in, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to initialize gzip reader: %w", err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}
	return out, nil
}

// attemptAutomaticRejoin tries to rejoin the cluster automatically using HTTP endpoints.
// It iterates through available HTTP endpoints, requests a join token, obtains credentials,
// and joins the etcd cluster as an embedded server.
func attemptAutomaticRejoin(
	ctx context.Context,
	httpEndpoints []string,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	if len(httpEndpoints) == 0 {
		return nil, fmt.Errorf("no HTTP endpoints available for automatic rejoin")
	}

	appCfg := cfg.Config()

	logger.Info().
		Str("host_id", appCfg.HostID).
		Int("endpoint_count", len(httpEndpoints)).
		Msg("attempting automatic rejoin via HTTP endpoints")

	// Try each HTTP endpoint until one succeeds
	var lastErr error
	for _, httpEndpoint := range httpEndpoints {
		logger.Info().
			Str("endpoint", httpEndpoint).
			Msg("trying to get credentials from cluster member")

		// Parse the HTTP endpoint
		serverURL, err := url.Parse(httpEndpoint)
		if err != nil {
			lastErr = fmt.Errorf("failed to parse HTTP endpoint: %w", err)
			continue
		}

		// Create HTTP client and API wrapper
		httpClient := &http.Client{Timeout: 30 * time.Second}
		apiClient := CreateAPIClient(serverURL, httpClient)

		// Get join token from the cluster member
		joinToken, err := apiClient.GetJoinToken(ctx)
		if err != nil {
			lastErr = fmt.Errorf("failed to get join token: %w", err)
			logger.Warn().Err(err).Str("endpoint", httpEndpoint).Msg("failed to get join token from endpoint")
			httpClient.CloseIdleConnections()
			continue
		}

		// Get join options with credentials for server mode
		joinOpts, err := apiClient.GetJoinOptions(ctx, &api.ClusterJoinRequest{
			HostID:              api.Identifier(appCfg.HostID),
			Hostname:            appCfg.Hostname,
			Ipv4Address:         appCfg.IPv4Address,
			Token:               joinToken.Token,
			EmbeddedEtcdEnabled: true, // server mode
		})
		if err != nil {
			lastErr = fmt.Errorf("failed to get join options: %w", err)
			logger.Warn().Err(err).Str("endpoint", httpEndpoint).Msg("failed to get join options from endpoint")
			httpClient.CloseIdleConnections()
			continue
		}

		// Successfully got credentials - decode and validate them
		etcdJoinOpts, err := DecodeJoinCredentials(joinOpts)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode credentials: %w", err)
			httpClient.CloseIdleConnections()
			continue
		}

		embedded := NewEmbeddedEtcd(cfg, logger)
		if err := embedded.Join(ctx, *etcdJoinOpts); err != nil {
			httpClient.CloseIdleConnections()
			return nil, fmt.Errorf("failed to join cluster: %w", err)
		}
		httpClient.CloseIdleConnections()
		logger.Info().Msg("successfully joined cluster as embedded etcd via automatic rejoin")
		return embedded, nil
	}

	return nil, fmt.Errorf("failed to rejoin via all endpoints: %w", lastErr)
}
