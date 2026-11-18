package systemd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

	osFamily      config.OSFamily
	pgConfigPaths map[string]string
	// servicePaths maps each supported service type to the path of its
	// binary.
	servicePaths map[string]string
	// servicePackages maps the service types whose binary can't report its
	// version to the package that installs it. Their version comes from the
	// package manager instead.
	servicePackages map[string]string

	mu sync.Mutex
	// installed holds the results of the most recent successful call to
	// InstalledPostgresVersions, keyed by Postgres major version.
	installed map[string]*ds.PgEdgeVersion
	// warnings holds the warning messages from the most recent successful
	// call to InstalledPostgresVersions, keyed by Postgres major version.
	warnings map[string][]string
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
	servicePaths := map[string]string{}
	if cfg.PostgRESTPath != "" {
		servicePaths["postgrest"] = cfg.PostgRESTPath
	}
	if cfg.MCPPath != "" {
		servicePaths["mcp"] = cfg.MCPPath
	}
	if cfg.RAGPath != "" {
		servicePaths["rag"] = cfg.RAGPath
	}
	return &InstalledVersionsProbe{
		ExecCommand:   execCommand,
		Fs:            fsys,
		osFamily:      cfg.OSFamily,
		pgConfigPaths: pgConfigPaths,
		servicePaths:  servicePaths,
		servicePackages: map[string]string{
			// The MCP server binary has no version flag.
			"mcp": mcpPackageName,
		},
	}, nil
}

// InstalledServices returns the installed version of each supported service
// type, keyed by service type. Service types whose binary isn't installed are
// omitted.
func (p *InstalledVersionsProbe) InstalledServices(ctx context.Context) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Probe each service concurrently, so that one stalled probe can't use up
	// the deadline before the others start.
	serviceTypes := slices.Sorted(maps.Keys(p.servicePaths))
	results := make([]serviceVersionResult, len(serviceTypes))
	var wg sync.WaitGroup
	for i, serviceType := range serviceTypes {
		wg.Go(func() {
			version, ok, err := p.serviceVersion(ctx, serviceType)
			results[i] = serviceVersionResult{version: version, ok: ok, err: err}
		})
	}
	wg.Wait()

	// A failure to detect one service shouldn't hide the others, so we
	// return every version we could detect along with any errors.
	installed := map[string]string{}
	var errs []error
	for i, result := range results {
		switch {
		case result.err != nil:
			errs = append(errs, result.err)
		case result.ok:
			installed[serviceTypes[i]] = result.version
		}
	}

	return installed, errors.Join(errs...)
}

type serviceVersionResult struct {
	version string
	ok      bool
	err     error
}

