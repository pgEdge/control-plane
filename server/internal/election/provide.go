package election

import (
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func Provide(i *do.Injector) {
	provideElectionStore(i)
	provideService(i)
}

func provideElectionStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*ElectionStore, error) {
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		return NewElectionStore(client, cfg.EtcdKeyRoot), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		store, err := do.Invoke[*ElectionStore](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		return NewService(store, logger), nil
	})
}
