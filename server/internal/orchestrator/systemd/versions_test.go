package systemd_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/systemd"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstalledVersionsProbe(t *testing.T) {
	const (
		debianPg17Config   = "/usr/lib/postgresql/17/bin/pg_config"
		debianPg18Config   = "/usr/lib/postgresql/18/bin/pg_config"
		debianPg17Spock    = "/usr/share/postgresql/17/extension/spock.control"
		debianPg18Spock    = "/usr/share/postgresql/18/extension/spock.control"
		rhelPg17Config     = "/usr/pgsql-17/bin/pg_config"
		rhelPg18Config     = "/usr/pgsql-18/bin/pg_config"
		rhelPg17Spock      = "/usr/pgsql-17/share/extension/spock.control"
		rhelPg18Spock      = "/usr/pgsql-18/share/extension/spock.control"
		customPg17Config   = "/opt/postgresql/17/bin/pg_config"
		customPg17Spock    = "/opt/postgresql/17/share/extension/spock.control"
		customPg17ShareDir = "/opt/postgresql/17/share"
	)

	for _, tc := range []struct {
		name           string
		fsys           func() afero.Fs
		osFamily       config.OSFamily
		cfgBinDirs     map[string]string
		outputs        map[string]mockPgConfigOutput
		expectedOutput []*ds.PgEdgeVersion
		// expectedWarning is a non-fatal error that's returned along with the
		// expected output.
		expectedWarning string
		expectedErr     string
	}{
		{
			name:     "debian no postgres installed",
			fsys:     testFs(nil),
			osFamily: config.OSFamilyDebian,
		},
		{
			name: "debian one postgres version with spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "debian two postgres versions with spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "debian one postgres version without spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
				},
			},
		},
		{
			name: "debian available libraries",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
				"/usr/lib/postgresql/17/lib/auto_explain.so":       "",
				"/usr/lib/postgresql/17/lib/bitcode/plpgsql.bc":    "",
				"/usr/lib/postgresql/17/lib/pg_stat_statements.so": "",
				"/usr/lib/postgresql/17/lib/pgxml.so":              "",
				"/usr/lib/postgresql/17/lib/plpgsql.so":            "",
				"/usr/lib/postgresql/17/lib/spock.so":              "",
				debianPg18Config:                                   "",
				"/usr/lib/postgresql/18/lib/plpgsql.so":            "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion:    ds.MustParseVersion("18.4"),
					AvailableLibraries: []string{"plpgsql"},
				},
				{
					PostgresVersion:    ds.MustParseVersion("17.11"),
					SpockVersion:       ds.MustParseVersion("5"),
					AvailableLibraries: []string{"auto_explain", "pg_stat_statements", "pgxml", "plpgsql", "spock"},
				},
			},
		},
		{
			name: "rhel available libraries",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				"/usr/pgsql-17/lib/pg_stat_statements.so": "",
				"/usr/pgsql-17/lib/plpgsql.so":            "",
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion:    ds.MustParseVersion("17.11"),
					AvailableLibraries: []string{"pg_stat_statements", "plpgsql"},
				},
			},
		},
		{
			name: "debian mixed with and without spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
				},
			},
		},
		{
			name: "debian unsupported spock version",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock: withPgConfigValues(spock5Control, map[string]string{
					"default_version": "'4.0.10'",
				}),
				debianPg18Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
				},
			},
			expectedWarning: "spock 4.0.10 is installed for postgres 17, but it isn't supported, so postgres 17 will not be available",
		},
		{
			name: "debian spock 6",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock6Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("6"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel one postgres version with spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel two postgres versions with spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  spock5Control,
				rhelPg18Config: "",
				rhelPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
				rhelPg18Config: {stdout: testRHELPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel one postgres version without spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
				},
			},
		},
		{
			name: "overridden bin dirs",
			fsys: testFs(map[string]string{
				customPg17Config: "",
				customPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			cfgBinDirs: map[string]string{
				"17": "/opt/postgresql/17/bin",
			},
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				customPg17Config: {
					stdout: withPgConfigValues(testDebianPg17ConfigOut, map[string]string{
						"BINDIR":   "/opt/postgresql/17/bin",
						"SHAREDIR": customPg17ShareDir,
						"VERSION":  "PostgreSQL 17.6",
					}),
				},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.6"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "pg_config is a directory",
			fsys: func() afero.Fs {
				fsys := afero.NewMemMapFs()
				if err := fsys.MkdirAll(debianPg17Config, 0o755); err != nil {
					panic(err)
				}
				return fsys
			},
			osFamily:    config.OSFamilyDebian,
			expectedErr: "/usr/lib/postgresql/17/bin/pg_config is a directory",
		},
		{
			name: "pg_config execution fails",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stderr: "pg_config: could not find own program executable",
					err:    errors.New("exit status 2"),
				},
			},
			expectedErr: "failed to get installed postgres 17 version: failed to execute pg_config: exit status 2, stderr: pg_config: could not find own program executable",
		},
		{
			name: "pg_config output missing VERSION",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stdout: withoutPgConfigKey(testDebianPg17ConfigOut, "VERSION"),
				},
			},
			expectedErr: "VERSION not found in pg_config output",
		},
		{
			name: "pg_config output missing SHAREDIR",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {
					stdout: withoutPgConfigKey(testRHELPg17ConfigOut, "SHAREDIR"),
				},
			},
			expectedErr: "SHAREDIR not found in pg_config output",
		},
		{
			name: "pg_config output missing PKGLIBDIR",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stdout: withoutPgConfigKey(testDebianPg17ConfigOut, "PKGLIBDIR"),
				},
			},
			expectedErr: "PKGLIBDIR not found in pg_config output",
		},
		{
			name: "pg_config output unrecognized version",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stdout: withPgConfigValues(testDebianPg17ConfigOut, map[string]string{
						"VERSION": "NotPostgres 17.11",
					}),
				},
			},
			expectedErr: `unrecognized pg_config version "NotPostgres 17.11"`,
		},
		{
			name: "release candidate",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianRCPgConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.0"),
					SpockVersion:    ds.MustParseVersion("5"),
					Stability:       ds.StabilityRC,
				},
			},
		},
		{
			name: "spock pre-release",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock: withPgConfigValues(spock6Control, map[string]string{
					"default_version": "'6.1.0-beta.1'",
				}),
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("6"),
					Stability:       ds.StabilityBeta,
				},
			},
		},
		{
			name: "unrecognized pre-release falls back to dev",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {
					stdout: withPgConfigValues(testDebianPg18ConfigOut, map[string]string{
						"VERSION": "PostgreSQL 18devel",
					}),
				},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.0"),
					SpockVersion:    ds.MustParseVersion("5"),
					Stability:       ds.StabilityDev,
				},
			},
		},
		{
			name: "pg_config reports unexpected major version",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
			}),
			cfgBinDirs: map[string]string{
				"17": "/usr/lib/postgresql/18/bin",
			},
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedErr: "pg_config for postgres 17 reported unexpected version 18.4",
		},
		{
			name: "spock control file missing default_version",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  withoutPgConfigKey(spock5Control, "default_version"),
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedErr: "failed to get installed spock version for postgres 17: default_version not found in /usr/pgsql-17/share/extension/spock.control",
		},
		{
			name: "spock control file invalid default_version",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock: withPgConfigValues(spock5Control, map[string]string{
					"default_version": "'five'",
				}),
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedErr: `failed to get installed spock version for postgres 17: failed to parse default_version "five"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := systemd.NewInstalledVersionsProbe(
				mockPgConfigExecCommand(t, tc.outputs),
				tc.fsys(),
				config.SystemD{
					OSFamily:  tc.osFamily,
					PgBinDirs: tc.cfgBinDirs,
				},
			)
			require.NoError(t, err)

			actual, err := probe.InstalledPostgresVersions(t.Context())
			switch {
			case tc.expectedErr != "":
				assert.ErrorContains(t, err, tc.expectedErr)
			case tc.expectedWarning != "":
				assert.EqualError(t, err, tc.expectedWarning)
				assert.Equal(t, tc.expectedOutput, actual)
			default:
				require.NoError(t, err)
				assert.Equal(t, tc.expectedOutput, actual)
			}
		})
	}
}

func TestInstalledVersionsProbeInventoryErrors(t *testing.T) {
	fsys := testFs(map[string]string{
		"/usr/lib/postgresql/17/bin/pg_config":             "",
		"/usr/share/postgresql/17/extension/spock.control": spock5Control,
		// The library directory is a file, which can't be read as a directory.
		"/usr/lib/postgresql/17/lib":            "",
		"/usr/lib/postgresql/18/bin/pg_config":  "",
		"/usr/lib/postgresql/18/lib/plpgsql.so": "",
	})
	probe, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, map[string]mockPgConfigOutput{
			"/usr/lib/postgresql/17/bin/pg_config": {stdout: testDebianPg17ConfigOut},
			"/usr/lib/postgresql/18/bin/pg_config": {stdout: testDebianPg18ConfigOut},
		}),
		fsys(),
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)

	actual, err := probe.InstalledPostgresVersions(t.Context())
	assert.ErrorContains(t, err, "failed to get installed libraries for postgres 17: failed to read /usr/lib/postgresql/17/lib")
	// Both versions are still discovered, and the list that couldn't be read
	// is left nil. Spock isn't skipped, since its library might be installed.
	assert.Equal(t, []*ds.PgEdgeVersion{
		{
			PostgresVersion:    ds.MustParseVersion("18.4"),
			AvailableLibraries: []string{"plpgsql"},
		},
		{
			PostgresVersion: ds.MustParseVersion("17.11"),
			SpockVersion:    ds.MustParseVersion("5"),
		},
	}, actual)
}

func TestInstalledVersionsProbeCache(t *testing.T) {
	const (
		pg17Config = "/usr/lib/postgresql/17/bin/pg_config"
		pg17LibDir = "/usr/lib/postgresql/17/lib"
		pg18Config = "/usr/lib/postgresql/18/bin/pg_config"
	)

	fsys := testFs(map[string]string{
		pg17Config:                 "",
		pg17LibDir + "/plpgsql.so": "",
		pg18Config:                 "",
	})()
	probe, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, map[string]mockPgConfigOutput{
			pg17Config: {stdout: testDebianPg17ConfigOut},
			pg18Config: {stdout: testDebianPg18ConfigOut},
		}),
		fsys,
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)

	_, ok := probe.InstalledPostgresVersion("17")
	assert.False(t, ok, "nothing is cached before the first call")

	_, err = probe.InstalledPostgresVersions(t.Context())
	require.NoError(t, err)

	pg17 := &ds.PgEdgeVersion{
		PostgresVersion:    ds.MustParseVersion("17.11"),
		AvailableLibraries: []string{"plpgsql"},
	}
	actual, ok := probe.InstalledPostgresVersion("17")
	require.True(t, ok)
	assert.Equal(t, pg17, actual)
	_, ok = probe.InstalledPostgresVersion("18")
	assert.True(t, ok)

	// The getter returns a copy.
	actual.AvailableLibraries[0] = "modified"
	actual, _ = probe.InstalledPostgresVersion("17")
	assert.Equal(t, pg17, actual)

	// Make the Postgres 17 library directory unreadable and uninstall
	// Postgres 18.
	require.NoError(t, fsys.RemoveAll(pg17LibDir))
	require.NoError(t, afero.WriteFile(fsys, pg17LibDir, nil, 0o644))
	require.NoError(t, fsys.Remove(pg18Config))

	versions, err := probe.InstalledPostgresVersions(t.Context())
	assert.ErrorContains(t, err, "failed to get installed libraries for postgres 17")
	// The previous libraries are kept for Postgres 17, and Postgres 18 is no
	// longer cached.
	assert.Equal(t, []*ds.PgEdgeVersion{pg17}, versions)
	actual, ok = probe.InstalledPostgresVersion("17")
	require.True(t, ok)
	assert.Equal(t, pg17, actual)
	_, ok = probe.InstalledPostgresVersion("18")
	assert.False(t, ok)

	// The failure was already reported by the previous call.
	versions, err = probe.InstalledPostgresVersions(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, []*ds.PgEdgeVersion{pg17}, versions)
}

func TestInstalledVersionsProbeSpockWithoutLibrary(t *testing.T) {
	const (
		pg17Config  = "/usr/lib/postgresql/17/bin/pg_config"
		pg17Spock   = "/usr/share/postgresql/17/extension/spock.control"
		pg17Library = "/usr/lib/postgresql/17/lib/spock.so"
		pg18Config  = "/usr/lib/postgresql/18/bin/pg_config"
		pg18Spock   = "/usr/share/postgresql/18/extension/spock.control"
		warning     = "spock 5.0.9 is installed for postgres 17 without its library in /usr/lib/postgresql/17/lib, so postgres 17 will be available without spock"
	)

	fsys := testFs(map[string]string{
		pg17Config:                              "",
		pg17Spock:                               spock5Control,
		"/usr/lib/postgresql/17/lib/plpgsql.so": "",
		pg18Config:                              "",
		pg18Spock:                               spock5Control,
		"/usr/lib/postgresql/18/lib/spock.so":   "",
	})()
	probe, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, map[string]mockPgConfigOutput{
			pg17Config: {stdout: testDebianPg17ConfigOut},
			pg18Config: {stdout: testDebianPg18ConfigOut},
		}),
		fsys,
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)

	pg18 := &ds.PgEdgeVersion{
		PostgresVersion:    ds.MustParseVersion("18.4"),
		SpockVersion:       ds.MustParseVersion("5"),
		AvailableLibraries: []string{"spock"},
	}
	withoutSpock := []*ds.PgEdgeVersion{
		pg18,
		{
			PostgresVersion:    ds.MustParseVersion("17.11"),
			AvailableLibraries: []string{"plpgsql"},
		},
	}

	// Postgres 17 is reported without spock, since Postgres would fail to
	// load it.
	actual, err := probe.InstalledPostgresVersions(t.Context())
	assert.EqualError(t, err, warning)
	assert.Equal(t, withoutSpock, actual)

	// The warning is only returned the first time.
	actual, err = probe.InstalledPostgresVersions(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, withoutSpock, actual)

	// Installing the library adds spock.
	require.NoError(t, afero.WriteFile(fsys, pg17Library, nil, 0o644))
	actual, err = probe.InstalledPostgresVersions(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, []*ds.PgEdgeVersion{
		pg18,
		{
			PostgresVersion:    ds.MustParseVersion("17.11"),
			SpockVersion:       ds.MustParseVersion("5"),
			AvailableLibraries: []string{"plpgsql", "spock"},
		},
	}, actual)

	// Removing it again repeats the warning, since the previous call didn't
	// report it.
	require.NoError(t, fsys.Remove(pg17Library))
	actual, err = probe.InstalledPostgresVersions(t.Context())
	assert.EqualError(t, err, warning)
	assert.Equal(t, withoutSpock, actual)
}

func TestInstalledServices(t *testing.T) {
	const postgrestPath = "/usr/bin/postgrest"

	for _, tc := range []struct {
		name           string
		fsys           func() afero.Fs
		postgrestPath  string
		outputs        map[string]mockPgConfigOutput
		expectedOutput map[string]string
		expectedErr    string
	}{
		{
			name:          "postgrest installed",
			fsys:          testFs(map[string]string{postgrestPath: ""}),
			postgrestPath: postgrestPath,
			outputs: map[string]mockPgConfigOutput{
				postgrestPath: {stdout: "PostgREST 14.5\n"},
			},
			expectedOutput: map[string]string{"postgrest": "14.5"},
		},
		{
			name:          "ignores build information",
			fsys:          testFs(map[string]string{postgrestPath: ""}),
			postgrestPath: postgrestPath,
			outputs: map[string]mockPgConfigOutput{
				postgrestPath: {stdout: "PostgREST 14.10 (a4e00ff)\n"},
			},
			expectedOutput: map[string]string{"postgrest": "14.10"},
		},
		{
			name:           "postgrest not installed",
			fsys:           testFs(nil),
			postgrestPath:  postgrestPath,
			expectedOutput: map[string]string{},
		},
		{
			name:           "postgrest path not configured",
			fsys:           testFs(map[string]string{postgrestPath: ""}),
			expectedOutput: map[string]string{},
		},
		{
			name:          "unparseable version",
			fsys:          testFs(map[string]string{postgrestPath: ""}),
			postgrestPath: postgrestPath,
			outputs: map[string]mockPgConfigOutput{
				postgrestPath: {stdout: "PostgREST dev\n"},
			},
			expectedErr: `failed to get postgrest version: no version found in output "PostgREST dev"`,
		},
		{
			name:          "command fails",
			fsys:          testFs(map[string]string{postgrestPath: ""}),
			postgrestPath: postgrestPath,
			outputs: map[string]mockPgConfigOutput{
				postgrestPath: {stderr: "boom", err: errors.New("exit status 1")},
			},
			expectedErr: "failed to get postgrest version: exit status 1, stderr: boom",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := systemd.NewInstalledVersionsProbe(
				mockPgConfigExecCommand(t, tc.outputs),
				tc.fsys(),
				config.SystemD{
					OSFamily:      config.OSFamilyDebian,
					PostgRESTPath: tc.postgrestPath,
				},
			)
			require.NoError(t, err)

			actual, err := probe.InstalledServices(t.Context())
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedOutput, actual)
			}
		})
	}
}

func TestInstalledServicesRAG(t *testing.T) {
	const (
		ragPath       = "/usr/pgedge/rag-server2/bin/pgedge-rag-server"
		postgrestPath = "/usr/bin/postgrest"
	)
	ragVersionOutput := "pgEdge RAG Server\n" +
		"  Version:    2.0.1\n" +
		"  Build Time: 2026-11-10T12:30:01Z\n" +
		"  Git Commit: e0fbdf2c5a93a5592b577a972594b5f729fa671e\n"

	for _, tc := range []struct {
		name           string
		fsys           func() afero.Fs
		outputs        map[string]mockPgConfigOutput
		expectedOutput map[string]string
		expectedErr    string
	}{
		{
			name:           "rag installed",
			fsys:           testFs(map[string]string{ragPath: ""}),
			outputs:        map[string]mockPgConfigOutput{ragPath: {stdout: ragVersionOutput}},
			expectedOutput: map[string]string{"rag": "2.0.1"},
		},
		{
			name:           "rag not installed",
			fsys:           testFs(nil),
			expectedOutput: map[string]string{},
		},
		{
			name: "rag failure doesn't hide other services",
			fsys: testFs(map[string]string{ragPath: "", postgrestPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				ragPath:       {stderr: "boom", err: errors.New("exit status 1")},
				postgrestPath: {stdout: "PostgREST 14.18\n"},
			},
			expectedOutput: map[string]string{"postgrest": "14.18"},
			expectedErr:    "failed to get rag version: exit status 1, stderr: boom",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := systemd.NewInstalledVersionsProbe(
				mockPgConfigExecCommand(t, tc.outputs),
				tc.fsys(),
				config.SystemD{
					OSFamily:      config.OSFamilyDebian,
					PostgRESTPath: postgrestPath,
					RAGPath:       ragPath,
				},
			)
			require.NoError(t, err)

			actual, err := probe.InstalledServices(t.Context())
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedOutput, actual)
		})
	}
}

func TestNewInstalledVersionsProbeUnknownOSFamily(t *testing.T) {
	_, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, nil),
		afero.NewMemMapFs(),
		config.SystemD{OSFamily: config.OSFamilyUnknown},
	)
	assert.ErrorContains(t, err, "unrecognized os family 'unknown'")
}

func testFs(files map[string]string) func() afero.Fs {
	return func() afero.Fs {
		fsys := afero.NewMemMapFs()
		for path, contents := range files {
			if err := afero.WriteFile(fsys, path, []byte(contents), 0o755); err != nil {
				panic(err)
			}
		}
		return fsys
	}
}

// withPgConfigValues replaces the values of the given keys in "KEY = value"
// formatted output, such as pg_config output or extension control files.
func withPgConfigValues(output string, values map[string]string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if value, ok := values[key]; ok {
			lines[i] = key + " = " + value
		}
	}
	return strings.Join(lines, "\n")
}

// withoutPgConfigKey removes the given key from "KEY = value" formatted
// output, such as pg_config output or extension control files.
func withoutPgConfigKey(output, key string) string {
	lines := strings.Split(output, "\n")
	lines = slices.DeleteFunc(lines, func(line string) bool {
		k, _, ok := strings.Cut(line, "=")
		return ok && strings.TrimSpace(k) == key
	})
	return strings.Join(lines, "\n")
}

type mockPgConfigOutput struct {
	stdout string
	stderr string
	err    error
}

func mockPgConfigExecCommand(t testing.TB, outputs map[string]mockPgConfigOutput) systemd.ExecCommand {
	t.Helper()

	return func(_ context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		output, ok := outputs[name]
		require.True(t, ok)

		if output.stdout != "" {
			_, err := stdout.Write([]byte(output.stdout))
			require.NoError(t, err)
		}
		if output.stderr != "" {
			_, err := stderr.Write([]byte(output.stderr))
			require.NoError(t, err)
		}
		return output.err
	}
}

const testDebianPg17ConfigOut = `BINDIR = /usr/lib/postgresql/17/bin
DOCDIR = /usr/share/doc/postgresql-doc-17
HTMLDIR = /usr/share/doc/postgresql-doc-17
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/17/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/17/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/17/man
SHAREDIR = /usr/share/postgresql/17
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/17/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/17/man' '--docdir=/usr/share/doc/postgresql-doc-17' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/17' '--bindir=/usr/lib/postgresql/17/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 17.11-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm 
VERSION = PostgreSQL 17.11 (Ubuntu 17.11-1.noble)`

const testDebianPg18ConfigOut = `BINDIR = /usr/lib/postgresql/18/bin
DOCDIR = /usr/share/doc/postgresql-doc-18
HTMLDIR = /usr/share/doc/postgresql-doc-18
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/18/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/18/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/18/man
SHAREDIR = /usr/share/postgresql/18
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/18/man' '--docdir=/usr/share/doc/postgresql-doc-18' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/18' '--bindir=/usr/lib/postgresql/18/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 18.4-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-libcurl' '--with-libnuma' 'LIBNUMA_CFLAGS= ' 'LIBNUMA_LIBS=-lnuma' '--with-liburing' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18.4 (Ubuntu 18.4-1.noble)`

const testRHELPg17ConfigOut = `BINDIR = /usr/pgsql-17/bin
DOCDIR = /usr/pgsql-17/doc
HTMLDIR = /usr/pgsql-17/doc/html
INCLUDEDIR = /usr/pgsql-17/include
PKGINCLUDEDIR = /usr/pgsql-17/include
INCLUDEDIR-SERVER = /usr/pgsql-17/include/server
LIBDIR = /usr/pgsql-17/lib
PKGLIBDIR = /usr/pgsql-17/lib
LOCALEDIR = /usr/pgsql-17/share/locale
MANDIR = /usr/pgsql-17/share/man
SHAREDIR = /usr/pgsql-17/share
SYSCONFDIR = /etc/sysconfig/pgsql
PGXS = /usr/pgsql-17/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--enable-rpath' '--prefix=/usr/pgsql-17' '--includedir=/usr/pgsql-17/include' '--mandir=/usr/pgsql-17/share/man' '--datadir=/usr/pgsql-17/share' '--libdir=/usr/pgsql-17/lib' '--with-lz4' '--with-zstd' '--enable-tap-tests' '--with-icu' '--with-llvm' '--with-perl' '--with-python' '--with-tcl' '--with-tclconfig=/usr/lib64' '--with-openssl' '--with-pam' '--with-gssapi' '--with-includes=/usr/include' '--with-libraries=/usr/lib64' '--enable-nls' '--enable-dtrace' '--with-uuid=e2fs' '--with-libxml' '--with-libxslt' '--with-ldap' '--with-selinux' '--with-systemd' '--with-system-tzdata=/usr/share/zoneinfo' '--sysconfdir=/etc/sysconfig/pgsql' '--docdir=/usr/pgsql-17/doc' '--htmldir=/usr/pgsql-17/doc/html' 'CFLAGS=-O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection' 'LDFLAGS=-Wl,--as-needed' 'LLVM_CONFIG=/usr/bin/llvm-config' 'CLANG=/usr/bin/clang' 'PKG_CONFIG_PATH=:/usr/lib64/pkgconfig:/usr/share/pkgconfig'
CC = gcc
CPPFLAGS = -D_GNU_SOURCE -I/usr/include -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,--as-needed -L/usr/lib64 -Wl,--as-needed -Wl,-rpath,'/usr/pgsql-17/lib',--enable-new-dtags
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm 
VERSION = PostgreSQL 17.11`

const testRHELPg18ConfigOut = `BINDIR = /usr/pgsql-18/bin
DOCDIR = /usr/pgsql-18/doc
HTMLDIR = /usr/pgsql-18/doc/html
INCLUDEDIR = /usr/pgsql-18/include
PKGINCLUDEDIR = /usr/pgsql-18/include
INCLUDEDIR-SERVER = /usr/pgsql-18/include/server
LIBDIR = /usr/pgsql-18/lib
PKGLIBDIR = /usr/pgsql-18/lib
LOCALEDIR = /usr/pgsql-18/share/locale
MANDIR = /usr/pgsql-18/share/man
SHAREDIR = /usr/pgsql-18/share
SYSCONFDIR = /etc/sysconfig/pgsql
PGXS = /usr/pgsql-18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--enable-rpath' '--prefix=/usr/pgsql-18' '--includedir=/usr/pgsql-18/include' '--mandir=/usr/pgsql-18/share/man' '--datadir=/usr/pgsql-18/share' '--libdir=/usr/pgsql-18/lib' '--with-lz4' '--with-zstd' '--enable-tap-tests' '--with-icu' '--with-gssapi' '--with-includes=/usr/include' '--with-libraries=/usr/lib64' '--with-libnuma' '--with-liburing' '--with-ldap' '--with-llvm' '--enable-nls' '--with-pam' '--with-perl' '--with-python' '--with-tcl' '--with-tclconfig=/usr/lib64' '--enable-dtrace' '--with-selinux' '--with-openssl' '--with-uuid=e2fs' '--with-libxml' '--with-libxslt' '--with-libcurl' '--with-systemd' '--with-system-tzdata=/usr/share/zoneinfo' '--sysconfdir=/etc/sysconfig/pgsql' '--docdir=/usr/pgsql-18/doc' '--htmldir=/usr/pgsql-18/doc/html' 'CFLAGS=-O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection' 'LDFLAGS=-Wl,--as-needed' 'LLVM_CONFIG=/usr/bin/llvm-config' 'CLANG=/usr/bin/clang' 'PKG_CONFIG_PATH=:/usr/lib64/pkgconfig:/usr/share/pkgconfig'
CC = gcc
CPPFLAGS = -D_GNU_SOURCE -I/usr/include -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,--as-needed -L/usr/lib64 -Wl,--as-needed -Wl,-rpath,'/usr/pgsql-18/lib',--enable-new-dtags
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18.4`

const testDebianRCPgConfigOut = `BINDIR = /usr/lib/postgresql/18/bin
DOCDIR = /usr/share/doc/postgresql-doc-18
HTMLDIR = /usr/share/doc/postgresql-doc-18
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/18/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/18/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/18/man
SHAREDIR = /usr/share/postgresql/18
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/18/man' '--docdir=/usr/share/doc/postgresql-doc-18' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/18' '--bindir=/usr/lib/postgresql/18/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 18.4-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-libcurl' '--with-libnuma' 'LIBNUMA_CFLAGS= ' 'LIBNUMA_LIBS=-lnuma' '--with-liburing' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18rc1 (Ubuntu 18~rc1-1.noble)`

const spock5Control = `# spock extension
comment = 'PostgreSQL Logical Replication'
default_version = '5.0.9'
module_pathname = '$libdir/spock'
relocatable = false

schema = spock`

const spock6Control = `# spock extension
comment = 'PostgreSQL Logical Replication'
default_version = '6.0.0'
module_pathname = '$libdir/spock'
relocatable = false

schema = spock`

func TestInstalledServicesMCP(t *testing.T) {
	const mcpPath = "/usr/bin/pgedge-postgres-mcp"

	for _, tc := range []struct {
		name           string
		osFamily       config.OSFamily
		fsys           func() afero.Fs
		outputs        map[string]mockPgConfigOutput
		expectedOutput map[string]string
		expectedErr    string
	}{
		{
			name:     "rpm",
			osFamily: config.OSFamilyRedHat,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"rpm": {stdout: "1.1.0"},
			},
			expectedOutput: map[string]string{"mcp": "1.1.0"},
		},
		{
			name:     "deb strips the revision",
			osFamily: config.OSFamilyDebian,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"dpkg-query": {stdout: "1.1.0-1.trixie"},
			},
			expectedOutput: map[string]string{"mcp": "1.1.0"},
		},
		{
			name:     "deb strips the epoch",
			osFamily: config.OSFamilyDebian,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"dpkg-query": {stdout: "2:1.1.0-1.noble"},
			},
			expectedOutput: map[string]string{"mcp": "1.1.0"},
		},
		{
			name:     "rpm pre-release",
			osFamily: config.OSFamilyRedHat,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"rpm": {stdout: "1.2.0~beta1"},
			},
			expectedOutput: map[string]string{"mcp": "1.2.0-beta1"},
		},
		{
			name:     "deb upstream version with a hyphen",
			osFamily: config.OSFamilyDebian,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"dpkg-query": {stdout: "1.2.0-beta1-1.trixie"},
			},
			expectedOutput: map[string]string{"mcp": "1.2.0-beta1"},
		},
		{
			name:     "deb native version without a revision",
			osFamily: config.OSFamilyDebian,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"dpkg-query": {stdout: "1.1.0"},
			},
			expectedOutput: map[string]string{"mcp": "1.1.0"},
		},
		{
			name:     "deb pre-release",
			osFamily: config.OSFamilyDebian,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"dpkg-query": {stdout: "1.2.0~beta1-1.trixie"},
			},
			expectedOutput: map[string]string{"mcp": "1.2.0-beta1"},
		},
		{
			name:           "binary not installed",
			osFamily:       config.OSFamilyRedHat,
			fsys:           testFs(nil),
			expectedOutput: map[string]string{},
		},
		{
			name:     "package query fails",
			osFamily: config.OSFamilyRedHat,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"rpm": {stderr: "package pgedge-postgres-mcp is not installed", err: errors.New("exit status 1")},
			},
			expectedErr: "failed to get mcp version: failed to query package pgedge-postgres-mcp: exit status 1, stderr: package pgedge-postgres-mcp is not installed",
		},
		{
			name:     "unparseable version",
			osFamily: config.OSFamilyRedHat,
			fsys:     testFs(map[string]string{mcpPath: ""}),
			outputs: map[string]mockPgConfigOutput{
				"rpm": {stdout: "dev"},
			},
			expectedErr: `failed to get mcp version: no version found in package version "dev"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := systemd.NewInstalledVersionsProbe(
				mockPgConfigExecCommand(t, tc.outputs),
				tc.fsys(),
				config.SystemD{
					OSFamily: tc.osFamily,
					MCPPath:  mcpPath,
				},
			)
			require.NoError(t, err)

			actual, err := probe.InstalledServices(t.Context())
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedOutput, actual)
			}
		})
	}
}

