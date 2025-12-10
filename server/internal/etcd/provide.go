package etcd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/grpclog"

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
//   - connect to the existing cluster as a remote client
//   - create host credentials with EmbeddedEtcdEnabled=true
//   - discover the leader
//   - join as an embedded server (learner -> promoted member)
//   - update GeneratedConfig via join (writeHostCredentials)
//   - return an EmbeddedEtcd
func reconfigureClientToServer(
	ctx context.Context,
	cfg *config.Manager,
	logger zerolog.Logger,
) (Etcd, error) {
	appCfg := cfg.Config()

	// "Old" view is as a remote client using persisted endpoints.
	remote := NewRemoteEtcd(cfg, logger)

	// RemoteEtcd.IsInitialized() uses EtcdClient.Endpoints, which should be
	// populated from the previous mode.
	if err := remote.Start(ctx); err != nil {
		if strings.Contains(err.Error(), "authentication failed") {
			return nil, fmt.Errorf(
				"etcd client->server transition failed for host %q: existing etcd credentials are not valid in the etcd cluster. "+
					"This usually means the host was previously removed or its credentials were lost. "+
					"Rejoin the host as a client (fresh generated.config.json) and then retry the mode change: %w",
				appCfg.HostID, err,
			)
		}
		return nil, fmt.Errorf("failed to start remote etcd for client->server transition: %w", err)
	}

	// Ask the existing cluster to create credentials for this host, including
	// embedded-server certs.
	creds, err := remote.AddHost(ctx, HostCredentialOptions{
		HostID:              appCfg.HostID,
		Hostname:            appCfg.Hostname,
		IPv4Address:         appCfg.IPv4Address,
		EmbeddedEtcdEnabled: true,
	})
	if err != nil {
		if strings.Contains(err.Error(), "authentication failed") {
			return nil, fmt.Errorf(
				"etcd client->server transition failed for host %q: existing etcd credentials are not valid in the etcd cluster. "+
					"This usually means the host was previously removed or its credentials were lost. "+
					"Rejoin the host as a client (fresh generated.config.json) and then retry the mode change: %w",
				appCfg.HostID, err,
			)
		}

		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to create host credentials for client->server transition: %w", err)
	}

	leader, err := remote.Leader(ctx)
	if err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to discover etcd leader for client->server transition: %w", err)
	}

	// Now create the "new" embedded etcd object and join the cluster as a
	// learner. Join() will:
	//   - write host credentials (including server certs) to disk
	//   - start embedded as a learner
	//   - promote when ready
	embedded := NewEmbeddedEtcd(cfg, logger)
	if err := embedded.Join(ctx, JoinOptions{
		Leader:      leader,
		Credentials: creds,
	}); err != nil {
		_ = remote.Shutdown()
		return nil, fmt.Errorf("failed to join etcd cluster as embedded server during client->server transition: %w", err)
	}

	// Join() already called writeHostCredentials(), which:
	//   - writes certs to DataDir/certificates
	//   - updates GeneratedConfig.EtcdUsername / EtcdPassword
	//   - sets GeneratedConfig.EtcdMode = cfg.Config().EtcdMode (now "server")
	// so we don't need to touch GeneratedConfig here.

	if err := remote.Shutdown(); err != nil {
		logger.Warn().Err(err).Msg("failed to shutdown temporary remote etcd after client->server transition")
	}

	logger.Info().Msg("completed etcd client->server transition; embedded etcd has joined the cluster")

	// From this point forward, this host uses embedded etcd.
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
