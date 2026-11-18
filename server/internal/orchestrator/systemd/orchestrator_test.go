package systemd

import (
	"context"
	"io"
	"os/user"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultVersion(t *testing.T) {
	pg18RC := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("18.0"),
		SpockVersion:    ds.MustParseVersion("5"),
		Stability:       ds.StabilityRC,
	}
	pg17 := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("17.11"),
		SpockVersion:    ds.MustParseVersion("5"),
	}
	pg16 := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("16.15"),
		SpockVersion:    ds.MustParseVersion("5"),
	}
	pg16Beta := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("16.15"),
		SpockVersion:    ds.MustParseVersion("6"),
		Stability:       ds.StabilityBeta,
	}

	for _, tc := range []struct {
		name     string
		versions []*ds.PgEdgeVersion
		expected *ds.PgEdgeVersion
	}{
		{
			name:     "single stable version",
			versions: []*ds.PgEdgeVersion{pg17},
			expected: pg17,
		},
		{
			name:     "multiple stable versions",
			versions: []*ds.PgEdgeVersion{pg17, pg16},
			expected: pg17,
		},
		{
			name:     "skips pre-releases",
			versions: []*ds.PgEdgeVersion{pg18RC, pg17, pg16},
			expected: pg17,
		},
		{
			name:     "only pre-releases",
			versions: []*ds.PgEdgeVersion{pg18RC, pg16Beta},
			expected: pg18RC,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Same(t, tc.expected, defaultVersion(tc.versions))
		})
	}
}

func TestGenerateInstanceResourcesPatroniUnit(t *testing.T) {
	for _, tc := range []struct {
		name            string
		osFamily        config.OSFamily
		pgBinDirs       map[string]string
		instanceDataDir string
		expectedPath    string
		expectedBaseDir string
	}{
		{
			name:            "debian default",
			osFamily:        config.OSFamilyDebian,
			expectedPath:    "/usr/lib/postgresql/17/bin",
			expectedBaseDir: "/var/lib/postgresql/17/storefront-n1-689qacsi",
		},
		{
			name:            "redhat default",
			osFamily:        config.OSFamilyRedHat,
			expectedPath:    "/usr/pgsql-17/bin",
			expectedBaseDir: "/var/lib/pgsql/17/storefront-n1-689qacsi",
		},
		{
			name:     "configured",
			osFamily: config.OSFamilyDebian,
			pgBinDirs: map[string]string{
				"17": "/opt/postgresql/17/bin",
			},
			instanceDataDir: "/data",
			expectedPath:    "/opt/postgresql/17/bin",
			expectedBaseDir: "/data/17/storefront-n1-689qacsi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// PatroniUnitOptions appends the host's PATH to the unit's PATH.
			t.Setenv("PATH", "")

			o := &Orchestrator{
				versionsProbe: &InstalledVersionsProbe{},
				userLookup: func(username string) (*user.User, error) {
					return &user.User{
						Uid:      "26",
						Gid:      "26",
						Username: username,
					}, nil
				},
				cfg: config.Config{
					DatabaseOwnerUID: 26,
					DatabaseOwnerGID: 26,
					SystemD: config.SystemD{
						OSFamily:        tc.osFamily,
						InstanceDataDir: tc.instanceDataDir,
						PgBinDirs:       tc.pgBinDirs,
					},
				},
			}
			spec := &database.InstanceSpec{
				InstanceID: "storefront-n1-689qacsi",
				DatabaseID: "storefront",
				HostID:     "host-1",
				NodeName:   "n1",
				PgEdgeVersion: &ds.PgEdgeVersion{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				Port:        utils.PointerTo(5432),
				PatroniPort: utils.PointerTo(8888),
			}

			resources, err := o.GenerateInstanceResources(spec, nil)
			require.NoError(t, err)

			unitID := UnitResourceIdentifier(
				patroniServiceName(spec.InstanceID),
				spec.DatabaseID,
				spec.HostID,
			)
			var unitData *resource.ResourceData
			for _, data := range resources.InstanceDependencies {
				if data.Identifier == unitID {
					unitData = data
				}
			}
			require.NotNil(t, unitData)

			unit, err := resource.ToResource[*UnitResource](unitData)
			require.NoError(t, err)

			var env []string
			for _, opt := range unit.Options {
				if opt.Section == "Service" && opt.Name == "Environment" {
					env = append(env, opt.Value)
				}
			}
			assert.Contains(t, env, `"PATH=`+tc.expectedPath+`"`)
			assert.Contains(t, env, `"PGSERVICEFILE=`+tc.expectedBaseDir+`/configs/pg_service.conf"`)
		})
	}
}

