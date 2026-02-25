package systemd

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

var _ PackageManager = (*Dnf)(nil)

type Dnf struct{}

func (d *Dnf) InstanceDataBaseDir(pgMajor string) string {
	return filepath.Join("/var/lib/pgsql", pgMajor)
}

func (d *Dnf) BinDir(pgMajor string) string {
	return fmt.Sprintf("/usr/pgsql-%s/bin", pgMajor)
}

func (d *Dnf) InstalledPostgresVersions(ctx context.Context) ([]*InstalledPostgres, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	args := append([]string{"list", "--installed"}, supportedDnfPackages()...)
	cmd := exec.CommandContext(ctx, "dnf", args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no matching packages to list") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to execute command: %w, output: %s", err, string(out))
	}

	installed := map[string]*InstalledPostgres{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)

		if len(fields) < 2 {
			continue
		}

		pkg, ver := fields[0], fields[1]
		switch {
		case strings.HasPrefix(pkg, "pgedge-postgresql"):
			inst, err := InstalledPostgresPackage(pkg, ver)
			if err != nil {
				return nil, err
			}
			postgres, ok := installed[inst.PostgresMajor]
			if !ok {
				postgres = &InstalledPostgres{}
				installed[inst.PostgresMajor] = postgres
			}
			postgres.Postgres = inst
		case strings.HasPrefix(pkg, "pgedge-spock"):
			inst, err := InstalledSpockPackage(pkg, ver)
			if err != nil {
				return nil, err
			}
			postgres, ok := installed[inst.PostgresMajor]
			if !ok {
				postgres = &InstalledPostgres{}
				installed[inst.PostgresMajor] = postgres
			}
			postgres.Spock = append(postgres.Spock, inst)
		}
	}

	ret := slices.Collect(maps.Values(installed))
	for i := range ret {
		slices.SortFunc(ret[i].Spock, PackageCmp)
	}
	slices.SortFunc(ret, InstalledPostgresCmp)

	return ret, nil
}

var supportedPostgresVersions = []string{"16", "17", "18"}
var supportedSpockVersions = []string{"50"}

func supportedDnfPackages() []string {
	var packages []string
	for _, postgres := range supportedPostgresVersions {
		packages = append(packages, fmt.Sprintf("pgedge-postgresql%s", postgres))

		for _, spock := range supportedSpockVersions {
			packages = append(packages, fmt.Sprintf("pgedge-spock%s_%s", spock, postgres))
		}
	}

	return packages
}

var digits = regexp.MustCompile(`\d+`)

func postgresVersionFromSpockPkg(pkg string) (string, error) {
	// pkg should look like pgedge-spock50_18.aarch64, so we want to extract the
	// second match.
	matches := digits.FindAllString(pkg, 2)
	if len(matches) < 2 {
		return "", fmt.Errorf("unexpected format for spock package '%s'", pkg)
	}
	return matches[1], nil
}

func postgresVersionFromPostgresPkg(pkg string) (string, error) {
	// pkg should look like pgedge-postgresql18.aarch64, so we want to extract the
	// first match.
	matches := digits.FindAllString(pkg, 1)
	if len(matches) == 0 {
		return "", fmt.Errorf("unexpected format for postgres package '%s'", pkg)
	}
	return matches[0], nil
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
