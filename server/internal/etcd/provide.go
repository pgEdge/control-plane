package etcd

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
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

	embedded := NewEmbeddedEtcd(cfg, logger)

	initialized, err := embedded.IsInitialized()
	if err != nil {
		return nil, fmt.Errorf("failed to check embedded etcd initialization during server->client transition: %w", err)
	}

	// If etcd was never initialized, there's nothing to demote – just persist
	// the new mode and come up as a client. The client won't be able to Start
	// until it has endpoints, which is consistent with current behavior.
	if !initialized {
		logger.Info().Msg("embedded etcd not initialized, skipping server->client demotion")

		generated := cfg.GeneratedConfig()
		generated.EtcdMode = appCfg.EtcdMode
		generated.EtcdServerInitialized = false
		if err := cfg.UpdateGeneratedConfig(generated); err != nil {
			return nil, fmt.Errorf("failed to update generated config for server->client (uninitialized) transition: %w", err)
		}

		return NewRemoteEtcd(cfg, logger), nil
	}

	// Start local embedded etcd so we can talk to the cluster as the "old" node.
	if err := embedded.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start embedded etcd for server->client transition: %w", err)
	}
	client, err := embedded.GetClient()
	if err != nil {
		_ = shutdownEtcd(embedded)
		return nil, fmt.Errorf("failed to get embedded etcd client for server->client transition: %w", err)
	}

	// Get the full member list *before* removing this host, so we can build
	// a list of remote endpoints.
	resp, err := client.MemberList(ctx)
	if err != nil {
		_ = shutdownEtcd(embedded)
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
		_ = shutdownEtcd(embedded)
		return nil, fmt.Errorf("cannot demote etcd server on host %s: no remaining cluster members with client URLs", appCfg.HostID)
	}

	// Remove this host's etcd member from the cluster. We intentionally call
	// RemoveMember instead of RemoveHost here so that we *do not* remove the
	// host user / credentials – we still need them as a remote client.
	if err := RemoveMember(ctx, client, appCfg.HostID); err != nil {
		_ = shutdownEtcd(embedded)
		return nil, fmt.Errorf("failed to remove this host from etcd cluster during server->client transition: %w", err)
	}

	if err := shutdownEtcd(embedded); err != nil {
		logger.Warn().Err(err).Msg("failed to shutdown embedded etcd after server->client transition")
	}

	// Persist new mode + remote endpoints; keep username/password the same.
	generated := cfg.GeneratedConfig()
	generated.EtcdMode = appCfg.EtcdMode
	generated.EtcdClient.Endpoints = endpoints
	if err := cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to update generated config after server->client transition: %w", err)
	}

	logger.Info().
		Strs("endpoints", endpoints).
		Msg("completed etcd server->client transition; using remaining cluster members as remote endpoints")

	// From this point on, the host behaves as a remote etcd client.
	return NewRemoteEtcd(cfg, logger), nil
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
	// These credentials should be valid if the host is still part of the cluster.
	remote := NewRemoteEtcd(cfg, logger)

	logger.Info().Msg("starting remote etcd client with existing credentials")
	if err := remote.Start(ctx); err != nil {
		// If authentication fails, the host was likely removed from the cluster.
		// Try to automatically rejoin using the known cluster endpoints.
		logger.Warn().
			Err(err).
			Msg("failed to authenticate with existing credentials - attempting automatic rejoin")

		// Attempt automatic rejoin
		embedded, rejoinErr := autoRejoinCluster(ctx, cfg, logger)
		if rejoinErr != nil {
			logger.Error().Err(rejoinErr).Msg("automatic rejoin failed")

			// Clear config as fallback
			if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
				logger.Error().Err(clearErr).Msg("failed to clear generated config")
			}

			return nil, fmt.Errorf("failed to automatically rejoin cluster after auth failure: %w", rejoinErr)
		}

		logger.Info().Msg("successfully rejoined cluster automatically")
		return embedded, nil
	}

	logger.Info().Msg("remote etcd client started, requesting server credentials via AddHost")

	// Ask the existing cluster to create server credentials for this host.
	// This is the same flow as GetJoinOptions in the API.
	creds, err := remote.AddHost(ctx, HostCredentialOptions{
		HostID:              appCfg.HostID,
		Hostname:            appCfg.Hostname,
		IPv4Address:         appCfg.IPv4Address,
		EmbeddedEtcdEnabled: true,
	})
	if err != nil {
		// If AddHost fails, we still have a working remote connection.
		// Try to remove the old host entry and re-add it with server credentials.
		logger.Warn().
			Err(err).
			Msg("failed to create server credentials - attempting to remove and re-add host")

		// Remove the old host entry
		if removeErr := remote.RemoveHost(ctx, appCfg.HostID); removeErr != nil {
			logger.Warn().
				Err(removeErr).
				Msg("failed to remove old host entry - will try automatic rejoin")

			_ = remote.Shutdown()

			// Fall back to automatic rejoin via HTTP
			embedded, rejoinErr := autoRejoinCluster(ctx, cfg, logger)
			if rejoinErr != nil {
				logger.Error().Err(rejoinErr).Msg("automatic rejoin failed")

				if clearErr := clearGeneratedConfig(cfg, logger); clearErr != nil {
					logger.Error().Err(clearErr).Msg("failed to clear generated config")
				}

				return nil, fmt.Errorf("failed to automatically rejoin cluster: %w", rejoinErr)
			}

			logger.Info().Msg("successfully rejoined cluster automatically")
			return embedded, nil
		}

		logger.Info().Msg("old host entry removed, re-adding with server credentials")

		// Try AddHost again after removing the old entry
		creds, err = remote.AddHost(ctx, HostCredentialOptions{
			HostID:              appCfg.HostID,
			Hostname:            appCfg.Hostname,
			IPv4Address:         appCfg.IPv4Address,
			EmbeddedEtcdEnabled: true,
		})
		if err != nil {
			_ = remote.Shutdown()
			return nil, fmt.Errorf("failed to re-add host after removal: %w", err)
		}

		logger.Info().Msg("host successfully re-added with server credentials")
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
	// Create an empty config to clear all fields
	emptyConfig := config.Config{
		EtcdMode: "", // Clear the mode
		EtcdClient: config.EtcdClient{
			Endpoints: nil, // Clear endpoints
		},
		EtcdUsername: "",
		EtcdPassword: "",
	}

	if err := cfg.UpdateGeneratedConfig(emptyConfig); err != nil {
		return fmt.Errorf("failed to update generated config: %w", err)
	}

	logger.Info().Msg("generated config cleared - host is now in pre-initialized state")
	return nil
}

