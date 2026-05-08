package systemd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

var (
	supportedPostgresVersions = []string{"16", "17", "18"}
	supportedSpockVersions    = []string{"50"}
)

type InstalledPackage struct {
	PostgresMajor string
	Version       *ds.Version
	Name          string
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

type ExecCommand = func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error

func DefaultExecCommand(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute command '%s': %w", cmd.String(), err)
	}
	return nil
}

func processPackageList(
	postgresPackageNames map[string]string,
	spockPackageNames map[string]string,
	packageList string,
) ([]*InstalledPostgres, error) {
	postgresPackages := map[string]*InstalledPackage{}
	spockPackages := map[string][]*InstalledPackage{}

	for line := range strings.SplitSeq(packageList, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pkg, ver := fields[0], fields[1]
		// Trim architecture qualifier from package name if it exists.
		pkg, _, _ = strings.Cut(pkg, ":")
		if postgresMajor, ok := postgresPackageNames[pkg]; ok {
			installed, err := installedPackage(postgresMajor, pkg, ver)
			if err != nil {
				return nil, err
			}
			postgresPackages[postgresMajor] = installed
		}
		if postgresMajor, ok := spockPackageNames[pkg]; ok {
			installed, err := installedPackage(postgresMajor, pkg, ver)
			if err != nil {
				return nil, err
			}
			spockPackages[postgresMajor] = append(spockPackages[postgresMajor], installed)
		}
	}

	installedPostgres := make([]*InstalledPostgres, 0, len(postgresPackages))
	for postgresMajor, postgresPackage := range postgresPackages {
		spock := spockPackages[postgresMajor]
		slices.SortFunc(spock, PackageCmp)
		installedPostgres = append(installedPostgres, &InstalledPostgres{
			Postgres: postgresPackage,
			Spock:    spock,
		})
	}
	slices.SortFunc(installedPostgres, InstalledPostgresCmp)

	return installedPostgres, nil
}

func installedPackage(postgresMajor, pkg, ver string) (*InstalledPackage, error) {
	version, err := toVersion(ver)
	if err != nil {
		return nil, fmt.Errorf("failed to parse version '%s' for package '%s': %w", ver, pkg, err)
	}
	return &InstalledPackage{
		PostgresMajor: postgresMajor,
		Version:       version,
		Name:          pkg,
	}, nil
}

func toVersion(ver string) (*ds.Version, error) {
	var buf []rune
	var components []string
parseLoop:
	for _, char := range ver {
		switch char {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			buf = append(buf, char)
		case '.':
			if len(buf) > 0 {
				components = append(components, string(buf))
				buf = nil
			}
			if len(components) == 3 {
				// Stop after collecting major, minor, patch
				break parseLoop
			}
		case ':':
			// this is the end of the epoch prefix. we want to discard what
			// we've captured so far
			buf = nil
		default:
			// this could be the start of the build number, or it could be a
			// version format that we don't support.
			break parseLoop
		}
	}
	if len(buf) > 0 {
		components = append(components, string(buf))
	}
	return ds.ParseVersion(strings.Join(components, "."))
}
