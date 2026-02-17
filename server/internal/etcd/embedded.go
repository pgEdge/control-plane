package etcd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"go.etcd.io/etcd/server/v3/etcdserver"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ Etcd = (*EmbeddedEtcd)(nil)
var _ do.Shutdownable = (*EmbeddedEtcd)(nil)

// Sets the maximum size of the database. This is the largest suggested maximum.
const quotaBackendBytes = 8 * 1024 * 1024 * 1024 // 8GB

type EmbeddedEtcd struct {
	mu          sync.Mutex
	certSvc     *certificates.Service
	client      *clientv3.Client
	etcd        *embed.Etcd
	logger      zerolog.Logger
	cfg         *config.Manager
	initialized chan struct{}
}

func NewEmbeddedEtcd(cfg *config.Manager, logger zerolog.Logger) *EmbeddedEtcd {
	return &EmbeddedEtcd{
		cfg:         cfg,
		initialized: make(chan struct{}),
		logger: logger.With().
			Str("component", "etcd_server").
			Logger(),
	}
}

func (e *EmbeddedEtcd) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.etcd != nil {
		return nil // already started
	}

	initialized, err := e.IsInitialized()
	if err != nil {
		return err
	}
	if !initialized {
		return e.initialize(ctx)
	}

	return e.start(ctx)
}

func (e *EmbeddedEtcd) initialize(ctx context.Context) error {
	appCfg := e.cfg.Config()

	etcdCfg, err := initializationConfig(appCfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}
	etcd, err := startEmbedded(ctx, etcdCfg)
	if err != nil {
		return fmt.Errorf("failed to start etcd for initialization: %w", err)
	}
	client, err := clientForEmbedded(appCfg, e.logger, etcd)
	if err != nil {
		return fmt.Errorf("failed to get etcd client for initialization: %w", err)
	}
	// Initialize the certificate authority. We don't persist this instance of
	// the cert service because this client is temporary.
	certSvc, err := certificateService(ctx, appCfg, client)
	if err != nil {
		return err
	}

	// create root user/role so that we can enable RBAC:
	// https://etcd.io/docs/v3.5/op-guide/authentication/rbac/
	if _, err = client.RoleAdd(ctx, "root"); err != nil {
		return fmt.Errorf("failed to create root role: %w", err)
	}
	// Setting an empty password makes it so this user cannot be authenticated.
	if _, err := client.UserAdd(ctx, "root", ""); err != nil {
		return fmt.Errorf("failed to create root user: %w", err)
	}
	if _, err := client.UserGrantRole(ctx, "root", "root"); err != nil {
		return fmt.Errorf("failed to grant root role to root user: %w", err)
	}

	creds, err := CreateHostCredentials(ctx, client, certSvc, HostCredentialOptions{
		HostID:              appCfg.HostID,
		Hostname:            appCfg.Hostname,
		IPv4Address:         appCfg.IPv4Address,
		EmbeddedEtcdEnabled: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create etcd host credentials: %w", err)
	}

	if err := writeHostCredentials(creds, e.cfg); err != nil {
		return err
	}

	// Update peer URL to use external IP and HTTPS
	members, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list cluster members: %w", err)
	}
	if len(members.Members) != 1 {
		return errors.New("expected exactly one member in the cluster")
	}
	if _, err := client.MemberUpdate(ctx,
		members.Members[0].ID,
		e.PeerEndpoints(),
	); err != nil {
		return fmt.Errorf("failed to update peer URL: %w", err)
	}

	// Enable RBAC - IMPORTANT: must be done last before restarting the server
	if _, err := client.AuthEnable(ctx); err != nil {
		return fmt.Errorf("failed to enable authentication: %w", err)
	}

	// Shutdown the temporary server
	if err := client.Close(); err != nil {
		return fmt.Errorf("failed to close initialization client: %w", err)
	}
	etcd.Close()

	// Perform a normal startup now that the server is initialized
	if err := e.start(ctx); err != nil {
		return err
	}

	close(e.initialized)

	return nil
}

