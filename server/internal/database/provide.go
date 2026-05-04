package database

import (
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/ports"
)

func Provide(i *do.Injector) {
	provideStore(i)
	provideService(i)
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		orch, err := do.Invoke[Orchestrator](i)
		if err != nil {
			return nil, err
		}
		store, err := do.Invoke[*Store](i)
		if err != nil {
			return nil, err
		}
		hostSvc, err := do.Invoke[*host.Service](i)
		if err != nil {
			return nil, err
		}
		portsSvc, err := do.Invoke[*ports.Service](i)
		if err != nil {
			return nil, err
		}
		loggerFactory, err := do.Invoke[*logging.Factory](i)
		if err != nil {
			return nil, err
		}
		return NewService(cfg, orch, store, hostSvc, portsSvc, loggerFactory), nil
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
