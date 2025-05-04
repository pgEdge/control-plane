package etcd

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
	"go.etcd.io/etcd/api/v3/authpb"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ common.HealthCheckable = (*EmbeddedEtcd)(nil)
var _ do.Shutdownable = (*EmbeddedEtcd)(nil)

type JoinOptions struct {
	Peer        Peer
	Credentials *HostCredentials
}

type EmbeddedEtcd struct {
	mu          sync.Mutex
	certSvc     *certificates.Service
	client      *clientv3.Client
	etcd        *embed.Etcd
	logger      zerolog.Logger
	cfg         config.Config
	initialized chan struct{}
}

func NewEmbeddedEtcd(cfg config.Config, logger zerolog.Logger) *EmbeddedEtcd {
	return &EmbeddedEtcd{
		cfg:         cfg,
		logger:      logger,
		initialized: make(chan struct{}, 1),
	}
}

type Peer struct {
	Name      string
	PeerURL   string
	ClientURL string
}

type StartOptions struct {
	JoinPeer     *Peer
	ClusterToken string
}

func (e *EmbeddedEtcd) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	initialized, err := e.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to determine if etcd is already initialized: %w", err)
	}
	if !initialized {
		return e.initialize(ctx)
	}

	return e.start(ctx)
}

// TODO: This is all tangled up because we need the cert service during etcd
// initialization. Ideally, we should inject the service into the EmbeddedEtcd.
func (e *EmbeddedEtcd) CertService() *certificates.Service {
	return e.certSvc
}

func (e *EmbeddedEtcd) initialize(ctx context.Context) error {
	cfg, err := initializationConfig(e.cfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}
	etcd, err := startEmbedded(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to start etcd for initialization: %w", err)
	}
	client, err := clientForEmbedded(e.cfg, e.logger, etcd)
	if err != nil {
		return fmt.Errorf("failed to get etcd client for initialization: %w", err)
	}
	// Initialize the certificate authority
	certStore := certificates.NewStore(client, e.cfg.EtcdKeyRoot)
	certSvc := certificates.NewService(e.cfg, certStore)
	if err := certSvc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start certificate service: %w", err)
	}

	// create root user/role so that we can enable RBAC:
	// https://etcd.io/docs/v3.5/op-guide/authentication/rbac/
	if _, err = client.RoleAdd(ctx, "root"); err != nil {
		return fmt.Errorf("failed to create root role: %w", err)
	}
	// TODO: We're not able to use cert auth, so we need secure passwords. This
	// password can be generated and thrown away, since each host will get its
	// own user.
	// Actually, no password might be the most secure option: https://etcd.io/docs/v3.5/op-guide/authentication/rbac/
	if _, err := client.UserAdd(ctx, "root", ""); err != nil {
		return fmt.Errorf("failed to create root user: %w", err)
	}
	if _, err := client.UserGrantRole(ctx, "root", "root"); err != nil {
		return fmt.Errorf("failed to grant root role to root user: %w", err)
	}

	creds, err := createEtcdHostCredentials(ctx, client, certSvc, HostCredentialOptions{
		HostID:      e.cfg.HostID,
		Hostname:    e.cfg.Hostname,
		IPv4Address: e.cfg.IPv4Address,
	})
	if err != nil {
		return fmt.Errorf("failed to create etcd host credentials: %w", err)
	}

	if err := e.writeCredentials(creds); err != nil {
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
		[]string{e.AsPeer().PeerURL},
	); err != nil {
		return fmt.Errorf("failed to update peer URL: %w", err)
	}

	// Enable RBAC - IMPORTANT: must be done last before restarting the server
	if _, err := client.Auth.AuthEnable(ctx); err != nil {
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

	e.initialized <- struct{}{}

	return nil
}

func (e *EmbeddedEtcd) start(ctx context.Context) error {
	cfg, err := embedConfig(e.cfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}
	etcd, err := startEmbedded(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to start etcd: %w", err)
	}
	e.etcd = etcd

	client, err := e.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get internal etcd client: %w", err)
	}

	certStore := certificates.NewStore(client, e.cfg.EtcdKeyRoot)
	certSvc := certificates.NewService(e.cfg, certStore)
	if err := certSvc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start certificate service: %w", err)
	}
	e.certSvc = certSvc

	return nil
}

