package systemd

import (
	"context"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

type InstalledPackage struct {
	PostgresMajor string
	Version       *ds.Version
	Name          string
}

func InstalledPostgresPackage(pkg, ver string) (*InstalledPackage, error) {
	version, err := toVersion(ver)
	if err != nil {
		return nil, err
	}
	pgMajor, err := postgresVersionFromPostgresPkg(pkg)
	if err != nil {
		return nil, err
	}
	return &InstalledPackage{
		PostgresMajor: pgMajor,
		Version:       version,
		Name:          pkg,
	}, nil
}

func InstalledSpockPackage(pkg, ver string) (*InstalledPackage, error) {
	version, err := toVersion(ver)
	if err != nil {
		return nil, err
	}
	pgMajor, err := postgresVersionFromSpockPkg(pkg)
	if err != nil {
		return nil, err
	}
	return &InstalledPackage{
		PostgresMajor: pgMajor,
		Version:       version,
		Name:          pkg,
	}, nil
}

func PackageCmp(a, b *InstalledPackage) int {
	return a.Version.Compare(b.Version)
}

type InstalledPostgres struct {
	Postgres *InstalledPackage
	Spock    []*InstalledPackage
}

func InstalledPostgresCmp(a, b *InstalledPostgres) int {
	return a.Postgres.Version.Compare(b.Postgres.Version)
}

type PackageManager interface {
	InstalledPostgresVersions(ctx context.Context) ([]*InstalledPostgres, error)
	InstanceDataBaseDir(pgMajor string) string
	BinDir(pgMajor string) string
}
