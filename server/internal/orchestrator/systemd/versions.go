package systemd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/spf13/afero"
)

var monitoredPgVersions = []string{"16", "17", "18"}

// supportedSpockVersions matches any 5.x or 6.x version. The max needs all
// three components because a shorter version sorts before a longer one with
// the same prefix, e.g. 6 < 6.0.0.
var supportedSpockVersions = ds.VersionConstraint{
	Min: &ds.Version{Components: []uint64{5}},
	Max: &ds.Version{Components: []uint64{6, math.MaxUint64, math.MaxUint64}},
}

var errPgConfigNotFound = errors.New("pg_config not found")

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

type InstalledVersionsProbe struct {
	ExecCommand ExecCommand
	Fs          afero.Fs

	pgConfigPaths map[string]string
}

func NewInstalledVersionsProbe(
	execCommand ExecCommand,
	fsys afero.Fs,
	cfg config.SystemD,
) (*InstalledVersionsProbe, error) {
	pgConfigPaths := make(map[string]string, len(monitoredPgVersions))
	for _, pgMajor := range monitoredPgVersions {
		pgBinDir := cfg.PgBinDir(pgMajor)
		if pgBinDir == "" {
			return nil, fmt.Errorf("failed to determine bin dir for postgres %s: unrecognized os family '%s'", pgMajor, cfg.OSFamily)
		}
		pgConfigPaths[pgMajor] = filepath.Join(pgBinDir, "pg_config")
	}
	return &InstalledVersionsProbe{
		ExecCommand:   execCommand,
		Fs:            fsys,
		pgConfigPaths: pgConfigPaths,
	}, nil
}

func (p *InstalledVersionsProbe) InstalledPostgresVersions(ctx context.Context) ([]*ds.PgEdgeVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var installed []*ds.PgEdgeVersion
	for _, pgMajor := range monitoredPgVersions {
		pgConfig := p.pgConfigPaths[pgMajor]
		out, err := p.execPgConfig(ctx, pgConfig)
		if errors.Is(err, errPgConfigNotFound) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to get installed postgres %s version: %w", pgMajor, err)
		}
		if major, ok := out.version.MajorString(); !ok || major != pgMajor {
			return nil, fmt.Errorf("pg_config for postgres %s reported unexpected version %s", pgMajor, out.version)
		}
		pgVersion := &ds.Version{
			Components: slices.Clone(out.version.Components),
			PreRelease: out.version.PreRelease,
		}
		if len(pgVersion.Components) == 1 {
			// Postgres pre-release versions don't include a minor version.
			pgVersion.Components = append(pgVersion.Components, 0)
		}
		postgres := &ds.PgEdgeVersion{
			PostgresVersion: pgVersion,
			Stability:       prereleaseStability(pgVersion),
		}

		spockVersion, err := extensionVersion(p.Fs, out.shareDir, "spock")
		if err != nil {
			return nil, fmt.Errorf("failed to get installed spock version for postgres %s: %w", pgMajor, err)
		}
		if spockVersion != nil && supportedSpockVersions.IsSatisfied(spockVersion) {
			postgres.SpockVersion = spockVersion
			// Normalize drops the Spock pre-release, so we need to reflect it
			// in the stability if Postgres is otherwise stable.
			if postgres.Stability == "" {
				postgres.Stability = prereleaseStability(spockVersion)
			}
		} else {
			// TODO(PLAT-752): Remove this condition once we've made Spock
			// optional.
			continue
		}
		normalized, err := postgres.Normalize()
		if err != nil {
			return nil, fmt.Errorf("failed to normalize version for postgres %s: %w", pgMajor, err)
		}

		installed = append(installed, normalized)
	}
	slices.SortFunc(installed, func(a, b *ds.PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})

	return installed, nil
}

// prereleaseStability returns the stability for the given version's
// pre-release, e.g. rc1 -> rc, beta.2 -> beta. It returns an empty stability
// for release versions.
func prereleaseStability(v *ds.Version) ds.Stability {
	if !v.IsPrerelease() {
		return ""
	}
	stability := ds.Stability(strings.TrimRight(v.PreRelease, "0123456789.-"))
	if stability.Normalized() == ds.StabilityStable || !stability.Valid() {
		// Pre-releases must be marked as unstable, so we fall back to a valid
		// pre-release stability.
		return ds.StabilityDev
	}
	return stability
}

type pgConfigOutput struct {
	version  *ds.Version
	shareDir string
}

func (p *InstalledVersionsProbe) execPgConfig(ctx context.Context, pgConfig string) (pgConfigOutput, error) {
	stat, err := p.Fs.Stat(pgConfig)
	switch {
	case errors.Is(err, afero.ErrFileNotFound):
		return pgConfigOutput{}, fmt.Errorf("%w: %s", errPgConfigNotFound, pgConfig)
	case err != nil:
		return pgConfigOutput{}, fmt.Errorf("failed to stat %s: %w", pgConfig, err)
	case stat.IsDir():
		return pgConfigOutput{}, fmt.Errorf("%s is a directory", pgConfig)
	}
	var stdout, stderr strings.Builder
	if err := p.ExecCommand(ctx, &stdout, &stderr, pgConfig); err != nil {
		return pgConfigOutput{}, fmt.Errorf("failed to execute pg_config: %w, stderr: %s", err, &stderr)
	}

	return parsePgConfigOutput(stdout.String())
}

// parsePgConfigOutput parses the output of pg_config when it's executed
// without any arguments. The output contains one "KEY = value" pair per line,
// e.g.:
//
//	SHAREDIR = /usr/share/postgresql/17
//	VERSION = PostgreSQL 17.2 (Debian 17.2-1.pgdg120+1)
func parsePgConfigOutput(output string) (pgConfigOutput, error) {
	var rawVersion, shareDir string
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "VERSION":
			rawVersion = strings.TrimSpace(value)
		case "SHAREDIR":
			shareDir = strings.TrimSpace(value)
		}
	}
	if rawVersion == "" {
		return pgConfigOutput{}, errors.New("VERSION not found in pg_config output")
	}
	if shareDir == "" {
		return pgConfigOutput{}, errors.New("SHAREDIR not found in pg_config output")
	}

	// The version value looks like "PostgreSQL 17.2 (Debian 17.2-1.pgdg120+1)"
	// or "PostgreSQL 18beta1".
	fields := strings.Fields(rawVersion)
	if len(fields) < 2 || fields[0] != "PostgreSQL" {
		return pgConfigOutput{}, fmt.Errorf("unrecognized pg_config version %q", rawVersion)
	}
	version, err := ds.ParseVersion(fields[1])
	if err != nil {
		return pgConfigOutput{}, fmt.Errorf("failed to parse pg_config version %q: %w", rawVersion, err)
	}

	return pgConfigOutput{
		version:  version,
		shareDir: shareDir,
	}, nil
}

// extensionVersion reads the default_version from the given extension's
// control file. It returns a nil version if the extension is not installed.
func extensionVersion(fsys afero.Fs, shareDir, extension string) (*ds.Version, error) {
	controlPath := filepath.Join(shareDir, "extension", extension+".control")
	f, err := fsys.Open(controlPath)
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", controlPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok || strings.TrimSpace(key) != "default_version" {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(value), "'\"")
		version, err := ds.ParseVersion(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse default_version %q from %s: %w", raw, controlPath, err)
		}
		return version, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", controlPath, err)
	}

	return nil, fmt.Errorf("default_version not found in %s", controlPath)
}
