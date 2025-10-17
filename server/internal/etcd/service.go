package etcd

import (
	"fmt"

	"github.com/rs/zerolog"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
)

type Service struct {
	cfg      *config.Manager
	logger   zerolog.Logger
	embedded *EmbeddedEtcd
	client   *clientv3.Client
}

func NewService(cfg *config.Manager, logger zerolog.Logger) (*Service, error) {
	appCfg := cfg.Config()
	switch appCfg.StorageType {
	case config.StorageTypeEmbeddedEtcd:
		return &Service{
			cfg:      cfg,
			logger:   logger,
			embedded: NewEmbeddedEtcd(cfg, logger),
		}, nil
	case config.StorageTypeRemoteEtcd:
		client, err := NewRemoteClient(appCfg, logger)
		if err != nil {
			return nil, err
		}
		return &Service{
			cfg:    cfg,
			logger: logger,
			client: client,
		}, nil
	default:
		return nil, fmt.Errorf("unrecognized storage type %q", appCfg.StorageType)
	}
}
