package swarm

import (
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/exec"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/ipam"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Activities struct {
	Fs          afero.Fs
	IPAM        *ipam.Service
	Docker      *docker.Docker
	CertService *certificates.Service
	Etcd        *etcd.EmbeddedEtcd
	EtcdClient  *clientv3.Client
	Run         exec.CmdRunner
	HostService *host.Service
	Config      config.Config
}
