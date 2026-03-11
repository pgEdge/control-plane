package apiv1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/server/internal/cluster"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/version"
)

var _ api.Service = (*PreInitHandlers)(nil)

type PreInitHandlers struct {
	cfg             config.Config
	etcd            etcd.Etcd
	handlersReadyCh <-chan error
}

func NewPreInitHandlers(cfg config.Config, etcdServer etcd.Etcd) *PreInitHandlers {
	return &PreInitHandlers{
		cfg:  cfg,
		etcd: etcdServer,
	}
}

func (s *PreInitHandlers) waitForHandlersReady() error {
	if s.handlersReadyCh != nil {
		// Block until the handlers are swapped. That way, clients know the
		// server is ready as soon as we return.
		return <-s.handlersReadyCh
	}

	return nil
}

func (s *PreInitHandlers) InitCluster(ctx context.Context, req *api.InitClusterRequest) (*api.ClusterJoinToken, error) {
	if err := s.etcd.Start(ctx); err != nil {
		return nil, apiErr(err)
	}

	etcdClient, err := s.etcd.GetClient()
	if err != nil {
		return nil, apiErr(err)
	}
	clusterStore := cluster.NewStore(etcdClient, s.cfg.EtcdKeyRoot)

	id := uuid.NewString() // default to uuid unless specified in request
	if req.ClusterID != nil {
		id, err = identToString(*req.ClusterID, []string{"cluster_id"})
		if err != nil {
			return nil, apiErr(err)
		}
	}
	if err := clusterStore.Cluster.
		Create(&cluster.StoredCluster{ID: id}).
		Exec(ctx); err != nil {
		return nil, apiErr(err)
	}

	if err := s.waitForHandlersReady(); err != nil {
		return nil, apiErr(err)
	}

	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.ClusterJoinToken{
		Token:      token,
		ServerUrls: GetServerURLs(s.cfg),
	}, nil
}

func (s *PreInitHandlers) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	cli, err := s.apiClient(ctx, token.ServerUrls)
	if err != nil {
		return apiErr(err)
	}

	opts, err := cli.GetJoinOptions(ctx, &api.ClusterJoinRequest{
		HostID:              api.Identifier(s.cfg.HostID),
		Addresses:           s.cfg.PeerAddresses,
		Token:               token.Token,
		EmbeddedEtcdEnabled: s.cfg.EtcdMode == config.EtcdModeServer,
	})
	if err != nil {
		return apiErr(err)
	}

	caCert, err := base64.StdEncoding.DecodeString(opts.Credentials.CaCert)
	if err != nil {
		return apiErr(fmt.Errorf("failed to decode CA certificate: %w", err))
	}
	clientCert, err := base64.StdEncoding.DecodeString(opts.Credentials.ClientCert)
	if err != nil {
		return apiErr(fmt.Errorf("failed to decode client certificate: %w", err))
	}
	clientKey, err := base64.StdEncoding.DecodeString(opts.Credentials.ClientKey)
	if err != nil {
		return apiErr(fmt.Errorf("failed to decode client key: %w", err))
	}
	serverCert, err := base64.StdEncoding.DecodeString(opts.Credentials.ServerCert)
	if err != nil {
		return apiErr(fmt.Errorf("failed to decode server certificate: %w", err))
	}
	serverKey, err := base64.StdEncoding.DecodeString(opts.Credentials.ServerKey)
	if err != nil {
		return apiErr(fmt.Errorf("failed to decode server key: %w", err))
	}

	err = s.etcd.Join(ctx, etcd.JoinOptions{
		Leader: &etcd.ClusterMember{
			Name:       opts.Leader.Name,
			PeerURLs:   opts.Leader.PeerUrls,
			ClientURLs: opts.Leader.ClientUrls,
		},
		Credentials: &etcd.HostCredentials{
			Username:   opts.Credentials.Username,
			Password:   opts.Credentials.Password,
			CaCert:     caCert,
			ClientCert: clientCert,
			ClientKey:  clientKey,
			ServerCert: serverCert,
			ServerKey:  serverKey,
		},
	})
	if err != nil {
		return apiErr(fmt.Errorf("failed to join existing cluster: %w", err))
	}

	if err := s.waitForHandlersReady(); err != nil {
		return apiErr(err)
	}

	return nil
}