func (e *EmbeddedEtcd) Join(ctx context.Context, options JoinOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	initialized, err := e.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to determine if etcd is already ")
	}
	if initialized {
		return errors.New("etcd already initialized - cannot join another cluster")
	}

	if err := e.writeCredentials(options.Credentials); err != nil {
		return err
	}

	cfg, err := embedConfig(e.cfg, e.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize embedded etcd config: %w", err)
	}

	lg, err := newZapLogger(e.logger, e.cfg.EmbeddedEtcd.ClientLogLevel, "etcd_peer_client")
	if err != nil {
		return fmt.Errorf("failed to initialize etcd peer client logger: %w", err)
	}

	tlsConfig, err := e.clientTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to initialize etcd client TLS config: %w", err)
	}

	client, err := clientv3.New(clientv3.Config{
		Logger:    lg,
		Endpoints: []string{options.Peer.ClientURL},
		TLS:       tlsConfig,
		Username:  fmt.Sprintf("host-%s", e.cfg.HostID.String()),
		Password:  e.cfg.HostID.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize etcd peer client: %w", err)
	}

	// This operation can fail if we're joining multiple members to the cluster
	// simultaneously. The max learners per cluster will be configurable in Etcd
	// 3.6, but in 3.5 it's hardcoded 1 learner per cluster. There's also a on
	// the number of unhealthy (which could just be starting up) members that's
	// calculated based on the cluster size. This is done to protect the quorum.
	err = utils.Retry(5, time.Second, func() error {
		// We always add as a learner first to minimize disruptions
		_, err = client.MemberAddAsLearner(ctx, []string{e.AsPeer().PeerURL})
		if err != nil {
			return fmt.Errorf("failed to add this etcd server as learner: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	members, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list cluster members: %w", err)
	}
	// This server will have an empty member name in the members list, so we
	// add this server to the list separately.
	self := e.AsPeer()
	peers := []string{fmt.Sprintf("%s=%s", self.Name, self.PeerURL)}
	for _, m := range members.Members {
		// Empty name indicates a member that hasn't started, including this
		// server.
		if m.Name == "" || m.Name == self.Name {
			continue
		}
		if len(m.PeerURLs) < 1 {
			return fmt.Errorf("member %q has no peer URLs", m.Name)
		}
		peers = append(peers, fmt.Sprintf("%s=%s", m.Name, m.PeerURLs[0]))
	}
	cfg.InitialCluster = strings.Join(peers, ",")
	cfg.ClusterState = embed.ClusterStateFlagExisting

	etcd, err := startEmbedded(ctx, cfg)
	if err != nil {
		return err
	}
	e.etcd = etcd

	if err := PromoteWhenReady(ctx, client, e.cfg.HostID.String()); err != nil {
		return fmt.Errorf("failed to promote this etcd server: %w", err)
	}

	client, err = e.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get internal etcd client: %w", err)
	}

	certStore := certificates.NewStore(client, e.cfg.EtcdKeyRoot)
	certSvc := certificates.NewService(e.cfg, certStore)
	if err := certSvc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start certificate service: %w", err)
	}
	e.certSvc = certSvc

	e.initialized <- struct{}{}

	return nil
}

func (e *EmbeddedEtcd) Initialized() <-chan struct{} {
	return e.initialized
}

func (e *EmbeddedEtcd) Shutdown() error {
	var errs []error
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close etcd client: %w", err))
		}
		e.client = nil
	}
	if e.etcd != nil {
		e.etcd.Close()
	}
	return errors.Join(errs...)
}

func (e *EmbeddedEtcd) Error() <-chan error {
	return e.etcd.Err()
}

func (e *EmbeddedEtcd) ClientEndpoint() string {
	return fmt.Sprintf("https://%s:%d", e.cfg.IPv4Address, e.cfg.EmbeddedEtcd.ClientPort)
}

func (e *EmbeddedEtcd) DataDir() string {
	return filepath.Join(e.cfg.DataDir, "etcd")
}

func (e *EmbeddedEtcd) AsPeer() Peer {
	return Peer{
		Name:      e.cfg.HostID.String(),
		PeerURL:   fmt.Sprintf("https://%s:%d", e.cfg.IPv4Address, e.cfg.EmbeddedEtcd.PeerPort),
		ClientURL: e.ClientEndpoint(),
	}
}

func (e *EmbeddedEtcd) IsInitialized() (bool, error) {
	// Use the existence of the WAL dir to determine if a server has already
	// been started with this data directory.
	walDir := filepath.Join(e.DataDir(), "member", "wal")
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
	} else {
		return e.certSvc.JoinToken(), nil
	}
}

