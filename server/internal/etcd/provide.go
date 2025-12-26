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
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/grpc/grpclog"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/server/internal/config"
)

func Provide(i *do.Injector) {
	provideEtcd(i)
	provideClient(i)
	provideGrpcLogger(i)
}

func provideClient(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*clientv3.Client, error) {
		etcd, err := do.Invoke[Etcd](i)
		if err != nil {
			return nil, err
		}
		return etcd.GetClient()
	})
}

// newEtcdForMode is the old behavior: pick implementation by current mode.
func newEtcdForMode(mode config.EtcdMode, cfg *config.Manager, logger zerolog.Logger) (Etcd, error) {
	switch mode {
	case config.EtcdModeServer:
		return NewEmbeddedEtcd(cfg, logger), nil
	case config.EtcdModeClient:
		return NewRemoteEtcd(cfg, logger), nil
	default:
		return nil, fmt.Errorf("invalid etcd mode: %s", mode)
	}
}

func provideEtcd(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (Etcd, error) {
		cfg, err := do.Invoke[*config.Manager](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}

		appCfg := cfg.Config()
		generated := cfg.GeneratedConfig()

		oldMode := generated.EtcdMode
		newMode := appCfg.EtcdMode

		logger.Info().
			Str("old_mode", string(oldMode)).
			Str("new_mode", string(newMode)).
			Bool("old_mode_empty", oldMode == "").
			Bool("modes_equal", oldMode == newMode).
			Msg("checking etcd mode for reconfiguration")

		// First startup (no generated config yet) or no change: behave as before.
		if oldMode == "" || oldMode == newMode {
			logger.Info().
				Str("mode", string(newMode)).
				Bool("first_startup", oldMode == "").
				Msg("creating new etcd instance for mode (no reconfiguration needed)")
			return newEtcdForMode(newMode, cfg, logger)
		}

		// There *was* a previous mode and it's different now: perform transition.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		logger.Info().
			Str("host_id", appCfg.HostID).
			Str("old_mode", string(oldMode)).
			Str("new_mode", string(newMode)).
			Msg("detected etcd_mode change, performing reconfiguration")

		switch {
		case oldMode == config.EtcdModeServer && newMode == config.EtcdModeClient:
			return reconfigureServerToClient(ctx, cfg, logger)

		case oldMode == config.EtcdModeClient && newMode == config.EtcdModeServer:
			return reconfigureClientToServer(ctx, cfg, logger)

		default:
			return nil, fmt.Errorf("unsupported etcd mode transition: %s -> %s", oldMode, newMode)
		}
	})
}

func provideGrpcLogger(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (grpclog.LoggerV2, error) {
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		return newGrpcLogger(logger), nil
	})
}

// ---------- Transition helpers ----------

// server -> client
//
// We were running an embedded etcd server on this host, but config now says
// EtcdModeClient. We need to:
//   - start embedded etcd using the existing data dir
//   - remove this host's member from the cluster
//   - discover remaining members' client URLs and persist as EtcdClient.Endpoints
//   - flip GeneratedConfig.EtcdMode to client
//   - return a RemoteEtcd (which will use those endpoints)
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
		generated.EtcdServerInitialized = false
		if err := cfg.UpdateGeneratedConfig(generated); err != nil {
			return nil, fmt.Errorf("failed to update generated config for server->client (uninitialized) transition: %w", err)
		}

		return NewRemoteEtcd(cfg, logger), nil
	}

	// Connect to cluster using existing credentials
	// We need to get the client URLs from the local embedded etcd's cluster config
	logger.Info().Msg("getting cluster member list to find remote endpoints")

	// Create a temporary client connection using the existing server credentials
	// We'll connect to localhost since we're still running as a server
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

	// Persist new mode + remote endpoints; keep username/password and HTTPEndpoints
	generated.EtcdMode = appCfg.EtcdMode
	generated.EtcdClient.Endpoints = endpoints
	// Preserve HTTPEndpoints - they're still needed for potential future transitions
	// generated.EtcdClient.HTTPEndpoints stays as is
	generated.EtcdServerInitialized = false
	if err := cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to update generated config after server->client transition: %w", err)
	}

	logger.Info().
		Strs("endpoints", endpoints).
		Msg("completed etcd server->client transition; using remaining cluster members as remote endpoints")

	// Return a new RemoteEtcd client for the demoted host
	return NewRemoteEtcd(cfg, logger), nil
}