// autoRejoinCluster attempts to automatically rejoin the cluster when credentials are invalid.
// It tries to contact known cluster members via HTTP to get a join token and rejoin.
func autoRejoinCluster(
	ctx context.Context,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	appCfg := cfg.Config()
	generated := cfg.GeneratedConfig()

	logger.Info().
		Str("host_id", appCfg.HostID).
		Msg("attempting automatic cluster rejoin")

	// Save HTTP endpoints BEFORE clearing the config
	httpEndpoints := generated.EtcdClient.HTTPEndpoints
	if httpEndpoints == nil || len(httpEndpoints) == 0 {
		return nil, fmt.Errorf("no HTTP endpoints available for rejoin")
	}

	// Clear the generated config
	if err := clearGeneratedConfig(cfg, logger); err != nil {
		return nil, fmt.Errorf("failed to clear generated config: %w", err)
	}

	// Try each stored HTTP endpoint to get join token
	var lastErr error
	for _, httpEndpoint := range httpEndpoints {
		logger.Info().
			Str("http_endpoint", httpEndpoint).
			Msg("trying to get join token from cluster member")

		// Try to rejoin via this endpoint
		embedded, err := rejoinViaHTTP(ctx, httpEndpoint, cfg, logger)
		if err != nil {
			logger.Warn().
				Err(err).
				Str("http_endpoint", httpEndpoint).
				Msg("failed to rejoin via this endpoint")
			lastErr = err
			continue
		}

		logger.Info().
			Str("http_endpoint", httpEndpoint).
			Msg("successfully rejoined cluster")
		return embedded, nil
	}

	return nil, fmt.Errorf("failed to rejoin cluster after trying all endpoints: %w", lastErr)
}

// rejoinViaHTTP attempts to rejoin the cluster by getting a join token via HTTP
func rejoinViaHTTP(
	ctx context.Context,
	httpEndpoint string,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	appCfg := cfg.Config()

	// Parse the HTTP endpoint
	serverURL, err := url.Parse(httpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTTP endpoint: %w", err)
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
	logger.Info().Msg("requesting join token from cluster")
	joinToken, err := cli.GetJoinToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get join token: %w", err)
	}

	logger.Info().
		Str("server_url", joinToken.ServerURL).
		Msg("received join token")

	// Get join options with embedded etcd enabled (for server mode)
	logger.Info().Msg("requesting join options")
	joinOpts, err := cli.GetJoinOptions(ctx, &api.ClusterJoinRequest{
		HostID:              api.Identifier(appCfg.HostID),
		Hostname:            appCfg.Hostname,
		Ipv4Address:         appCfg.IPv4Address,
		Token:               joinToken.Token,
		EmbeddedEtcdEnabled: true, // We want to join as server
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get join options: %w", err)
	}

	logger.Info().
		Str("leader", joinOpts.Leader.Name).
		Msg("received join options")

	// Decode credentials
	caCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.CaCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA cert: %w", err)
	}
	clientCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client cert: %w", err)
	}
	clientKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client key: %w", err)
	}
	serverCert, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerCert)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server cert: %w", err)
	}
	serverKey, err := base64.StdEncoding.DecodeString(joinOpts.Credentials.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server key: %w", err)
	}

	// Create embedded etcd and join
	embedded := NewEmbeddedEtcd(cfg, logger)
	if err := embedded.Join(ctx, JoinOptions{
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
		HTTPEndpoints: joinOpts.HTTPEndpoints,
	}); err != nil {
		return nil, fmt.Errorf("failed to join cluster: %w", err)
	}

	return embedded, nil
}

// shutdownEtcd best-effort shuts down either embedded or remote etcd,
// depending on which concrete type we actually have.
func shutdownEtcd(e Etcd) error {
	switch v := e.(type) {
	case *EmbeddedEtcd:
		return v.Shutdown()
	case *RemoteEtcd:
		return v.Shutdown()
	default:
		return nil
	}
}
