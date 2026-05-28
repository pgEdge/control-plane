package systemd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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

func (d *Apt) InstalledPostgresVersions(ctx context.Context) ([]*InstalledPostgres, error) {
	execCmd := d.ExecCommand
	if execCmd == nil {
		execCmd = DefaultExecCommand
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var stdout, stderr strings.Builder
	err := execCmd(ctx, &stdout, &stderr, "dpkg-query", "-f", "${binary:Package} ${Version}\n", "-W")
	if err != nil {
		return nil, fmt.Errorf("failed to query installed packages: %w, stderr: %s", err, stderr.String())
	}

	return processPackageList(
		aptPostgresPackageNames,
		aptSpockPackageNames,
		stdout.String(),
	)
}

var aptPostgresPackageNames, aptSpockPackageNames = aptPackageNames()

func aptPackageNames() (map[string]string, map[string]string) {
	postgresPackageNames := make(map[string]string, len(supportedPostgresVersions))
	spockPackageNames := make(map[string]string, len(supportedPostgresVersions)*len(supportedSpockVersions))

	for _, postgres := range supportedPostgresVersions {
		postgresPackageName := fmt.Sprintf("pgedge-postgresql-%s", postgres)
		postgresPackageNames[postgresPackageName] = postgres
		for _, spock := range supportedSpockVersions {
			spockPackageName := fmt.Sprintf("pgedge-postgresql-%s-spock%s", postgres, spock)
			spockPackageNames[spockPackageName] = postgres
		}
	}

	return postgresPackageNames, spockPackageNames
}