// decompressData handles both compressed and uncompressed data
// If data is gzip compressed, it decompresses it. Otherwise returns as-is.
func decompressData(in []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(in))
	if errors.Is(err, gzip.ErrHeader) {
		// The gzip.NewReader checks for a valid header. If there is no valid
		// header, this data is not compressed.
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

// client -> server
//
// We were a pure remote etcd client, but config now says EtcdModeServer.
// We need to:
//   - connect to the existing cluster as a remote client using existing credentials
//   - create host credentials with EmbeddedEtcdEnabled=true via AddHost()
//   - discover the leader via Leader()
//   - join as an embedded server (learner -> promoted member)
//   - update GeneratedConfig via join (writeHostCredentials)
//   - return an EmbeddedEtcd
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
		// Without HTTP API, we cannot automatically recover
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

	// We have an authenticated client - query cluster state using raw etcd operations
	client, err := remote.GetClient()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get etcd client")
		if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
			logger.Error().Err(clearErr).Msg("failed to clear generated config")
		}
		return NewRemoteEtcd(cfg, logger), nil
	}

	// Query existing hosts in the cluster for informational purposes
	hostsKeyPrefix := "/hosts/"
	logger.Info().
		Str("key_prefix", hostsKeyPrefix).
		Msg("querying hosts from etcd")

	hostsResp, err := client.Get(ctx, hostsKeyPrefix, clientv3.WithPrefix())
	if err != nil {
		logger.Warn().
			Err(err).
			Str("key_prefix", hostsKeyPrefix).
			Msg("failed to query hosts from cluster")
	} else if hostsResp.Count == 0 {
		logger.Warn().
			Str("key_prefix", hostsKeyPrefix).
			Msg("no hosts found in cluster - cluster may be empty or host data not yet populated")
	} else {
		logger.Info().Int64("count", hostsResp.Count).Msg("found existing hosts in cluster")
	}

	// Query existing databases
	dbsResp, err := client.Get(ctx, "/databases/", clientv3.WithPrefix())
	if err == nil && dbsResp.Count > 0 {
		logger.Info().Int64("count", dbsResp.Count).Msg("found existing databases in cluster")
	}

	// Query existing instances
	instancesResp, err := client.Get(ctx, "/instances/", clientv3.WithPrefix())
	if err == nil && instancesResp.Count > 0 {
		logger.Info().Int64("count", instancesResp.Count).Msg("found existing instances in cluster")
	}

	logger.Info().Msg("completed cluster information query")

	// Check if THIS host already has an entry in the cluster
	// If it does, the host was previously part of the cluster but credentials may be incomplete
	thisHostKey := fmt.Sprintf("/hosts/%s", appCfg.HostID)
	thisHostResp, err := client.Get(ctx, thisHostKey)
	if err != nil {
		logger.Warn().Err(err).Str("key", thisHostKey).Msg("failed to check if this host exists in cluster")
	} else if thisHostResp.Count > 0 {
		logger.Warn().
			Str("host_id", appCfg.HostID).
			Msg("this host already has an entry in cluster - will attempt to remove and rejoin")

		// Try to remove the existing host entry
		// This is necessary when the host has stale/incomplete credentials
		logger.Info().Msg("attempting to remove existing host entry from cluster")
		if err := remote.RemoveHost(ctx, appCfg.HostID); err != nil {
			logger.Warn().
				Err(err).
				Msg("failed to remove existing host entry - will continue with AddHost anyway")
		} else {
			logger.Info().Msg("successfully removed existing host entry from cluster")
		}
	} else {
		logger.Info().
			Str("host_id", appCfg.HostID).
			Msg("this host does not have an entry in cluster yet")
	}

	// First, collect HTTP endpoints from the hosts in the cluster for potential rejoin
	// We need these BEFORE attempting AddHost, in case it fails
	httpEndpoints := collectHTTPEndpointsFromHosts(ctx, client, appCfg.HostID, logger)

	logger.Info().Msg("requesting server credentials via AddHost")

	// Ask the existing cluster to create server credentials for this host.
	// This is the same flow as GetJoinOptions in the API.
	creds, err := remote.AddHost(ctx, HostCredentialOptions{
		HostID:              appCfg.HostID,
		Hostname:            appCfg.Hostname,
		IPv4Address:         appCfg.IPv4Address,
		EmbeddedEtcdEnabled: true,
	})
	if err != nil {
		// AddHost failed even after attempting to remove the existing entry
		// This means the credentials truly lack the required permissions
		//
		// Strategy: Attempt automatic rejoin via HTTP endpoints
		// This replicates the JoinCluster API flow automatically
		logger.Warn().
			Err(err).
			Msg("failed to create server credentials - attempting automatic rejoin via HTTP")

		// Shutdown the remote client with insufficient credentials
		if shutdownErr := remote.Shutdown(); shutdownErr != nil {
			logger.Warn().Err(shutdownErr).Msg("failed to shutdown remote client during credential failure")
		}

		// Attempt automatic rejoin using HTTP endpoints
		embedded, rejoinErr := attemptAutomaticRejoin(ctx, httpEndpoints, cfg, logger)
		if rejoinErr != nil {
			logger.Error().
				Err(rejoinErr).
				Msg("automatic rejoin failed - clearing config and returning to pre-init mode")

			// Clear the insufficient credentials
			if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
				logger.Error().Err(clearErr).Msg("failed to clear generated config")
			}

			// Return to pre-initialization mode - manual intervention required
			logger.Info().Msg("returning to pre-initialization mode - use JoinCluster API with valid join token to rejoin")
			return NewRemoteEtcd(cfg, logger), nil
		}

		logger.Info().Msg("automatic rejoin successful - joined cluster as embedded etcd server")
		return embedded, nil
	}

	logger.Info().Msg("server credentials obtained, discovering cluster leader")

	// Get the current cluster leader information.
	leader, err := remote.Leader(ctx)
	if err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to discover etcd leader for client->server transition: %w", err)
	}

	logger.Info().
		Str("leader", leader.Name).
		Msg("leader discovered, joining as embedded etcd server")

	// Create embedded etcd and join the cluster as a server.
	// Join() automatically handles the entire process:
	//   - writes credentials (including server certs) to disk via writeHostCredentials()
	//   - connects to the leader and adds this host as a learner via MemberAddAsLearner()
	//   - builds initial cluster configuration from member list
	//   - starts embedded etcd as a learner
	//   - promotes to voting member when ready
	//   - updates GeneratedConfig with new username/password/mode
	embedded := NewEmbeddedEtcd(cfg, logger)
	if err := embedded.Join(ctx, JoinOptions{
		Leader:      leader,
		Credentials: creds,
	}); err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to join etcd cluster as embedded server during client->server transition: %w", err)
	}

	// Shutdown the remote client connection - we're now running as an embedded server.
	if err := remote.Shutdown(); err != nil {
		logger.Warn().Err(err).Msg("failed to shutdown temporary remote etcd after client->server transition")
	}

	logger.Info().Msg("completed etcd client->server transition; embedded etcd has joined the cluster")

	// From this point forward, this host uses embedded etcd.
	return embedded, nil
}

