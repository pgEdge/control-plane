package etcd

import (
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
)

type ClusterMember struct {
	Name       string
	PeerURLs   []string
	ClientURLs []string
}

type JoinOptions struct {
	Leader      *ClusterMember
	Credentials *HostCredentials
}

type HostCredentialOptions struct {
	HostID              string
	Hostname            string
	IPv4Address         string
	EmbeddedEtcdEnabled bool
}

type HostCredentials struct {
	Username   string
	Password   string
	CaCert     []byte
	ClientCert []byte
	ClientKey  []byte
	ServerCert []byte
	ServerKey  []byte
}

type Etcd interface {
	common.HealthCheckable

	IsInitialized() (bool, error)
	Start(ctx context.Context) error
	Join(ctx context.Context, options JoinOptions) error
	Initialized() <-chan struct{}
	Error() <-chan error
	GetClient() (*clientv3.Client, error)
	Leader(ctx context.Context) (*ClusterMember, error)
	AddHost(ctx context.Context, opts HostCredentialOptions) (*HostCredentials, error)
	RemoveHost(ctx context.Context, hostID string) error
	JoinToken() (string, error)
	VerifyJoinToken(in string) error
	ChangeMode(ctx context.Context, mode config.EtcdMode) (Etcd, error)
}
