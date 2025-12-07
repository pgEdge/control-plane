package etcd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/grpclog"

	"github.com/pgEdge/control-plane/server/internal/config"
)

// Provide wires up Etcd, *clientv3.Client, and grpclog.LoggerV2 into the DI container.
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
		firstRun := isFirstRunDataDir(appCfg, logger)

		if firstRun {
			logger.Info().
				Str("host_id", appCfg.HostID).
				Str("data_dir", appCfg.DataDir).
				Str("etcd_mode", string(appCfg.EtcdMode)).
				Msg("first run detected – skipping etcd mode transition logic and not starting embedded etcd")
		} else {
			// Non–first run: safe to apply mode transition rules.
			if err := handleModeTransition(cfg, logger); err != nil {
				logger.Warn().Err(err).Msg("failed to handle etcd mode transition")
			}
		}

		switch appCfg.EtcdMode {
		case config.EtcdModeServer:
			embeddedEtcd := NewEmbeddedEtcd(cfg, logger)

			// On first run we DO NOT start embedded etcd automatically.
			// Something else (e.g. init-cluster handler) must call Start/Initialize
			// explicitly when you're ready to bootstrap the cluster.
			if !firstRun {
				go func() {
					ctx := context.Background()
					if err := embeddedEtcd.Start(ctx); err != nil {
						logger.Error().Err(err).Msg("failed to start embedded etcd")
					}
				}()
			}

			return embeddedEtcd, nil

		case config.EtcdModeClient:
			// Pure client mode – always RemoteEtcd, first-run or not.
			return NewRemoteEtcd(cfg, logger), nil

		default:
			return nil, fmt.Errorf("invalid etcd mode: %s", appCfg.EtcdMode)
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

//
// Data dir / etcd helpers
//

// isFirstRunDataDir returns true if the host data directory is missing or empty.
func isFirstRunDataDir(cfg config.Config, logger zerolog.Logger) bool {
	entries, err := os.ReadDir(cfg.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info().
				Str("host_id", cfg.HostID).
				Str("data_dir", cfg.DataDir).
				Msg("data directory does not exist – treating as first run")
			return true
		}
		logger.Warn().
			Err(err).
			Str("data_dir", cfg.DataDir).
			Msg("failed to read data directory – conservatively treating as first run")
		return true
	}
	if len(entries) == 0 {
		logger.Info().
			Str("host_id", cfg.HostID).
			Str("data_dir", cfg.DataDir).
			Msg("data directory is empty – treating as first run")
		return true
	}
	return false
}

// hasEtcdData returns true if this host already has etcd WAL data on disk.
func hasEtcdData(cfg config.Config) bool {
	etcdDir := filepath.Join(cfg.DataDir, "etcd")
	walDir := filepath.Join(etcdDir, "member", "wal")
	_, err := os.Stat(walDir)
	return err == nil
}

//
// Mode transition helpers
//

// attemptHostRemoval properly removes this host from the cluster when
// transitioning from server to client.
//
// Important: we deliberately bypass the Etcd.RemoveHost() API here because
// that method treats removing self as an error (ErrCannotRemoveSelf) for
// safety at the public API layer. For mode transitions we *do* want to
// remove this host's membership, so we call the lower-level membership
// helpers directly.
func attemptHostRemoval(cfg *config.Manager, logger zerolog.Logger) error {
	appCfg := cfg.Config()

	// Only attempt removal if we have cluster endpoints configured
	if len(appCfg.EtcdClient.Endpoints) == 0 {
		logger.Info().Msg("no cluster endpoints configured - skipping host removal")
		return nil
	}

	logger.Info().
		Str("host_id", appCfg.HostID).
		Strs("cluster_endpoints", appCfg.EtcdClient.Endpoints).
		Msg("attempting to remove host from cluster using etcd membership helpers")

	// Create a RemoteEtcd to get a TLS client + cert service
	remoteEtcd := NewRemoteEtcd(cfg, logger)

	const hostRemovalTimeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), hostRemovalTimeout)
	defer cancel()

	client, err := remoteEtcd.GetClient()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to create etcd client for host removal")
		return nil // don't block startup on this
	}

	if remoteEtcd.certSvc == nil {
		logger.Warn().Msg("no cert service available for host removal – skipping membership cleanup")
		return nil
	}

	// Call the lower-level helper directly. This:
	//   * removes the etcd member (if present)
	//   * removes the host's user/creds
	if err := RemoveHost(ctx, client, remoteEtcd.certSvc, appCfg.HostID); err != nil {
		// We still don't want to hard-fail startup on this; just log it.
		logger.Warn().Err(err).Msg("could not remove host from cluster (membership cleanup)")
		return nil
	}

	logger.Info().Msg("successfully removed host from cluster using membership helpers")
	return nil
}