func (e *EmbeddedEtcd) start(ctx context.Context) error {
	appCfg := e.cfg.Config()
	e.logger.Info().
		Int("peer_port", appCfg.EtcdServer.PeerPort).
		Int("client_port", appCfg.EtcdServer.ClientPort).
		Msg("starting embedded etcd server")

	etcdCfg, err := embedConfig(appCfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}
	etcd, err := startEmbedded(ctx, etcdCfg)
	if err != nil {
		return fmt.Errorf("failed to start etcd: %w", err)
	}
	e.etcd = etcd

	client, err := e.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get internal etcd client: %w", err)
	}

	urlsChanged, updateErr := UpdateMemberPeerURLs(ctx, client, appCfg.HostID, e.PeerEndpoints())
	if updateErr != nil {
		e.logger.Warn().Err(updateErr).Msg("failed to update peer URLs after IP/hostname change")
	} else if urlsChanged {
		e.logger.Info().Msg("peer URLs updated due to IP/hostname change")
	}

	e.certSvc, err = certificateService(ctx, e.cfg.Config(), client)
	if err != nil {
		return err
	}

	return nil
}

func (e *EmbeddedEtcd) Join(ctx context.Context, options JoinOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	initialized, err := e.IsInitialized()
	if err != nil {
		return err
	}
	if initialized {
		return errors.New("etcd already initialized - cannot join another cluster")
	}

	if err := writeHostCredentials(options.Credentials, e.cfg); err != nil {
		return err
	}

	appCfg := e.cfg.Config()

	etcdCfg, err := embedConfig(appCfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}

	clientCfg, err := clientConfig(e.cfg.Config(), e.logger, options.Leader.ClientURLs...)
	if err != nil {
		return fmt.Errorf("failed to get initial client config: %w", err)
	}

	initClient, err := clientv3.New(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to get initial client: %w", err)
	}

	// This operation can fail if we're joining multiple members to the cluster
	// simultaneously. The max learners per cluster will be configurable in Etcd
	// 3.6, but in 3.5 it's hardcoded 1 learner per cluster. There's also a
	// limit on the number of unhealthy (which could just be starting up)
	// members that's calculated based on the cluster size. This is done to
	// protect the quorum.
	err = utils.Retry(5, time.Second, func() error {
		// We always add as a learner first to minimize disruptions
		_, err = initClient.MemberAddAsLearner(ctx, e.PeerEndpoints())
		if err != nil {
			return fmt.Errorf("failed to add this etcd server as learner: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	members, err := initClient.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list cluster members: %w", err)
	}
	// This server will have an empty member name in the members list, so we
	// add this server to the list separately.
	name := e.cfg.Config().HostID
	var peers []string
	for _, peerURL := range e.PeerEndpoints() {
		peers = append(peers, fmt.Sprintf("%s=%s", name, peerURL))
	}
	for _, m := range members.Members {
		// Empty name indicates a member that hasn't started, including this
		// server.
		if m.Name == "" || m.Name == name {
			continue
		}
		if len(m.PeerURLs) < 1 {
			return fmt.Errorf("member %q has no peer URLs", m.Name)
		}
		for _, peerURL := range m.PeerURLs {
			peers = append(peers, fmt.Sprintf("%s=%s", m.Name, peerURL))
		}
	}
	etcdCfg.InitialCluster = strings.Join(peers, ",")
	etcdCfg.ClusterState = embed.ClusterStateFlagExisting

	e.logger.Info().
		Int("peer_port", appCfg.EtcdServer.PeerPort).
		Int("client_port", appCfg.EtcdServer.ClientPort).
		Msg("starting embedded etcd server")

	etcd, err := startEmbedded(ctx, etcdCfg)
	if err != nil {
		return err
	}
	e.etcd = etcd

	e.logger.Info().Msg("etcd started as learner")
	if err := e.PromoteWhenReady(ctx, initClient, appCfg.HostID); err != nil {
		return fmt.Errorf("failed to promote this etcd server: %w", err)
	}

	if err := initClient.Close(); err != nil {
		return fmt.Errorf("failed to close initialization client: %w", err)
	}

	client, err := e.GetClient()
	if err != nil {
		return err
	}

	e.certSvc, err = certificateService(ctx, appCfg, client)
	if err != nil {
		return err
	}

	close(e.initialized)

	return nil
}

func (e *EmbeddedEtcd) Initialized() <-chan struct{} {
	return e.initialized
}

func (e *EmbeddedEtcd) Shutdown() error {
	e.logger.Info().Msg("shutting down embedded etcd server")

	var errs []error
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close etcd client: %w", err))
		}
		e.client = nil
	}
	if e.etcd != nil {
		e.etcd.Close()
		e.etcd = nil
	}
	return errors.Join(errs...)
}

func (e *EmbeddedEtcd) Error() <-chan error {
	return e.etcd.Err()
}

