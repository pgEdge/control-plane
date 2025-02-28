package general

import (
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/exec"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/spf13/afero"
)

type Activities struct {
	Fs          afero.Fs
	Run         exec.CmdRunner
	Etcd        *etcd.EmbeddedEtcd
	LoopMgr     filesystem.LoopDeviceManager
	HostService *host.Service
}
