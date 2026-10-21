package systemd

type PackageManager interface {
	InstanceDataBaseDir(pgMajor string) string
	BinDir(pgMajor string) string
}