func (e *EmbeddedEtcd) ClientEndpoints() []string {
	appCfg := e.cfg.Config()
	clientPort := appCfg.EtcdServer.ClientPort
	return []string{
		fmt.Sprintf("https://%s:%d", appCfg.IPv4Address, clientPort),
		fmt.Sprintf("https://%s:%d", appCfg.Hostname, clientPort),
	}
}

func (e *EmbeddedEtcd) PeerEndpoints() []string {
	appCfg := e.cfg.Config()
	peerPort := appCfg.EtcdServer.PeerPort
	return []string{
		fmt.Sprintf("https://%s:%d", appCfg.IPv4Address, peerPort),
		fmt.Sprintf("https://%s:%d", appCfg.Hostname, peerPort),
	}
}

func (e *EmbeddedEtcd) etcdDir() string {
	return filepath.Join(e.cfg.Config().DataDir, "etcd")
}

func (e *EmbeddedEtcd) Leader(ctx context.Context) (*ClusterMember, error) {
	client, err := e.GetClient()
	if err != nil {
		return nil, err
	}

	return GetClusterLeader(ctx, client)
}

func (e *EmbeddedEtcd) IsInitialized() (bool, error) {
	// Use the existence of the WAL dir to determine if a server has already
	// been started with this data directory.
	walDir := filepath.Join(e.etcdDir(), "member", "wal")
	info, err := os.Stat(walDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to stat wal dir %q: %w", walDir, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%q is not a directory", walDir)
	}

	return true, nil
}

func (e *EmbeddedEtcd) JoinToken() (string, error) {
	if e.certSvc == nil {
		return "", errors.New("etcd not initialized")
	}

	return e.certSvc.JoinToken(), nil
}

func (e *EmbeddedEtcd) VerifyJoinToken(in string) error {
	if e.certSvc == nil {
		return errors.New("etcd not initialized")
	}

	return VerifyJoinToken(e.certSvc, in)
}

func (e *EmbeddedEtcd) GetClient() (*clientv3.Client, error) {
	if e.client != nil {
		return e.client, nil
	}

	cfg := e.cfg.Config()
	clientCfg, err := clientConfig(cfg, e.logger, e.etcd.Server.Cluster().ClientURLs()...)
	if err != nil {
		return nil, fmt.Errorf("failed to get client config: %w", err)
	}

	client, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	e.client = client

	return client, nil
}

func (e *EmbeddedEtcd) AddHost(ctx context.Context, opts HostCredentialOptions) (*HostCredentials, error) {
	client, err := e.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	return CreateHostCredentials(ctx, client, e.certSvc, opts)
}

func (e *EmbeddedEtcd) RemoveHost(ctx context.Context, hostID string) error {
	if hostID == e.cfg.Config().HostID {
		return ErrCannotRemoveSelf
	}

	client, err := e.GetClient()
	if err != nil {
		return err
	}
	if err := RemoveHost(ctx, client, e.certSvc, hostID); err != nil {
		return err
	}

	return nil
}

func (e *EmbeddedEtcd) HealthCheck() common.ComponentStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := e.client.Status(ctx, e.client.Endpoints()[0])
	if err != nil {
		return common.ComponentStatus{
			Name:    "etcd",
			Healthy: false,
			Error:   err.Error(),
		}
	}

	alarms, err := e.client.AlarmList(ctx)
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
		Healthy: len(status.Errors) == 0 && len(alarmStrs) == 0,
		Details: map[string]interface{}{
			"errors": status.Errors,
			"alarms": alarmStrs,
		},
	}
}

func (e *EmbeddedEtcd) ChangeMode(ctx context.Context, mode config.EtcdMode) (Etcd, error) {
	if mode != config.EtcdModeClient {
		return nil, fmt.Errorf("invalid mode transition from %s to %s", config.EtcdModeServer, mode)
	}

	if err := e.Start(ctx); err != nil {
		return nil, err
	}

	cfg := e.cfg.Config()

	embeddedClient, err := e.GetClient()
	if err != nil {
		return nil, err
	}

	// Get the full member list before removing this host
	resp, err := embeddedClient.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd members for server->client transition: %w", err)
	}

	var endpoints []string
	for _, m := range resp.Members {
		// Skip this host's member; we are about to remove it.
		if m.Name == cfg.HostID {
			continue
		}
		endpoints = append(endpoints, m.ClientURLs...)
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("cannot demote etcd server on host %s: no remaining cluster members with client URLs", cfg.HostID)
	}

	generated := e.cfg.GeneratedConfig()
	generated.EtcdClient.Endpoints = endpoints
	if err := e.cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to update generated config with client endpoints: %w", err)
	}

	if err := e.Shutdown(); err != nil {
		return nil, err
	}

	remote := NewRemoteEtcd(e.cfg, e.logger)
	if err := remote.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start remote client: %w", err)
	}

	remoteClient, err := remote.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get remote client: %w", err)
	}

	if err := RemoveMember(ctx, remoteClient, cfg.HostID); err != nil {
		return nil, fmt.Errorf("failed to remove embedded etcd from cluster: %w", err)
	}

	if err := os.RemoveAll(e.etcdDir()); err != nil {
		return nil, fmt.Errorf("failed to remove embedded etcd data dir: %w", err)
	}

	generated.EtcdMode = config.EtcdModeClient
	generated.EtcdServer = config.EtcdServer{}
	if err := e.cfg.UpdateGeneratedConfig(generated); err != nil {
		return nil, fmt.Errorf("failed to clear out etcd server settings in generated config: %w", err)
	}

	return remote, nil
}

