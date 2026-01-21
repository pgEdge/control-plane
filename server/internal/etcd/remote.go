package etcd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
)

var ErrOperationNotSupported = errors.New("operation not supported")

var _ Etcd = (*RemoteEtcd)(nil)
var _ do.Shutdownable = (*RemoteEtcd)(nil)

type RemoteEtcd struct {
	mu          sync.Mutex
	certSvc     *certificates.Service
	client      *clientv3.Client
	logger      zerolog.Logger
	cfg         *config.Manager
	initialized chan struct{}
	err         chan error
}

func NewRemoteEtcd(cfg *config.Manager, logger zerolog.Logger) *RemoteEtcd {
	return &RemoteEtcd{
		cfg:         cfg,
		initialized: make(chan struct{}),
		err:         make(chan error),
		logger: logger.With().
			Str("component", "etcd_client").
			Logger(),
	}
}

func (r *RemoteEtcd) IsInitialized() (bool, error) {
	// We're initialized if we have a cluster to connect to.
	return len(r.cfg.Config().EtcdClient.Endpoints) > 0, nil
}

func (r *RemoteEtcd) Start(ctx context.Context) error {
	r.logger.Debug().Msg("starting etcd client")

	initialized, err := r.IsInitialized()
	if err != nil {
		return err
	}
	if !initialized {
		return ErrOperationNotSupported
	}

	return r.start(ctx)
}

func (r *RemoteEtcd) start(ctx context.Context) error {
	client, err := r.GetClient()
	if err != nil {
		return err
	}
	r.certSvc, err = certificateService(ctx, r.cfg.Config(), client)
	if err != nil {
		return err
	}

	return nil
}

func (r *RemoteEtcd) Join(ctx context.Context, options JoinOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	initialized, err := r.IsInitialized()
	if err != nil {
		return err
	}
	if initialized {
		return errors.New("etcd already initialized - cannot join another cluster")
	}

	if err := writeHostCredentials(options.Credentials, r.cfg); err != nil {
		return err
	}

	clientCfg, err := clientConfig(r.cfg.Config(), r.logger, options.Leader.ClientURLs...)
	if err != nil {
		return fmt.Errorf("failed to get initial client config: %w", err)
	}

	initClient, err := clientv3.New(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to get initial client: %w", err)
	}
	defer initClient.Close()

	if err := r.updateEndpointsConfig(ctx, initClient); err != nil {
		return err
	}

	if err := r.start(ctx); err != nil {
		return err
	}

	close(r.initialized)

	return nil
}

func (r *RemoteEtcd) Initialized() <-chan struct{} {
	return r.initialized
}

func (r *RemoteEtcd) Error() <-chan error {
	return r.err
}

func (r *RemoteEtcd) GetClient() (*clientv3.Client, error) {
	if r.client != nil {
		return r.client, nil
	}

	cfg := r.cfg.Config()
	clientCfg, err := clientConfig(cfg, r.logger, cfg.EtcdClient.Endpoints...)
	if err != nil {
		return nil, fmt.Errorf("failed to get client config: %w", err)
	}

	// AutoSyncInterval determines how often the client will sync the list of
	// known endpoints from the cluster. The client will automatically load
	// balance and failover between endpoints, but syncs are desirable for
	// permanent membership changes.
	clientCfg.AutoSyncInterval = 5 * time.Minute

	client, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	r.client = client

	return client, nil
}

func (r *RemoteEtcd) Leader(ctx context.Context) (*ClusterMember, error) {
	client, err := r.GetClient()
	if err != nil {
		return nil, err
	}

	return GetClusterLeader(ctx, client)
}

func (r *RemoteEtcd) AddHost(ctx context.Context, opts HostCredentialOptions) (*HostCredentials, error) {
	client, err := r.GetClient()
	if err != nil {
		return nil, err
	}

	return CreateHostCredentials(ctx, client, r.certSvc, opts)
}