func TestInstalledServicesPartialFailure(t *testing.T) {
	const (
		postgrestPath = "/usr/bin/postgrest"
		mcpPath       = "/usr/bin/pgedge-postgres-mcp"
	)

	probe, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, map[string]mockPgConfigOutput{
			postgrestPath: {stdout: "PostgREST 14.18\n"},
			"dpkg-query":  {stdout: "dev"},
		}),
		testFs(map[string]string{postgrestPath: "", mcpPath: ""})(),
		config.SystemD{
			OSFamily:      config.OSFamilyDebian,
			PostgRESTPath: postgrestPath,
			MCPPath:       mcpPath,
		},
	)
	require.NoError(t, err)

	actual, err := probe.InstalledServices(t.Context())
	assert.EqualError(t, err, `failed to get mcp version: no version found in package version "dev"`)
	assert.Equal(t, map[string]string{"postgrest": "14.18"}, actual)
}

func TestInstalledServicesStalledProbe(t *testing.T) {
	const (
		postgrestPath = "/usr/bin/postgrest"
		mcpPath       = "/usr/bin/pgedge-postgres-mcp"
	)

	probe, err := systemd.NewInstalledVersionsProbe(
		func(ctx context.Context, stdout, _ io.Writer, name string, _ ...string) error {
			if name == postgrestPath {
				if err := ctx.Err(); err != nil {
					return err
				}
				_, err := stdout.Write([]byte("PostgREST 14.18\n"))
				return err
			}
			// The MCP package query, which runs first in sorted order, stalls
			// until the deadline.
			<-ctx.Done()
			return ctx.Err()
		},
		testFs(map[string]string{postgrestPath: "", mcpPath: ""})(),
		config.SystemD{
			OSFamily:      config.OSFamilyRedHat,
			PostgRESTPath: postgrestPath,
			MCPPath:       mcpPath,
		},
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	actual, err := probe.InstalledServices(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, map[string]string{"postgrest": "14.18"}, actual)
}

func TestInstalledVersionsProbeUnsupportedSpockWarning(t *testing.T) {
	const pg17Config = "/usr/lib/postgresql/17/bin/pg_config"

	fsys := testFs(map[string]string{
		pg17Config: "",
		"/usr/share/postgresql/17/extension/spock.control": withPgConfigValues(spock5Control, map[string]string{
			"default_version": "'4.0.10'",
		}),
	})()
	probe, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, map[string]mockPgConfigOutput{
			pg17Config: {stdout: testDebianPg17ConfigOut},
		}),
		fsys,
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)

	actual, err := probe.InstalledPostgresVersions(t.Context())
	assert.EqualError(t, err, "spock 4.0.10 is installed for postgres 17, but it isn't supported, so postgres 17 will not be available")
	assert.Empty(t, actual)

	// The warning is only returned the first time, even though the skipped
	// version isn't in the previous results.
	actual, err = probe.InstalledPostgresVersions(t.Context())
	assert.NoError(t, err)
	assert.Empty(t, actual)
}