const maxLearnerStallTime = 5 * time.Minute

type learnerProgress struct {
	id               uint64
	name             string
	member           *etcdserverpb.Member
	raftAppliedIndex uint64
	lastProgress     time.Time
}

func (e *EmbeddedEtcd) promoteWhenReadyHelper(ctx context.Context, client *clientv3.Client, learner learnerProgress) error {
	e.logger.Info().Msg("attempting to promote from learner to voting cluster member")
	_, err := client.MemberPromote(ctx, learner.id)
	if err == nil {
		// Etcd checks that cluster members have been connected for a minimum
		// interval before considering them part of the quorum. This affects
		// Etcd's internal health checks. We'll block for that interval so that
		// clients can safely modify the cluster when this returns.
		e.logger.Info().Msg("promotion successful")
		e.logger.Info().Msg("waiting for cluster to be healthy")
		time.Sleep(etcdserver.HealthInterval)
		return nil
	}

	now := time.Now()

	// get status from the member's first reachable client URL
	for _, ep := range learner.member.ClientURLs {
		status, err := client.Status(ctx, ep)
		if err != nil {
			continue
		}
		if learner.raftAppliedIndex < status.RaftAppliedIndex {
			learner.raftAppliedIndex = status.RaftAppliedIndex
			learner.lastProgress = now
		}
		break
	}

	if now.Sub(learner.lastProgress) > maxLearnerStallTime {
		if _, err := client.MemberRemove(ctx, learner.id); err != nil {
			return fmt.Errorf("failed to remove stalled learner: %w", err)
		}
		return fmt.Errorf("removed learner member %q because it failed to make progress", learner.name)
	}

	e.logger.Info().
		Time("last_progress", learner.lastProgress).
		Uint64("raft_applied_index", learner.raftAppliedIndex).
		Msg("waiting before attempting promotion again")

	time.Sleep(1 * time.Second)

	return e.promoteWhenReadyHelper(ctx, client, learner)
}

func (e *EmbeddedEtcd) PromoteWhenReady(ctx context.Context, client *clientv3.Client, memberName string) error {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to get member list: %w", err)
	}
	member := findMember(resp.Members, memberName)
	if member == nil {
		return fmt.Errorf("member not found: %s", memberName)
	}
	return e.promoteWhenReadyHelper(ctx, client, learnerProgress{
		id:               member.ID,
		name:             memberName,
		member:           member,
		raftAppliedIndex: 0,
		lastProgress:     time.Now(),
	})
}

