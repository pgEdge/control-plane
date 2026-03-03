package systemd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/unit"

	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
)

func patroniUnitOptions(paths common.InstancePaths, pgBinPath string) []*unit.UnitOption {
	pathEnv := "PATH=" + pgBinPath
	if p := os.Getenv("PATH"); p != "" {
		pathEnv += ":" + p
	}

	return []*unit.UnitOption{
		{
			Section: "Unit",
			Name:    "After",
			Value:   "syslog.target network.target",
		},
		{
			Section: "Service",
			Name:    "Type",
			Value:   "simple",
		},
		{
			Section: "Service",
			Name:    "User",
			Value:   "postgres",
		},
		{
			Section: "Service",
			Name:    "ExecStart",
			Value:   fmt.Sprintf("%s %s", paths.PatroniPath, paths.Instance.PatroniConfig()),
		},
		{
			Section: "Service",
			Name:    "ExecReload",
			Value:   "/bin/kill -s HUP $MAINPID",
		},
		{
			Section: "Service",
			Name:    "KillMode",
			Value:   "process",
		},
		{
			Section: "Service",
			Name:    "TimeoutSec",
			Value:   "30",
		},
		{
			Section: "Service",
			Name:    "Restart",
			Value:   "on-failure",
		},
		{
			Section: "Service",
			Name:    "Environment",
			Value:   pathEnv,
		},
		{
			Section: "Service",
			Name:    "Environment",
			Value:   "PGSERVICEFILE=" + filepath.Join(paths.Instance.Configs(), "pg_service.conf"),
		},
		{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target",
		},
	}
}
