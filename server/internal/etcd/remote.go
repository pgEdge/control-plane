package etcd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
)

type RemoteEtcd struct {
	client *clientv3.Client
	logger zerolog.Logger
	cfg    config.Config
	errCh  chan error
}

// func RegisterRemoteEtcd(i *do.Injector) {
// 	do.Provide(i, func(i *do.Injector) (*Client, error) {
// 		mgr, err := do.Invoke[*config.Manager](i)
// 		if err != nil {
// 			return nil, err
// 		}
// 		logger, err := do.Invoke[zerolog.Logger](i)
// 		if err != nil {
// 			return nil, err
// 		}
// 		return NewRemoteEtcd(mgr.Config(), logger)
// 	})
// }

func NewRemoteClient(cfg config.Config, logger zerolog.Logger) (*clientv3.Client, error) {
	zapLogger, err := newZapLogger(logger, cfg.RemoteEtcd.LogLevel, "etcd_client")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client logger: %w", err)
	}

	// AutoSyncInterval determines how often the client will sync the list of
	// known endpoints from the cluster. The client will automatically load
	// balance and failover between endpoints, but syncs are desirable for
	// permanent membership changes.
	client, err := clientv3.New(clientv3.Config{
		Endpoints:        cfg.RemoteEtcd.Endpoints,
		Logger:           zapLogger,
		AutoSyncInterval: 5 * time.Minute,
		DialTimeout:      5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client: %w", err)
	}

	return client, nil
}

func NewRemoteEtcd(cfg config.Config, logger zerolog.Logger) (*RemoteEtcd, error) {
	zapLogger, err := newZapLogger(logger, cfg.RemoteEtcd.LogLevel, "etcd_client")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client logger: %w", err)
	}

	// AutoSyncInterval determines how often the client will sync the list of
	// known endpoints from the cluster. The client will automatically load
	// balance and failover between endpoints, but syncs are desirable for
	// permanent membership changes.
	client, err := clientv3.New(clientv3.Config{
		Endpoints:        cfg.RemoteEtcd.Endpoints,
		Logger:           zapLogger,
		AutoSyncInterval: 5 * time.Minute,
		DialTimeout:      5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client: %w", err)
	}

	return &RemoteEtcd{
		client: client,
		logger: logger,
		cfg:    cfg,
		errCh:  make(chan error, 1),
	}, nil
}

func (e *RemoteEtcd) IsInitialized() (bool, error) {
	return true, nil
}

func (e *RemoteEtcd) Start(ctx context.Context) error {
	return nil
}

func (e *RemoteEtcd) Join(ctx context.Context, options JoinOptions) error {
	return errors.New("join is not supported for remote etcd")
}

func (e *RemoteEtcd) Shutdown() error {
	if err := e.client.Close(); err != nil {
		return fmt.Errorf("error while closing remote etcd client: %w", err)
	}
	return nil
}

func (e *RemoteEtcd) Error() <-chan error {
	return e.errCh
}

func (e *RemoteEtcd) GetClient() (*clientv3.Client, error) {
	return e.client, nil
}

// TODO
func (e *RemoteEtcd) AsPeer() Peer {
	return Peer{}
}
