package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	goahttp "goa.design/goa/v3/http"

	api "github.com/pgEdge/control-plane/api/gen/control_plane"
	"github.com/pgEdge/control-plane/api/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/version"
)

var ErrUninitialized = api.MakeClusterNotInitialized(errors.New("cluster is not initialized"))

var _ api.Service = (*PreInitService)(nil)

type PreInitService struct {
	cfg  config.Config
	etcd *etcd.EmbeddedEtcd
}

func NewPreInitService(cfg config.Config, etcd *etcd.EmbeddedEtcd) *PreInitService {
	return &PreInitService{
		cfg:  cfg,
		etcd: etcd,
	}
}

func (s *PreInitService) InitCluster(ctx context.Context) (*api.ClusterJoinToken, error) {
	if err := s.etcd.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start etcd: %w", err)
	}
	token, err := s.etcd.JoinToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get join token: %w", err)
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

func (s *PreInitService) JoinCluster(ctx context.Context, token *api.ClusterJoinToken) error {
	serverURL, err := url.Parse(token.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid server URL %q: %w", serverURL, err)
	}

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	c := client.NewClient(serverURL.Scheme, serverURL.Host, http.DefaultClient, enc, dec, false)
	cli := &api.Client{
		GetJoinOptionsEndpoint: c.GetJoinOptions(),
	}

	opts, err := cli.GetJoinOptions(ctx, &api.ClusterJoinRequest{
		HostID:      s.cfg.HostID.String(),
		Hostname:    s.cfg.Hostname,
		Ipv4Address: s.cfg.IPv4Address,
		Token:       token.Token,
	})
	if err != nil {
		return err
	}

	caCert, err := base64.StdEncoding.DecodeString(opts.Credentials.CaCert)
	if err != nil {
		return fmt.Errorf("failed to decode CA certificate: %w", err)
	}
	clientCert, err := base64.StdEncoding.DecodeString(opts.Credentials.ClientCert)
	if err != nil {
		return fmt.Errorf("failed to decode client certificate: %w", err)
	}
	clientKey, err := base64.StdEncoding.DecodeString(opts.Credentials.ClientKey)
	if err != nil {
		return fmt.Errorf("failed to decode client key: %w", err)
	}
	serverCert, err := base64.StdEncoding.DecodeString(opts.Credentials.ServerCert)
	if err != nil {
		return fmt.Errorf("failed to decode server certificate: %w", err)
	}
	serverKey, err := base64.StdEncoding.DecodeString(opts.Credentials.ServerKey)
	if err != nil {
		return fmt.Errorf("failed to decode server key: %w", err)
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
		return fmt.Errorf("failed to join existing cluster: %w", err)
	}

	return nil
}

func (s *PreInitService) GetVersion(context.Context) (res *api.VersionInfo, err error) {
	info, err := version.GetInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get version info: %w", err)
	}

	return &api.VersionInfo{
		Version:      info.Version,
		Revision:     info.Revision,
		RevisionTime: info.RevisionTime,
		Arch:         info.Arch,
	}, nil
}

func (s *PreInitService) GetJoinOptions(ctx context.Context, req *api.ClusterJoinRequest) (*api.ClusterJoinOptions, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) GetJoinToken(ctx context.Context) (*api.ClusterJoinToken, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrUninitialized
}

func (s *PreInitService) InspectCluster(ctx context.Context) (*api.Cluster, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) ListHosts(ctx context.Context) ([]*api.Host, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) InspectHost(ctx context.Context, req *api.InspectHostPayload) (*api.Host, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	return ErrUninitialized
}

func (s *PreInitService) ListDatabases(ctx context.Context) (api.DatabaseCollection, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.Database, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) InspectDatabase(ctx context.Context, req *api.InspectDatabasePayload) (*api.Database, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.Database, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) error {
	return ErrUninitialized
}

func (s *PreInitService) InitiateDatabaseBackup(ctx context.Context, req *api.InitiateDatabaseBackupPayload) (*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) ListDatabaseTasks(ctx context.Context, req *api.ListDatabaseTasksPayload) ([]*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) InspectDatabaseTask(ctx context.Context, req *api.InspectDatabaseTaskPayload) (*api.Task, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) GetDatabaseTaskLog(ctx context.Context, req *api.GetDatabaseTaskLogPayload) (*api.TaskLog, error) {
	return nil, ErrUninitialized
}

func (s *PreInitService) RestoreDatabase(ctx context.Context, req *api.RestoreDatabasePayload) (res *api.RestoreDatabaseResponse, err error) {
	return nil, ErrUninitialized
}