// serviceVersion returns the installed version of the given service type. It
// returns false if the service's binary isn't installed.
func (p *InstalledVersionsProbe) serviceVersion(ctx context.Context, serviceType string) (string, bool, error) {
	binPath := p.servicePaths[serviceType]
	stat, err := p.Fs.Stat(binPath)
	switch {
	case errors.Is(err, afero.ErrFileNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("failed to stat %s: %w", binPath, err)
	case stat.IsDir():
		return "", false, fmt.Errorf("%s is a directory", binPath)
	}

	var version string
	if pkg, ok := p.servicePackages[serviceType]; ok {
		version, err = p.packageVersion(ctx, pkg)
	} else {
		version, err = p.binaryVersion(ctx, binPath)
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to get %s version: %w", serviceType, err)
	}
	return version, true, nil
}

// binaryVersion returns the version reported by the binary's --version flag.
func (p *InstalledVersionsProbe) binaryVersion(ctx context.Context, binPath string) (string, error) {
	var stdout, stderr strings.Builder
	if err := p.ExecCommand(ctx, &stdout, &stderr, binPath, "--version"); err != nil {
		return "", fmt.Errorf("%w, stderr: %s", err, &stderr)
	}
	return parseServiceVersionOutput(stdout.String())
}

// packageVersion returns the upstream version of the given installed
// package, without the package revision, e.g. "1.1.0-1.trixie" -> "1.1.0".
func (p *InstalledVersionsProbe) packageVersion(ctx context.Context, pkg string) (string, error) {
	var name string
	var args []string
	switch p.osFamily {
	case config.OSFamilyRedHat:
		name, args = "rpm", []string{"-q", "--queryformat", "%{VERSION}", pkg}
	case config.OSFamilyDebian:
		name, args = "dpkg-query", []string{"--show", "--showformat", "${Version}", pkg}
	default:
		return "", fmt.Errorf("unrecognized os family '%s'", p.osFamily)
	}

	var stdout, stderr strings.Builder
	if err := p.ExecCommand(ctx, &stdout, &stderr, name, args...); err != nil {
		return "", fmt.Errorf("failed to query package %s: %w, stderr: %s", pkg, err, &stderr)
	}
	return parsePackageVersion(stdout.String())
}

// parsePackageVersion extracts the upstream version from a package version,
// dropping any epoch ("1:") and revision ("-1.trixie"). The revision follows
// the last hyphen, because Debian upstream versions can contain hyphens. RPM
// versions can't, and the rpm query only selects the version. Packages mark
// pre-releases with a tilde so that they sort before the release, e.g.
// "1.2.0~beta1", which we convert to "1.2.0-beta1".
func parsePackageVersion(output string) (string, error) {
	version := strings.TrimSpace(output)
	if _, after, ok := strings.Cut(version, ":"); ok {
		version = after
	}
	if i := strings.LastIndex(version, "-"); i >= 0 {
		version = version[:i]
	}
	version = strings.ReplaceAll(version, "~", "-")
	if _, err := ds.ParseVersion(version); err != nil {
		return "", fmt.Errorf("no version found in package version %q", strings.TrimSpace(output))
	}
	return version, nil
}

// parseServiceVersionOutput extracts the version from a service binary's
// --version output, e.g. "PostgREST 14.5" -> "14.5". It returns the first
// whitespace-separated field that parses as a version, so that trailing
// build information is ignored.
func parseServiceVersionOutput(output string) (string, error) {
	for field := range strings.FieldsSeq(output) {
		field = strings.TrimPrefix(field, "v")
		if _, err := ds.ParseVersion(field); err == nil {
			return field, nil
		}
	}
	return "", fmt.Errorf("no version found in output %q", strings.TrimSpace(output))
}

// versionWarning is a problem with an installed Postgres version that doesn't
// prevent that version from being discovered, such as a failure to list its
// libraries.
type versionWarning struct {
	err error
}

func (e *versionWarning) Error() string {
	return e.err.Error()
}

func (e *versionWarning) Unwrap() error {
	return e.err
}

// InstalledPostgresVersion returns the given Postgres major version from the
// most recent successful call to InstalledPostgresVersions. It returns false
// if that call didn't find the version.
func (p *InstalledVersionsProbe) InstalledPostgresVersion(pgMajor string) (*ds.PgEdgeVersion, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	version, ok := p.installed[pgMajor]
	if !ok {
		return nil, false
	}
	return version.Clone(), true
}

// hadWarning returns true if the most recent successful call to
// InstalledPostgresVersions reported the given warning for the given Postgres
// major version.
func (p *InstalledVersionsProbe) hadWarning(pgMajor string, warning error) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Contains(p.warnings[pgMajor], warning.Error())
}

