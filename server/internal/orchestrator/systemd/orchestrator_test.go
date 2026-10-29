package systemd

import (
	"os/user"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
