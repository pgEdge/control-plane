package etcd

import (
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/grpclog"

	"github.com/pgEdge/control-plane/server/internal/config"
)

func Provide(i *do.Injector) {
	provideEmbeddedEtcd(i)
	provideClient(i)
	provideGrpcLogger(i)
}

func provideClient(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*clientv3.Client, error) {
		etcd, err := do.Invoke[*EmbeddedEtcd](i)
		if err != nil {
			return nil, err
		}
		return etcd.GetClient()
	})
}

func provideEmbeddedEtcd(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*EmbeddedEtcd, error) {
		cfg, err := do.Invoke[*config.Manager](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		return NewEmbeddedEtcd(cfg, logger), nil
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
