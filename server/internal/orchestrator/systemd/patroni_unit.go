package systemd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/unit"
	"github.com/pgEdge/control-plane/server/internal/database"
)

func PatroniUnitOptions(
	paths database.InstancePaths,
	pgBinPath string,
	cpus float64,
	memoryBytes uint64,
) []*unit.UnitOption {
	pathEnv := pgBinPath
	if p := os.Getenv("PATH"); p != "" {
		pathEnv += ":" + p
	}
	patroniCmd := fmt.Sprintf("%s %s", paths.PatroniPath, paths.Instance.PatroniConfig())
	pgServiceFileEnv := filepath.Join(paths.Instance.Configs(), "pg_service.conf")

	return UnitFile{
		Unit: UnitSection{
			After: []string{"syslog.target", "network.target"},
		},
		Service: ServiceSection{
			Type:        ServiceTypeSimple,
			User:        "postgres",
			ExecStart:   patroniCmd,
			ExecReload:  "/bin/kill -s HUP $MAINPID",
			KillMode:    ServiceKillModeProcess,
			TimeoutSec:  30,
			CPUs:        cpus,
			MemoryBytes: memoryBytes,
			Restart:     ServiceRestartOnFailure,
			Environment: map[string]string{
				"PATH":          pathEnv,
				"PGSERVICEFILE": pgServiceFileEnv,
			},
		},
		Install: InstallSection{
			WantedBy: []string{"multi-user.target"},
		},
	}.Options()
}