func (e *EmbeddedEtcd) VerifyJoinToken(in string) error {
	if e.certSvc == nil {
		return errors.New("etcd not initialized")
	}
	token := e.certSvc.JoinToken()
	if subtle.ConstantTimeCompare([]byte(in), []byte(token)) != 1 {
		return errors.New("invalid join token")
	}
	return nil
}

func (e *EmbeddedEtcd) clientTLSConfig() (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(e.cfg.DataDir, "certificates", "etcd-user.crt"),
		filepath.Join(e.cfg.DataDir, "certificates", "etcd-user.key"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read client cert: %w", err)
	}
	rootCA, err := os.ReadFile(filepath.Join(e.cfg.DataDir, "certificates", "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(rootCA); !ok {
		return nil, errors.New("failed to use CA cert")
	}

	return &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
	}, nil
}

func (e *EmbeddedEtcd) GetClient() (*clientv3.Client, error) {
	if e.client != nil {
		return e.client, nil
	}

	lg, err := newZapLogger(e.logger, e.cfg.EmbeddedEtcd.ClientLogLevel, "etcd_client")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client logger: %w", err)
	}

	tlsConfig, err := e.clientTLSConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client TLS config: %w", err)
	}

	client, err := clientv3.New(clientv3.Config{
		Logger:             lg,
		Endpoints:          e.etcd.Server.Cluster().ClientURLs(),
		TLS:                tlsConfig,
		Username:           fmt.Sprintf("host-%s", e.cfg.HostID.String()),
		Password:           e.cfg.HostID.String(),
		MaxCallSendMsgSize: 10 * 1024 * 1024, // 10MB
		MaxCallRecvMsgSize: 10 * 1024 * 1024, // 10MB
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd client: %w", err)
	}
	e.client = client

	return client, nil
}

func (e *EmbeddedEtcd) AddPeerUser(ctx context.Context, opts HostCredentialOptions) (*HostCredentials, error) {
	client, err := e.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	return createEtcdHostCredentials(ctx, client, e.certSvc, opts)
}

func (e *EmbeddedEtcd) AddInstanceUser(ctx context.Context, opts InstanceUserOptions) (*InstanceUserCredentials, error) {
	client, err := e.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	return CreateInstanceEtcdUser(ctx, client, e.certSvc, opts)
}

func (e *EmbeddedEtcd) writeCredentials(creds *HostCredentials) error {
	certs := &filesystem.Directory{
		Path: "certificates",
		Mode: 0o700,
		Children: []filesystem.TreeNode{
			&filesystem.File{
				Path:     "ca.crt",
				Mode:     0o644,
				Contents: creds.CaCert,
			},
			&filesystem.File{
				Path:     "etcd-server.crt",
				Mode:     0o644,
				Contents: creds.ServerCert,
			},
			&filesystem.File{
				Path:     "etcd-server.key",
				Mode:     0o600,
				Contents: creds.ServerKey,
			},
			&filesystem.File{
				Path:     "etcd-user.crt",
				Mode:     0o644,
				Contents: creds.ClientCert,
			},
			&filesystem.File{
				Path:     "etcd-user.key",
				Mode:     0o600,
				Contents: creds.ClientKey,
			},
		},
	}
	err := certs.Create(context.Background(), afero.NewOsFs(), e.cfg.DataDir, e.cfg.DatabaseOwnerUID)
	if err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
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

const maxLearnerStallTime = 5 * time.Minute

type learnerProgress struct {
	id               uint64
	name             string
	member           *etcdserverpb.Member
	raftAppliedIndex uint64
	lastProgress     time.Time
}

func promoteWhenReadyHelper(ctx context.Context, client *clientv3.Client, learner learnerProgress) error {
	_, err := client.MemberPromote(ctx, learner.id)
	if err == nil {
		// Success!
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

	time.Sleep(1 * time.Second)

	return promoteWhenReadyHelper(ctx, client, learner)
}

func PromoteWhenReady(ctx context.Context, client *clientv3.Client, memberName string) error {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to get member list: %w", err)
	}

	for _, m := range resp.Members {
		if m.Name != memberName {
			continue
		}
		return promoteWhenReadyHelper(ctx, client, learnerProgress{
			id:               m.ID,
			name:             memberName,
			member:           m,
			raftAppliedIndex: 0,
			lastProgress:     time.Now(),
		})
	}

	return fmt.Errorf("failed to find member %q in member list", memberName)
}

func embedConfig(cfg config.Config, logger zerolog.Logger) (*embed.Config, error) {
	lg, err := newZapLogger(logger, cfg.EmbeddedEtcd.ServerLogLevel, "etcd_server")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd server logger: %w", err)
	}

	c := embed.NewConfig()
	c.ZapLoggerBuilder = embed.NewZapLoggerBuilder(lg)
	c.Name = cfg.HostID.String()
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

	clientPort := cfg.EmbeddedEtcd.ClientPort
	peerPort := cfg.EmbeddedEtcd.PeerPort
	myIP := cfg.IPv4Address
	c.ListenClientUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("0.0.0.0:%d", clientPort)},
	}
	c.AdvertiseClientUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", myIP, clientPort)},
	}
	c.ListenPeerUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("0.0.0.0:%d", peerPort)},
	}
	c.AdvertisePeerUrls = []url.URL{
		{Scheme: "https", Host: fmt.Sprintf("%s:%d", myIP, peerPort)},
	}
	// This will get overridden when joining an existing cluster
	c.InitialCluster = fmt.Sprintf(
		"%s=http://%s:%d",
		cfg.HostID.String(),
		myIP,
		peerPort,
	)
	// Using a large number here as a precaution. We're unlikely to hit this,
	// but the workflows backend can produce large transactions in complex
	// workflows.
	c.MaxTxnOps = 2048
	c.MaxRequestBytes = 10 * 1024 * 1024 // 10MB

	return c, nil
}

