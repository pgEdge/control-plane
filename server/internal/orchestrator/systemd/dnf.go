package systemd

import (
	"fmt"
	"path/filepath"
)

var _ PackageManager = (*Dnf)(nil)

type Dnf struct {
	ExecCommand ExecCommand
}

func (d *Dnf) InstanceDataBaseDir(pgMajor string) string {
	return filepath.Join("/var/lib/pgsql", pgMajor)
}

func (d *Dnf) BinDir(pgMajor string) string {
	return fmt.Sprintf("/usr/pgsql-%s/bin", pgMajor)
}