// handleModeTransition detects and handles etcd mode transitions based on
// the configured EtcdMode and the presence of etcd data on disk.
//
// It focuses on two main transitions (and is only called on non–first run):
//
//   - server -> client:
//
//   - host was previously running an embedded server (WAL exists)
//
//   - new mode is client
//
//   - we remove it from the cluster (best effort) and delete local etcd dir
//
//   - client -> server (promotion on an existing cluster):
//
//   - host has no etcd WAL (so it's not an existing server instance)
//
//   - new mode is server
//
//   - etcd client endpoints are configured (cluster exists)
//
//   - we remove any stale membership by host_id and ensure local etcd dir is clean
//
// All other cases are a NO-OP.
func handleModeTransition(cfg *config.Manager, logger zerolog.Logger) error {
	appCfg := cfg.Config()
	etcdDir := filepath.Join(appCfg.DataDir, "etcd")

	hadEtcdData := hasEtcdData(appCfg)
	isServerMode := appCfg.EtcdMode == config.EtcdModeServer
	isClientMode := appCfg.EtcdMode == config.EtcdModeClient

	logger.Info().
		Str("host_id", appCfg.HostID).
		Str("etcd_mode", string(appCfg.EtcdMode)).
		Bool("had_etcd_data", hadEtcdData).
		Str("etcd_dir", etcdDir).
		Msg("evaluating etcd mode transition state")

	// --- Case 1: Server -> Client demotion ---
	if isClientMode && hadEtcdData {
		logger.Info().Msg("detected server->client transition – removing host from cluster and cleaning up local etcd data")

		if err := attemptHostRemoval(cfg, logger); err != nil {
			logger.Error().Err(err).Msg("failed to attempt host removal during server->client transition")
		}

		if err := os.RemoveAll(etcdDir); err != nil {
			return fmt.Errorf("failed to remove etcd data directory during server->client transition: %w", err)
		}

		logger.Info().Str("path", etcdDir).Msg("removed etcd data directory for client mode")
		return nil
	}

	// --- Case 2: Client -> Server promotion on an existing cluster ---
	if isServerMode && !hadEtcdData && len(appCfg.EtcdClient.Endpoints) > 0 {
		logger.Info().
			Strs("endpoints", appCfg.EtcdClient.Endpoints).
			Msg("detected client->server promotion on existing cluster – cleaning up stale membership and local data")

		if err := attemptHostRemoval(cfg, logger); err != nil {
			logger.Warn().Err(err).Msg("failed to remove stale host membership during client->server transition")
		}

		if err := os.RemoveAll(etcdDir); err != nil && !os.IsNotExist(err) {
			logger.Warn().
				Err(err).
				Str("path", etcdDir).
				Msg("failed to remove etcd data directory during client->server transition")
		} else {
			logger.Info().
				Str("path", etcdDir).
				Msg("ensured etcd data directory is clean for client->server transition")
		}

		return nil
	}

	logger.Info().Msg("no etcd mode transition actions required for this startup")
	return nil
}