func (s *PreInitHandlers) GetVersion(context.Context) (res *api.VersionInfo, err error) {
	info, err := version.GetInfo()
	if err != nil {
		return nil, apiErr(err)
	}

	return &api.VersionInfo{
		Version:      info.Version,
		Revision:     info.Revision,
		RevisionTime: info.RevisionTime,
		Arch:         info.Arch,
	}, nil
}

func (s *PreInitHandlers) GetJoinOptions(ctx context.Context, req *api.ClusterJoinRequest) (*api.ClusterJoinOptions, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrUninitialized
}

func (s *PreInitHandlers) GetCluster(ctx context.Context) (*api.Cluster, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ListHosts(ctx context.Context) (*api.ListHostsResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) (*api.RemoveHostResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ListDatabases(ctx context.Context) (*api.ListDatabasesResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.CreateDatabaseResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetDatabase(ctx context.Context, req *api.GetDatabasePayload) (*api.Database, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.UpdateDatabaseResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (*api.DeleteDatabaseResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) BackupDatabaseNode(ctx context.Context, req *api.BackupDatabaseNodePayload) (*api.BackupDatabaseNodeResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) SwitchoverDatabaseNode(ctx context.Context, req *api.SwitchoverDatabaseNodePayload) (*api.SwitchoverDatabaseNodeResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) FailoverDatabaseNode(ctx context.Context, req *api.FailoverDatabaseNodeRequest) (*api.FailoverDatabaseNodeResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) (*api.ListDatabaseTasksResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetDatabaseTask(ctx context.Context, req *api.GetDatabaseTaskPayload) (*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (res *api.RestoreDatabaseResponse, err error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (res *api.RestartInstanceResponse, err error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) StopInstance(ctx context.Context, req *api.StopInstancePayload) (res *api.StopInstanceResponse, err error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) StartInstance(ctx context.Context, req *api.StartInstancePayload) (res *api.StartInstanceResponse, err error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) CancelDatabaseTask(ctx context.Context, req *api.CancelDatabaseTaskPayload) (*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ListHostTasks(ctx context.Context, req *api.ListHostTasksPayload) (*api.ListHostTasksResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetHostTask(ctx context.Context, req *api.GetHostTaskPayload) (*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetHostTaskLog(ctx context.Context, req *api.GetHostTaskLogPayload) (*api.TaskLog, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) ListTasks(ctx context.Context, req *api.ListTasksPayload) (*api.ListTasksResponse, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) httpClient() (res *http.Client, err error) {
	if s.cfg.HTTP.ClientCert == "" {
		return http.DefaultClient, nil
	}

	cert, err := tls.LoadX509KeyPair(s.cfg.HTTP.ClientCert, s.cfg.HTTP.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %w", err)
	}

	caCert, err := os.ReadFile(s.cfg.HTTP.CACert)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA Cert: %w", err)
	}
	caCertPool := x509.NewCertPool()
	ok := caCertPool.AppendCertsFromPEM(caCert)
	if !ok {
		return nil, fmt.Errorf("failed to use CA cert")
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      caCertPool,
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS13,
			},
		},
	}, nil
}

func (s *PreInitHandlers) apiClient(ctx context.Context, serverURLs []string) (*api.Client, error) {
	httpClient, err := s.httpClient()
	if err != nil {
		return nil, err
	}

	for _, u := range serverURLs {
		serverURL, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("invalid server URL '%s': %w", u, err)
		}
		enc := goahttp.RequestEncoder
		dec := goahttp.ResponseDecoder
		c := client.NewClient(serverURL.Scheme, serverURL.Host, httpClient, enc, dec, false)
		apiClient := &api.Client{
			GetJoinOptionsEndpoint: c.GetJoinOptions(),
			GetVersionEndpoint:     c.GetVersion(),
		}

		// Validate the URL by calling the version endpoint
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err = apiClient.GetVersion(reqCtx)
		if err == nil {
			return apiClient, nil
		}
	}

	return nil, fmt.Errorf("failed to reach any of the given servers: %s", strings.Join(serverURLs, ", "))
}