func embedConfig(cfg config.Config, logger zerolog.Logger) (*embed.Config, error) {
	lg, err := newZapLogger(logger, cfg.EtcdServer.LogLevel, "etcd_server")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd server logger: %w", err)
	}

	c := embed.NewConfig()
	c.ZapLoggerBuilder = embed.NewZapLoggerBuilder(lg)
	c.Name = cfg.HostID
	c.Dir = filepath.Join(cfg.DataDir, "etcd")
	// Recommended auto-compaction settings for Kubernetes. We don't expect
	// nearly the amount of write traffic, but it's a good starting point to
	// determine our ideal settings. Unlike defrag, auto-compaction does not
	// require a full lock, so it's less impactful to run frequently.
	c.AutoCompactionMode = "periodic"
	c.AutoCompactionRetention = "5m"
	// Lease TTL is managed by the leader, so TTLs get reset if the leader
	// fails. These settings enable checkpoints to persist the lease state so
	// that it can be recovered if the leader fails.
	c.ExperimentalEnableLeaseCheckpoint = true
	c.ExperimentalEnableLeaseCheckpointPersist = true
	c.ClientTLSInfo = transport.TLSInfo{
		TrustedCAFile: filepath.Join(cfg.DataDir, "certificates", "ca.crt"),
		CertFile:      filepath.Join(cfg.DataDir, "certificates", "etcd-server.crt"),
		KeyFile:       filepath.Join(cfg.DataDir, "certificates", "etcd-server.key"),
	}
	c.PeerTLSInfo = transport.TLSInfo{
		ClientCertAuth: true,
		TrustedCAFile:  filepath.Join(cfg.DataDir, "certificates", "ca.crt"),
		CertFile:       filepath.Join(cfg.DataDir, "certificates", "etcd-server.crt"),
		KeyFile:        filepath.Join(cfg.DataDir, "certificates", "etcd-server.key"),
		ClientCertFile: filepath.Join(cfg.DataDir, "certificates", "etcd-user.crt"),
		ClientKeyFile:  filepath.Join(cfg.DataDir, "certificates", "etcd-user.key"),
	}

	clientPort := cfg.EtcdServer.ClientPort
	peerPort := cfg.EtcdServer.PeerPort
	myIP := cfg.IPv4Address
	c.ListenClientUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("0.0.0.0:%d", clientPort)},
	}
	c.AdvertiseClientUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", myIP, clientPort)},
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", cfg.Hostname, clientPort)},
	}

	c.ListenPeerUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("0.0.0.0:%d", peerPort)},
	}
	c.AdvertisePeerUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", myIP, peerPort)},
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", cfg.Hostname, peerPort)},
	}

	// This will get overridden when joining an existing cluster
	c.InitialCluster = fmt.Sprintf(
		"%s=http://%s:%d",
		cfg.HostID,
		myIP,
		peerPort,
	)
	// Using a large number here as a precaution. We're unlikely to hit this,
	// but the workflows backend can produce large transactions in complex
	// workflows.
	c.MaxTxnOps = 2048
	c.MaxRequestBytes = 10 * 1024 * 1024 // 10MB
	c.QuotaBackendBytes = quotaBackendBytes

	return c, nil
}

func initializationConfig(cfg config.Config, logger zerolog.Logger) (*embed.Config, error) {
	lg, err := newZapLogger(logger, cfg.EtcdServer.LogLevel, "etcd_server")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd server logger: %w", err)
	}

	c := embed.NewConfig()
	c.ZapLoggerBuilder = embed.NewZapLoggerBuilder(lg)
	c.Name = cfg.HostID
	c.Dir = filepath.Join(cfg.DataDir, "etcd")

	// Only bind/advertise localhost for initialization
	clientPort := cfg.EtcdServer.ClientPort
	peerPort := cfg.EtcdServer.PeerPort
	c.ListenClientUrls = []url.URL{
		{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", clientPort)},
	}
	c.AdvertiseClientUrls = []url.URL{
		{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", clientPort)},
	}
	c.ListenPeerUrls = []url.URL{
		{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", peerPort)},
	}
	c.AdvertisePeerUrls = []url.URL{
		{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", peerPort)},
	}
	c.InitialCluster = fmt.Sprintf(
		"%s=http://127.0.0.1:%d",
		cfg.HostID,
		cfg.EtcdServer.PeerPort,
	)
	c.QuotaBackendBytes = quotaBackendBytes

	return c, nil
}

func startEmbedded(ctx context.Context, cfg *embed.Config) (*embed.Etcd, error) {
	etcd, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to start etcd: %w", err)
	}

	// Block until ready
	select {
	case <-etcd.Server.ReadyNotify():
		return etcd, nil
	case <-time.After(60 * time.Second):
		etcd.Server.Stop() // trigger a shutdown
		return nil, errors.New("server took too long to start")
	case <-ctx.Done():
		etcd.Server.Stop()
		return nil, fmt.Errorf("context cancelled while starting etcd: %w", ctx.Err())
	}
}

func clientForEmbedded(cfg config.Config, logger zerolog.Logger, etcd *embed.Etcd) (*clientv3.Client, error) {
	lg, err := newZapLogger(logger, cfg.EtcdClient.LogLevel, "etcd_client")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client logger: %w", err)
	}

	client, err := clientv3.New(clientv3.Config{
		Logger:    lg,
		Endpoints: etcd.Server.Cluster().ClientURLs(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client: %w", err)
	}

	return client, nil
}