// clearGeneratedConfig clears the generated configuration file, resetting the host
// to a pre-initialized state. This is used when credentials become invalid and the
// host needs to rejoin the cluster from scratch.
func clearGeneratedConfig(cfg *config.Manager, logger zerolog.Logger) error {
	// Get the current user-configured mode - we want to preserve this intent
	appCfg := cfg.Config()
	desiredMode := appCfg.EtcdMode

	// Create an empty config to clear credentials but preserve the desired mode
	emptyConfig := config.Config{
		EtcdMode: desiredMode, // Keep the user's intended mode
		EtcdClient: config.EtcdClient{
			Endpoints: nil, // Clear endpoints
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
// and constructs HTTP endpoints from the stored host data (IPv4Address + HTTPPort)
func collectHTTPEndpointsFromHosts(ctx context.Context, client *clientv3.Client, thisHostID string, logger zerolog.Logger) []string {
	// Query all hosts from etcd
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
		// Parse the host data
		hostData, err := decompressData(kv.Value)
		if err != nil {
			logger.Warn().Err(err).Str("key", string(kv.Key)).Msg("failed to decompress host data")
			continue
		}

		// Extract host ID from the key (format: /hosts/{host-id})
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

		// Parse IPv4 address and HTTP port from the host data
		// Host data is stored as JSON with fields: ipv4_address and http_port
		var hostInfo struct {
			IPv4Address string `json:"ipv4_address"`
			HTTPPort    int    `json:"http_port"`
		}

		if err := json.Unmarshal(hostData, &hostInfo); err != nil {
			logger.Warn().Err(err).Str("host_id", hostID).Msg("failed to parse host data")
			continue
		}

		// Construct HTTP endpoint from IPv4 address and HTTP port
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

// attemptAutomaticRejoin tries to rejoin the cluster automatically using HTTP endpoints
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

		// Create HTTP client
		httpClient := &http.Client{Timeout: 30 * time.Second}
		enc := goahttp.RequestEncoder
		dec := goahttp.ResponseDecoder
		c := client.NewClient(serverURL.Scheme, serverURL.Host, httpClient, enc, dec, false)
		cli := &api.Client{
			GetJoinTokenEndpoint:   c.GetJoinToken(),
			GetJoinOptionsEndpoint: c.GetJoinOptions(),
		}

		// Get join token
		joinToken, err := cli.GetJoinToken(ctx)
		if err != nil {
			lastErr = fmt.Errorf("failed to get join token: %w", err)
			logger.Warn().Err(err).Str("endpoint", httpEndpoint).Msg("failed to get join token from endpoint")
			continue
		}

		// Get join options with credentials
		joinOpts, err := cli.GetJoinOptions(ctx, &api.ClusterJoinRequest{
			HostID:              api.Identifier(appCfg.HostID),
			Hostname:            appCfg.Hostname,
			Ipv4Address:         appCfg.IPv4Address,
			Token:               joinToken.Token,
			EmbeddedEtcdEnabled: true, // server mode
		})
		if err != nil {
			lastErr = fmt.Errorf("failed to get join options: %w", err)
			logger.Warn().Err(err).Str("endpoint", httpEndpoint).Msg("failed to get join options from endpoint")
			continue
		}

		// Successfully got credentials - decode them
		caCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.CaCert)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode CA certificate: %w", err)
			continue
		}
		clientCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientCert)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode client certificate: %w", err)
			continue
		}
		clientKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientKey)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode client key: %w", err)
			continue
		}
		serverCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerCert)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode server certificate: %w", err)
			continue
		}
		serverKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerKey)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode server key: %w", err)
			continue
		}

		logger.Info().
			Str("leader", joinOpts.Leader.Name).
			Msg("received credentials via HTTP - joining cluster as embedded etcd")

		// Create JoinOptions for embedded etcd
		etcdJoinOpts := JoinOptions{
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
		}

		// Create embedded etcd and join the cluster
		embedded := NewEmbeddedEtcd(cfg, logger)
		if err := embedded.Join(ctx, etcdJoinOpts); err != nil {
			return nil, fmt.Errorf("failed to join cluster as embedded etcd: %w", err)
		}

		logger.Info().Msg("successfully joined cluster as embedded etcd via automatic rejoin")
		return embedded, nil
	}

	return nil, fmt.Errorf("failed to rejoin via all endpoints: %w", lastErr)
}
