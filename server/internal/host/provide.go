package host

import (
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
			return nil, err
		}
		svc, err := do.Invoke[*Service](i)
		if err != nil {
			return nil, err
		}
		return NewUpdateTicker(logger, svc), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		embeddedEtcd, err := do.Invoke[*etcd.EmbeddedEtcd](i)
		if err != nil {
			return nil, err
		}
		store, err := do.Invoke[*Store](i)
		if err != nil {
			return nil, err
		}
		orchestrator, err := do.Invoke[Orchestrator](i)
		if err != nil {
			return nil, err
		}
		return NewService(cfg, embeddedEtcd, store, orchestrator), nil
	})
}

func provideStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Store, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		return NewStore(client, cfg.EtcdKeyRoot), nil
	})
}