func initializationConfig(cfg config.Config, logger zerolog.Logger) (*embed.Config, error) {
	lg, err := newZapLogger(logger, cfg.EmbeddedEtcd.ServerLogLevel, "etcd_server")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize etcd server logger: %w", err)
	}

	c := embed.NewConfig()
	c.ZapLoggerBuilder = embed.NewZapLoggerBuilder(lg)
	c.Name = cfg.HostID.String()
	c.Dir = filepath.Join(cfg.DataDir, "etcd")

	// Only bind/advertise localhost for initialization
	clientPort := cfg.EmbeddedEtcd.ClientPort
	peerPort := cfg.EmbeddedEtcd.PeerPort
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
		cfg.EmbeddedEtcd.PeerPort,
	)

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
	lg, err := newZapLogger(logger, cfg.EmbeddedEtcd.ClientLogLevel, "etcd_client")
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

type HostCredentialOptions struct {
	HostID      uuid.UUID
	Hostname    string
	IPv4Address string
}

type HostCredentials struct {
	CaCert     []byte
	ClientCert []byte
	ClientKey  []byte
	ServerCert []byte
	ServerKey  []byte
}

func createEtcdHostCredentials(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	opts HostCredentialOptions,
) (*HostCredentials, error) {
	username := fmt.Sprintf("host-%s", opts.HostID.String())

	// Create a user for the peer host
	// TODO: patroni doesn't support CN auth, so we need a password. Replace this
	// with something random
	if _, err := client.UserAdd(ctx, username, opts.HostID.String()); err != nil {
		return nil, fmt.Errorf("failed to create host user: %w", err)
	}
	if _, err := client.UserGrantRole(ctx, username, "root"); err != nil {
		return nil, fmt.Errorf("failed to grant root role to host user: %w", err)
	}

	// Create a cert for the peer user
	clientPrincipal, err := certSvc.HostEtcdUser(ctx, opts.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert for etcd host user: %w", err)
	}
	// Create a cert for the peer server
	serverPrincipal, err := certSvc.EtcdServer(ctx,
		opts.HostID,
		opts.Hostname,
		[]string{"localhost", opts.Hostname},
		[]string{"127.0.0.1", opts.IPv4Address},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert for etcd server: %w", err)
	}

	return &HostCredentials{
		CaCert:     certSvc.CACert(),
		ClientCert: clientPrincipal.CertPEM,
		ClientKey:  clientPrincipal.KeyPEM,
		ServerCert: serverPrincipal.CertPEM,
		ServerKey:  serverPrincipal.KeyPEM,
	}, nil
}

type InstanceUserOptions struct {
	InstanceID uuid.UUID
	KeyPrefix  string
	Password   string
}

