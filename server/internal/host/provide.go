package host

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
)

func Provide(i *do.Injector) {
	provideStore(i)
	provideService(i)
	provideTicker(i)
}

func provideTicker(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*UpdateTicker, error) {
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		svc, err := do.Invoke[*Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get host service: %w", err)
		}
		return NewUpdateTicker(logger, svc), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		e, err := do.Invoke[etcd.Etcd](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get etcd: %w", err)
		}
		store, err := do.Invoke[*Store](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get host store: %w", err)
		}
		orchestrator, err := do.Invoke[Orchestrator](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get orchestrator: %w", err)
		}
		return NewService(cfg, e, store, orchestrator), nil
	})
}

func provideStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Store, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get etcd client: %w", err)
		}
		return NewStore(client, cfg.EtcdKeyRoot), nil
	})
}