func (p *InstalledVersionsProbe) setInstalledPostgresVersions(
	versions []*ds.PgEdgeVersion,
	warnings map[string][]error,
) {
	installed := make(map[string]*ds.PgEdgeVersion, len(versions))
	for _, version := range versions {
		if pgMajor, ok := version.PostgresVersion.MajorString(); ok {
			installed[pgMajor] = version.Clone()
		}
	}
	messages := make(map[string][]string, len(warnings))
	for pgMajor, errs := range warnings {
		for _, err := range errs {
			messages[pgMajor] = append(messages[pgMajor], err.Error())
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.installed = installed
	p.warnings = messages
}

// InstalledPostgresVersions returns the installed Postgres versions, sorted in
// descending order. A failure to list a version's libraries keeps the list
// from the previous call, so that a transient failure doesn't change the
// defaults or validation for that version. The list is left nil if there was
// no previous list, since its contents are unknown. A version whose Spock
// control file is installed without its library is reported without Spock,
// and a version with an unsupported Spock version is skipped.
//
// Every discovered version is returned along with an error that wraps a
// *versionWarning for each of these problems and each skipped version. Since this is called
// periodically, a warning is only returned if the previous successful call
// didn't report it for the same version.
func (p *InstalledVersionsProbe) InstalledPostgresVersions(ctx context.Context) ([]*ds.PgEdgeVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var installed []*ds.PgEdgeVersion
	warnings := map[string][]error{}
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
		// These warnings are only reported if this version is discovered.
		var versionWarnings []error
		libraries, err := installedLibraries(p.Fs, out.pkgLibDir)
		if err != nil {
			if previous, ok := p.InstalledPostgresVersion(pgMajor); ok {
				libraries = previous.AvailableLibraries
			}
			versionWarnings = append(versionWarnings, fmt.Errorf("failed to get installed libraries for postgres %s: %w", pgMajor, err))
		}
		postgres := &ds.PgEdgeVersion{
			PostgresVersion:    pgVersion,
			Stability:          prereleaseStability(pgVersion),
			AvailableLibraries: libraries,
		}

		spockVersion, err := extensionVersion(p.Fs, out.shareDir, "spock")
		if err != nil {
			return nil, fmt.Errorf("failed to get installed spock version for postgres %s: %w", pgMajor, err)
		}
		switch {
		case spockVersion == nil:
			// Spock is optional. This version will be reported without it.
		case libraries != nil && !slices.Contains(libraries, "spock"):
			// Spock's control file is installed without its library, so
			// Postgres would fail to start with spock preloaded. An unknown
			// list of libraries is not checked.
			versionWarnings = append(versionWarnings, fmt.Errorf("spock %s is installed for postgres %s without its library in %s, so postgres %s will be available without spock", spockVersion, pgMajor, out.pkgLibDir, pgMajor))
		case supportedSpockVersions.IsSatisfied(spockVersion):
			postgres.SpockVersion = spockVersion
			// Normalize drops the Spock pre-release, so we need to reflect it
			// in the stability if Postgres is otherwise stable.
			if postgres.Stability == "" {
				postgres.Stability = prereleaseStability(spockVersion)
			}
		default:
			// An unsupported Spock version is installed. We skip this Postgres
			// version rather than reporting it without Spock, since that would
			// be surprising to someone who intentionally installed Spock. The
			// other warnings don't apply to a version that isn't discovered.
			warnings[pgMajor] = []error{fmt.Errorf("spock %s is installed for postgres %s, but it isn't supported, so postgres %s will not be available", spockVersion, pgMajor, pgMajor)}
			continue
		}
		normalized, err := postgres.Normalize()
		if err != nil {
			return nil, fmt.Errorf("failed to normalize version for postgres %s: %w", pgMajor, err)
		}

		installed = append(installed, normalized)
		if len(versionWarnings) > 0 {
			warnings[pgMajor] = versionWarnings
		}
	}
	slices.SortFunc(installed, func(a, b *ds.PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})

	var newWarnings []error
	for _, pgMajor := range monitoredPgVersions {
		for _, warning := range warnings[pgMajor] {
			if !p.hadWarning(pgMajor, warning) {
				newWarnings = append(newWarnings, &versionWarning{err: warning})
			}
		}
	}
	p.setInstalledPostgresVersions(installed, warnings)

	return installed, errors.Join(newWarnings...)
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
	version   *ds.Version
	shareDir  string
	pkgLibDir string
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
//	PKGLIBDIR = /usr/lib/postgresql/17/lib
//	VERSION = PostgreSQL 17.2 (Debian 17.2-1.pgdg120+1)
func parsePgConfigOutput(output string) (pgConfigOutput, error) {
	var rawVersion, shareDir, pkgLibDir string
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
		case "PKGLIBDIR":
			pkgLibDir = strings.TrimSpace(value)
		}
	}
	if rawVersion == "" {
		return pgConfigOutput{}, errors.New("VERSION not found in pg_config output")
	}
	if shareDir == "" {
		return pgConfigOutput{}, errors.New("SHAREDIR not found in pg_config output")
	}
	if pkgLibDir == "" {
		return pgConfigOutput{}, errors.New("PKGLIBDIR not found in pg_config output")
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
		version:   version,
		shareDir:  shareDir,
		pkgLibDir: pkgLibDir,
	}, nil
}

// installedLibraries returns the names of the shared libraries in the given
// package library directory, without their ".so" extension, sorted in
// ascending order. These are the names that Postgres can load from $libdir,
// e.g. via shared_preload_libraries. It returns nil if the directory does not
// exist.
func installedLibraries(fsys afero.Fs, pkgLibDir string) ([]string, error) {
	entries, err := afero.ReadDir(fsys, pkgLibDir)
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", pkgLibDir, err)
	}

	var names []string
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".so")
		if !ok || name == "" || entry.IsDir() {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
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
