package apiv1

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"

	goahttp "goa.design/goa/v3/http"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/version"
)

var _ api.Service = (*PreInitHandlers)(nil)

type PreInitHandlers struct {
	cfg             config.Config
	etcd            *etcd.EmbeddedEtcd
	handlersReadyCh <-chan struct{}
}

func NewPreInitHandlers(cfg config.Config, etcdServer *etcd.EmbeddedEtcd) *PreInitHandlers {
	return &PreInitHandlers{
		cfg:  cfg,
		etcd: etcdServer,
	}
}

func (s *PreInitHandlers) waitForHandlersReady() {
	if s.handlersReadyCh != nil {
		// Block until the handlers are swapped. That way, clients know the
		// server is ready as soon as we return.
		<-s.handlersReadyCh
	}
}

func (s *PreInitHandlers) InitCluster(ctx context.Context) (*api.ClusterJoinToken, error) {
	if err := s.etcd.Start(ctx); err != nil {
		return nil, apiErr(err)
	}

	s.waitForHandlersReady()

	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, apiErr(err)
	}

	// TODO: Https support
	serverURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", s.cfg.IPv4Address, s.cfg.HTTP.Port),
	}

	return &api.ClusterJoinToken{
		Token:     token,
		ServerURL: serverURL.String(),
	}, nil
}

func (s *PreInitHandlers) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	serverURL, err := url.Parse(token.ServerURL)
	if err != nil {
		return ErrInvalidServerURL
	}

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	c := client.NewClient(serverURL.Scheme, serverURL.Host, http.DefaultClient, enc, dec, false)
	cli := &api.Client{
		GetJoinOptionsEndpoint: c.GetJoinOptions(),
	}

	opts, err := cli.GetJoinOptions(ctx, &api.ClusterJoinRequest{
		HostID:      api.Identifier(s.cfg.HostID),
		Hostname:    s.cfg.Hostname,
		Ipv4Address: s.cfg.IPv4Address,
		Token:       token.Token,
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
		Peer: etcd.Peer{
			Name:      opts.Peer.Name,
			PeerURL:   opts.Peer.PeerURL,
			ClientURL: opts.Peer.ClientURL,
		},
		Credentials: &etcd.HostCredentials{
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

	s.waitForHandlersReady()

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

func (s *PreInitHandlers) ListHosts(ctx context.Context) ([]*api.Host, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) GetHost(ctx context.Context, req *api.GetHostPayload) (*api.Host, error) {
	return nil, ErrUninitialized
}

func (s *PreInitHandlers) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	return ErrUninitialized
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

func (s *PreInitHandlers) RestartInstance(ctx context.Context, req *api.RestartInstancePayload) (res *api.Task, err error) {
	return nil, ErrUninitialized
}