func (r *RemoteEtcd) RemoveHost(ctx context.Context, hostID string) error {
	if hostID == r.cfg.Config().HostID {
		return ErrCannotRemoveSelf
	}

	client, err := r.GetClient()
	if err != nil {
		return err
	}
	if err := RemoveHost(ctx, client, r.certSvc, hostID); err != nil {
		return err
	}

	return nil
}

func (r *RemoteEtcd) JoinToken() (string, error) {
	if r.certSvc == nil {
		return "", errors.New("etcd not initialized")
	}

	return r.certSvc.JoinToken(), nil
}

func (r *RemoteEtcd) VerifyJoinToken(in string) error {
	if r.certSvc == nil {
		return errors.New("etcd not initialized")
	}

	return VerifyJoinToken(r.certSvc, in)
}

func (r *RemoteEtcd) Shutdown() error {
	r.logger.Debug().Msg("shutting down etcd client")

	if r.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// We resync the endpoints before shutdown to capture any membership changes
	// that happened while the server was up.
	return errors.Join(
		r.updateEndpointsConfig(ctx, r.client),
		r.client.Close(),
	)
}

func (r *RemoteEtcd) HealthCheck() common.ComponentStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	alarms, err := r.client.AlarmList(ctx)
	if err != nil {
		return common.ComponentStatus{
			Name:    "etcd",
			Healthy: false,
			Error:   err.Error(),
		}
	}
	alarmStrs := make([]string, len(alarms.Alarms))
	for i, a := range alarms.Alarms {
		alarmStrs[i] = fmt.Sprintf("%d: %s", a.MemberID, a.Alarm.String())
	}

	return common.ComponentStatus{
		Name:    "etcd",
		Healthy: len(alarmStrs) == 0,
		Details: map[string]interface{}{
			"alarms": alarmStrs,
		},
	}
}

func (r *RemoteEtcd) updateEndpointsConfig(ctx context.Context, client *clientv3.Client) error {
	if err := client.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync client endpoints: %w", err)
	}

	generated := r.cfg.GeneratedConfig()
	generated.EtcdClient.Endpoints = client.Endpoints()
	if err := r.cfg.UpdateGeneratedConfig(generated); err != nil {
		return fmt.Errorf("failed to update generated config with client endpoints: %w", err)
	}

	return nil
}

func (r *RemoteEtcd) ChangeMode(ctx context.Context, mode config.EtcdMode) (Etcd, error) {
	if mode != config.EtcdModeServer {
		return nil, fmt.Errorf("invalid mode transition from %s to %s", config.EtcdModeClient, mode)
	}

	if err := r.Start(ctx); err != nil {
		return nil, err
	}

	cfg := r.cfg.Config()

	clientPrincipal, err := r.certSvc.HostEtcdUser(ctx, cfg.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client principal: %w", err)
	}

	creds := &HostCredentials{
		Username:   cfg.EtcdUsername,
		Password:   cfg.EtcdPassword,
		CaCert:     r.certSvc.CACert(),
		ClientCert: clientPrincipal.CertPEM,
		ClientKey:  clientPrincipal.KeyPEM,
	}

	if err := addEtcdServerCredentials(ctx, cfg.HostID, cfg.Hostname, cfg.IPv4Address, r.certSvc, creds); err != nil {
		return nil, err
	}

	client, err := r.GetClient()
	if err != nil {
		return nil, err
	}

	leader, err := GetClusterLeader(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster leader: %w", err)
	}

	if err := r.Shutdown(); err != nil {
		return nil, err
	}

	embedded := NewEmbeddedEtcd(r.cfg, r.logger)
	err = embedded.Join(ctx, JoinOptions{
		Leader:      leader,
		Credentials: creds,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to join embedded etcd to cluster: %w", err)
	}

	generated := r.cfg.GeneratedConfig()
	generated.EtcdMode = config.EtcdModeServer
	generated.EtcdClient = config.EtcdClient{}
	generated.EtcdServer = cfg.EtcdServer
	if err := r.cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to clear out etcd client settings in generated config: %w", err)
	}

	return embedded, nil
}
