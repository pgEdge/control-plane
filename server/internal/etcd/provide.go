package etcd

import (
	"fmt"

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

		switch storageType := cfg.Config().StorageType; storageType {
		case config.StorageTypeEmbeddedEtcd:
			return NewEmbeddedEtcd(cfg, logger), nil
		case config.StorageTypeRemoteEtcd:
			return NewRemoteEtcd(cfg, logger), nil
		default:
			return nil, fmt.Errorf("invalid storage type: %s", storageType)
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