type InstanceUserCredentials struct {
	Username   string
	Password   string
	CaCert     []byte
	ClientCert []byte
	ClientKey  []byte
}

func CreateInstanceEtcdUser(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	opts InstanceUserOptions,
) (*InstanceUserCredentials, error) {
	username := fmt.Sprintf("instance-%s", opts.InstanceID.String())
	password := opts.Password
	if password == "" {
		pw, err := utils.RandomString(16)
		if err != nil {
			return nil, fmt.Errorf("failed to generate random password: %w", err)
		}
		password = pw
	}

	if err := createRoleIfNotExists(ctx, client, username, opts.KeyPrefix); err != nil {
		return nil, fmt.Errorf("failed to create instance role: %w", err)
	}

	if err := createUserIfNotExists(ctx, client, username, password, username); err != nil {
		return nil, fmt.Errorf("failed to create instance user: %w", err)
	}

	// Create a cert for the instance user. This operation is idempotent.
	clientPrincipal, err := certSvc.InstanceEtcdUser(ctx, opts.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert for etcd host user: %w", err)
	}

	return &InstanceUserCredentials{
		Username:   username,
		Password:   password,
		CaCert:     certSvc.CACert(),
		ClientCert: clientPrincipal.CertPEM,
		ClientKey:  clientPrincipal.KeyPEM,
	}, nil
}

func createRoleIfNotExists(
	ctx context.Context,
	client *clientv3.Client,
	roleName string,
	keyPrefix string,
) error {
	var perms []*authpb.Permission
	resp, err := client.RoleGet(ctx, roleName)
	if errors.Is(err, rpctypes.ErrRoleNotFound) {
		if _, err := client.RoleAdd(ctx, roleName); err != nil {
			return fmt.Errorf("failed to create role %q: %w", roleName, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get role %q: %w", roleName, err)
	} else {
		perms = resp.Perm
	}
	if keyPrefix == "" {
		return nil
	}
	var hasPerm bool
	for _, perm := range perms {
		if string(perm.Key) == keyPrefix {
			hasPerm = true
			break
		}
	}
	if hasPerm {
		return nil
	}
	rangeEnd := clientv3.GetPrefixRangeEnd(keyPrefix)
	permType := clientv3.PermissionType(clientv3.PermReadWrite)
	if _, err := client.RoleGrantPermission(ctx, roleName, keyPrefix, rangeEnd, permType); err != nil {
		return fmt.Errorf("failed to grant role permission: %w", err)
	}

	return nil
}

func createUserIfNotExists(
	ctx context.Context,
	client *clientv3.Client,
	username string,
	password string,
	roleNames ...string,
) error {
	var roles []string
	resp, err := client.UserGet(ctx, username)
	if errors.Is(err, rpctypes.ErrUserNotFound) {
		if _, err := client.UserAdd(ctx, username, password); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get user %q: %w", username, err)
	} else {
		roles = resp.Roles
	}
	haveRoles := ds.NewSet(roles...)
	wantRoles := ds.NewSet(roleNames...)
	for roleName := range wantRoles.Difference(haveRoles) {
		if _, err := client.UserGrantRole(ctx, username, roleName); err != nil {
			return fmt.Errorf("failed to grant role to user: %w", err)
		}
	}
	return nil
}

func RemoveInstanceEtcdUser(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	instanceID uuid.UUID,
) error {
	username := fmt.Sprintf("instance-%s", instanceID.String())

	if err := removeUserIfExists(ctx, client, username); err != nil {
		return fmt.Errorf("failed to create instance user: %w", err)
	}
	if err := removeRoleIfExists(ctx, client, username); err != nil {
		return fmt.Errorf("failed to create instance role: %w", err)
	}
	if err := certSvc.RemoveInstanceEtcdUser(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to remove instance cert: %w", err)
	}

	return nil
}

func removeRoleIfExists(
	ctx context.Context,
	client *clientv3.Client,
	roleName string,
) error {
	_, err := client.RoleDelete(ctx, roleName)
	if errors.Is(err, rpctypes.ErrRoleNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to delete role %q: %w", roleName, err)
	}

	return nil
}

func removeUserIfExists(
	ctx context.Context,
	client *clientv3.Client,
	username string,
) error {
	_, err := client.UserDelete(ctx, username)
	if errors.Is(err, rpctypes.ErrUserNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to delete user %q: %w", username, err)
	}

	return nil
}