func TestPopulateHostInventoryError(t *testing.T) {
	const pgConfig = "/usr/lib/postgresql/17/bin/pg_config"

	fsys := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fsys, pgConfig, nil, 0o755))
	// The library directory is a file, which can't be read as a directory.
	require.NoError(t, afero.WriteFile(fsys, "/usr/lib/postgresql/17/lib", nil, 0o644))
	probe, err := NewInstalledVersionsProbe(
		func(_ context.Context, stdout, _ io.Writer, name string, _ ...string) error {
			require.Equal(t, pgConfig, name)
			_, err := stdout.Write([]byte("SHAREDIR = /usr/share/postgresql/17\n" +
				"PKGLIBDIR = /usr/lib/postgresql/17/lib\n" +
				"VERSION = PostgreSQL 17.11\n"))
			return err
		},
		fsys,
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)

	o := &Orchestrator{logger: zerolog.Nop(), versionsProbe: probe}
	h := &host.Host{}
	require.NoError(t, o.PopulateHost(t.Context(), h))

	expected := []*ds.PgEdgeVersion{{PostgresVersion: ds.MustParseVersion("17.11")}}
	assert.Equal(t, expected, h.SupportedPgEdgeVersions)
	assert.Equal(t, expected[0], h.DefaultPgEdgeVersion)
}

func TestDefaultGUCs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		version   *ds.PgEdgeVersion
		libraries []string
		expected  map[string]any
	}{
		{
			name:      "with spock",
			version:   ds.MustParsePgEdgeVersion("17.10", "5"),
			libraries: []string{"pg_stat_statements", "spock"},
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,spock",
			},
		},
		{
			name:      "with spock and output plugin libraries",
			version:   ds.MustParsePgEdgeVersion("17.11", "5"),
			libraries: []string{"pg_stat_statements", "spock"},
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,spock",
				"output_plugin_libraries":  "pgoutput, test_decoding, spock_output",
			},
		},
		{
			name:      "with spock without pg_stat_statements",
			version:   ds.MustParsePgEdgeVersion("17.10", "5"),
			libraries: []string{"spock"},
			expected: map[string]any{
				"shared_preload_libraries": "spock",
			},
		},
		{
			name:      "without spock",
			version:   ds.MustParsePgEdgeVersion("17.11", ""),
			libraries: []string{"pg_stat_statements"},
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements",
			},
		},
		{
			name:      "without spock or pg_stat_statements",
			version:   ds.MustParsePgEdgeVersion("17.11", ""),
			libraries: []string{"plpgsql"},
			expected:  map[string]any{},
		},
		{
			name:     "unknown libraries",
			version:  ds.MustParsePgEdgeVersion("17.11", ""),
			expected: map[string]any{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, defaultGUCs(tc.version, tc.libraries))
		})
	}
}

func TestInstalledLibraries(t *testing.T) {
	const pgConfig = "/usr/lib/postgresql/17/bin/pg_config"

	fsys := afero.NewMemMapFs()
	for _, path := range []string{pgConfig, "/usr/lib/postgresql/17/lib/pg_stat_statements.so"} {
		require.NoError(t, afero.WriteFile(fsys, path, nil, 0o755))
	}
	probe, err := NewInstalledVersionsProbe(
		func(_ context.Context, stdout, _ io.Writer, name string, _ ...string) error {
			require.Equal(t, pgConfig, name)
			_, err := stdout.Write([]byte("SHAREDIR = /usr/share/postgresql/17\n" +
				"PKGLIBDIR = /usr/lib/postgresql/17/lib\n" +
				"VERSION = PostgreSQL 17.11\n"))
			return err
		},
		fsys,
		config.SystemD{OSFamily: config.OSFamilyDebian},
	)
	require.NoError(t, err)
	o := &Orchestrator{versionsProbe: probe}

	// The probe hasn't run yet.
	assert.Nil(t, o.installedLibraries(ds.MustParsePgEdgeVersion("17.11", "")))

	_, err = probe.InstalledPostgresVersions(t.Context())
	require.NoError(t, err)

	// The instance spec's minor version can differ from the installed one.
	assert.Equal(t, []string{"pg_stat_statements"}, o.installedLibraries(ds.MustParsePgEdgeVersion("17.6", "")))
	assert.Nil(t, o.installedLibraries(ds.MustParsePgEdgeVersion("18.4", "")))
}
