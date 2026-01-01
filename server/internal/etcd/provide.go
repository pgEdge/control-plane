package etcd

import (
	"context"
	"fmt"
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

// newEtcdForMode creates an Etcd instance based on the specified mode.
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

		// First startup (no generated config yet) or no change: use the configured mode.
		if oldMode == "" || oldMode == newMode {
			logger.Info().
				Str("mode", string(newMode)).
				Bool("first_startup", oldMode == "").
				Msg("creating new etcd instance for mode (no reconfiguration needed)")
			return newEtcdForMode(newMode, cfg, logger)
		}

		// Mode has changed - perform reconfiguration.
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
