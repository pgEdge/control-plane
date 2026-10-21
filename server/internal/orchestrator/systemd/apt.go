package systemd

import (
	"path/filepath"
)

var _ PackageManager = (*Apt)(nil)

type Apt struct {
	ExecCommand ExecCommand
}

func (d *Apt) InstanceDataBaseDir(pgMajor string) string {
	return filepath.Join("/var/lib/postgresql", pgMajor)
}

func (d *Apt) BinDir(pgMajor string) string {
	return filepath.Join("/usr/lib/postgresql", pgMajor, "bin")
}
