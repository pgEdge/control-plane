package database

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestVersionsFromImage(t *testing.T) {
	cases := []struct {
		name      string
		image     string
		wantPg    string
		wantSpock string
		wantOk    bool
	}{
		{"empty image", "", "", "", false},
		{"pgEdge tag", "127.0.0.1:5001/pgedge/pgedge-postgres:16.14-spock5.0.10-standard-1", "16.14", "5", true},
		{"pgEdge tag with digest", "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2@sha256:abc123", "17.9", "5", true},
		{"unrecognized dev tag", "127.0.0.1:5001/pgedge/pgedge-postgres:my-dev-build", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg, spock, ok := versionsFromImage(tc.image)
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				assert.Equal(t, tc.wantPg, pg)
				assert.Equal(t, tc.wantSpock, spock)
			}
		})
	}
}

func TestMergePostgreSQLConf(t *testing.T) {
	cases := []struct {
		name     string
		base     map[string]any
		override map[string]any
		want     map[string]any
	}{
		{
			name:     "nil base",
			override: map[string]any{"max_connections": 100},
			want:     map[string]any{"max_connections": 100},
		},
		{
			name: "nil override",
			base: map[string]any{"max_connections": 100},
			want: map[string]any{"max_connections": 100},
		},
		{
			name:     "same spelling",
			base:     map[string]any{"max_connections": 100, "work_mem": "4MB"},
			override: map[string]any{"max_connections": 200},
			want:     map[string]any{"max_connections": 200, "work_mem": "4MB"},
		},
		{
			name:     "different spelling",
			base:     map[string]any{"shared_preload_libraries": "pg_stat_statements,spock"},
			override: map[string]any{" SHARED_PRELOAD_LIBRARIES ": "spock"},
			want:     map[string]any{" SHARED_PRELOAD_LIBRARIES ": "spock"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := maps.Clone(tc.base)
			assert.Equal(t, tc.want, mergePostgreSQLConf(tc.base, tc.override))
			assert.Equal(t, base, tc.base, "base must not be modified")
		})
	}
}

func TestOrchestratorOptsSwarmImage(t *testing.T) {
	assert.Equal(t, "", (*OrchestratorOpts)(nil).swarmImage())
	assert.Equal(t, "", (&OrchestratorOpts{}).swarmImage())
	assert.Equal(t, "my-image", (&OrchestratorOpts{Swarm: &SwarmOpts{Image: "my-image"}}).swarmImage())
}

func TestValidateLocaleVersions(t *testing.T) {
	builtin := &Locale{Provider: "builtin", Locale: "C.UTF-8"}

	for _, tc := range []struct {
		name        string
		spec        *Spec
		expectedErr string
	}{
		{
			name: "no locale on postgres 16",
			spec: &Spec{PostgresVersion: "16.10", Nodes: []*Node{{Name: "n1"}}},
		},
		{
			name: "icu on postgres 16",
			spec: &Spec{
				PostgresVersion: "16.10",
				Locale:          &Locale{Provider: "icu", ICULocale: "de-DE"},
				Nodes:           []*Node{{Name: "n1"}},
			},
		},
		{
			name: "builtin on postgres 17",
			spec: &Spec{PostgresVersion: "17.6", Locale: builtin, Nodes: []*Node{{Name: "n1"}}},
		},
		{
			name:        "builtin on postgres 16",
			spec:        &Spec{PostgresVersion: "16.10", Locale: builtin, Nodes: []*Node{{Name: "n1"}}},
			expectedErr: "the 'builtin' locale provider requires Postgres 17 or later, got 16.10",
		},
		{
			// e.g. a node version that was defaulted from a node-level image
			// override by PopulateSpecDefaults.
			name: "builtin with a postgres 16 node",
			spec: &Spec{
				PostgresVersion: "18.1",
				Locale:          builtin,
				Nodes: []*Node{
					{Name: "n1"},
					{Name: "n2", PostgresVersion: "16.10"},
				},
			},
			expectedErr: "the 'builtin' locale provider requires Postgres 17 or later, got 16.10",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLocaleVersions(tc.spec)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateServiceHosts(t *testing.T) {
	hostsByID := map[string]*host.Host{
		"host-1": {
			ID:                     "host-1",
			SupportedServices:      map[string][]string{"postgrest": {"14.5"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.5"},
		},
		"host-2": {
			ID:                     "host-2",
			SupportedServices:      map[string][]string{"postgrest": {"14.10"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.10"},
		},
		"host-3": {
			ID:                     "host-3",
			SupportedServices:      map[string][]string{},
			DefaultServiceVersions: map[string]string{},
		},
		// A host record from a Control Plane version that didn't report its
		// supported services.
		"host-4": {ID: "host-4"},
		"host-systemd": {
			ID:                     "host-systemd",
			Orchestrator:           config.OrchestratorSystemD,
			SupportedServices:      map[string][]string{"postgrest": {"14.5"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.5"},
		},
	}
	postgrest := func(version string, hostIDs ...string) *ServiceSpec {
		return &ServiceSpec{
			ServiceID:   "api",
			ServiceType: "postgrest",
			Version:     version,
			HostIDs:     hostIDs,
		}
	}

	for _, tc := range []struct {
		name        string
		service     *ServiceSpec
		expectedErr string
	}{
		{
			name:    "supported version",
			service: postgrest("14.5", "host-1"),
		},
		{
			name:    "latest",
			service: postgrest("latest", "host-1"),
		},
		{
			name:        "unsupported version",
			service:     postgrest("14.10", "host-1"),
			expectedErr: `service "api": host host-1 does not support postgrest version 14.10`,
		},
		{
			name:        "service not installed",
			service:     postgrest("14.5", "host-3"),
			expectedErr: `service "api": host host-3 does not support postgrest version 14.5`,
		},
		{
			name:        "latest with different defaults",
			service:     postgrest("latest", "host-1", "host-2"),
			expectedErr: `service "api": unable to resolve version latest: hosts have different default versions of service type "postgrest": 'host-1' has 14.5 and 'host-2' has 14.10`,
		},
		{
			name: "swarm image override with an unlisted version",
			service: func() *ServiceSpec {
				svc := postgrest("99.0", "host-1")
				svc.OrchestratorOpts = &OrchestratorOpts{Swarm: &SwarmOpts{Image: "registry.example.com/postgrest:dev"}}
				return svc
			}(),
		},
		{
			name: "image override doesn't bypass validation on systemd hosts",
			service: func() *ServiceSpec {
				svc := postgrest("99.0", "host-systemd")
				svc.Port = utils.PointerTo(0)
				svc.OrchestratorOpts = &OrchestratorOpts{Swarm: &SwarmOpts{Image: "registry.example.com/postgrest:dev"}}
				return svc
			}(),
			expectedErr: `service "api": host host-systemd does not support postgrest version 99.0`,
		},
		{
			name:    "host without supported services is not validated",
			service: postgrest("99.0", "host-4"),
		},
		{
			name: "systemd host with port",
			service: func() *ServiceSpec {
				svc := postgrest("14.5", "host-systemd")
				svc.Port = utils.PointerTo(0)
				return svc
			}(),
		},
		{
			name:        "systemd host without port",
			service:     postgrest("14.5", "host-systemd"),
			expectedErr: `service "api": port is required for host host-systemd, which uses the systemd orchestrator. set it to 0 to assign a random port`,
		},
		{
			name:        "unknown host",
			service:     postgrest("14.5", "host-5"),
			expectedErr: `service "api": host host-5 not found`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServiceHosts([]*ServiceSpec{tc.service}, hostsByID)
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSpockRequirements(t *testing.T) {
	withSpock := ds.MustParsePgEdgeVersion("17.6", "5")
	withoutSpock := ds.MustParsePgEdgeVersion("17.6", "")
	hostsByID := map[string]*host.Host{
		"host-1": {ID: "host-1", SupportedPgEdgeVersions: []*ds.PgEdgeVersion{withoutSpock, withSpock}},
		"host-2": {ID: "host-2", SupportedPgEdgeVersions: []*ds.PgEdgeVersion{withoutSpock, withSpock}},
	}

	for _, tc := range []struct {
		name        string
		spec        *Spec
		version     *ds.PgEdgeVersion
		expectedErr string
	}{
		{
			name:    "spock with multiple nodes",
			spec:    &Spec{Nodes: []*Node{{Name: "n1"}, {Name: "n2"}}},
			version: withSpock,
		},
		{
			name:    "no spock with a single node",
			spec:    &Spec{Nodes: []*Node{{Name: "n1"}}},
			version: withoutSpock,
		},
		{
			name: "no spock with multiple nodes",
			spec: &Spec{
				Nodes: []*Node{{Name: "n1"}, {Name: "n2"}},
			},
			version:     withoutSpock,
			expectedErr: "databases without spock are limited to a single node",
		},
		{
			name: "no spock with multiple nodes on hosts with spock",
			spec: &Spec{
				PostgresVersion: "17.6",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
			version:     withoutSpock,
			expectedErr: `databases without spock are limited to a single node: set spock_version to "5" to add spock to this database`,
		},
		{
			name: "spock with spock in shared_preload_libraries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "snowflake, spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: withSpock,
		},
		{
			name: "spock with quoted spock in shared_preload_libraries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": `snowflake, "spock"`},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: withSpock,
		},
		{
			name: "spock with uppercase param name",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"SHARED_PRELOAD_LIBRARIES": "snowflake, spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: withSpock,
		},
		{
			name: "spock missing from spec-level shared_preload_libraries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "pg_stat_statements"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version:     withSpock,
			expectedErr: `postgresql_conf[shared_preload_libraries]: 'spock' must be included in shared_preload_libraries`,
		},
		{
			name: "spock missing from node-level shared_preload_libraries",
			spec: &Spec{
				Nodes: []*Node{
					{Name: "n1"},
					{
						Name:           "n2",
						PostgreSQLConf: map[string]any{"shared_preload_libraries": "pg_stat_statements"},
					},
				},
			},
			version:     withSpock,
			expectedErr: `nodes[1].postgresql_conf[shared_preload_libraries]: 'spock' must be included in shared_preload_libraries`,
		},
		{
			name: "no spock without spock in shared_preload_libraries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "pg_stat_statements"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: withoutSpock,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSpockRequirements(tc.spec, tc.version, hostsByID)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePreloadedLibraries(t *testing.T) {
	version := &ds.PgEdgeVersion{
		PostgresVersion:    ds.MustParseVersion("17.6"),
		SpockVersion:       ds.MustParseVersion("5"),
		AvailableLibraries: []string{"auto_explain", "pg_stat_statements", "pgxml", "plpgsql", "spock"},
	}

	for _, tc := range []struct {
		name        string
		spec        *Spec
		version     *ds.PgEdgeVersion
		expectedErr string
	}{
		{
			name:    "parameter absent",
			spec:    &Spec{Nodes: []*Node{{Name: "n1"}}},
			version: version,
		},
		{
			name: "available spec-level libraries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": `pg_stat_statements, "spock"`},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: version,
		},
		{
			name: "libraries without an extension",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "auto_explain,pgxml,spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: version,
		},
		{
			name: "library with file extension",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "pg_stat_statements.so,spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: version,
		},
		{
			name: "uninstalled built-in library",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "sepgsql,spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version:     version,
			expectedErr: `postgresql_conf[shared_preload_libraries]: 'sepgsql' is not installed on host host-1`,
		},
		{
			name: "path-style entries",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": `$libdir/plugins/foo,/opt/lib/bar.so,"/opt/pg/my,module.so",spock`},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: version,
		},
		{
			name: "unavailable spec-level library",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "pg_stat_statements,snowflake,spock"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version:     version,
			expectedErr: `postgresql_conf[shared_preload_libraries]: 'snowflake' is not installed on host host-1 for postgres=17.6, spock=5`,
		},
		{
			name: "unavailable library with uppercase param name",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"SHARED_PRELOAD_LIBRARIES": "snowflake"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version:     version,
			expectedErr: `postgresql_conf[SHARED_PRELOAD_LIBRARIES]: 'snowflake' is not installed on host host-1`,
		},
		{
			name: "unavailable node-level library",
			spec: &Spec{
				Nodes: []*Node{{
					Name:           "n1",
					PostgreSQLConf: map[string]any{"shared_preload_libraries": "spock,pg_cron"},
				}},
			},
			version:     version,
			expectedErr: `nodes[0].postgresql_conf[shared_preload_libraries]: 'pg_cron' is not installed on host host-1`,
		},
		{
			name: "node-level parameter overrides spec-level parameter",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "snowflake"},
				Nodes: []*Node{{
					Name:           "n1",
					PostgreSQLConf: map[string]any{"shared_preload_libraries": "spock"},
				}},
			},
			version: version,
		},
		{
			// The API rejects lists with invalid syntax.
			name: "invalid list syntax is not validated",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": `"snowflake`},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: version,
		},
		{
			name: "host without available libraries is not validated",
			spec: &Spec{
				PostgreSQLConf: map[string]any{"shared_preload_libraries": "snowflake"},
				Nodes:          []*Node{{Name: "n1"}},
			},
			version: ds.MustParsePgEdgeVersion("17.6", "5"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePreloadedLibraries(tc.spec, 0, "host-1", tc.version)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAvailableSpockVersion(t *testing.T) {
	newHost := func(id string, versions ...*ds.PgEdgeVersion) *host.Host {
		return &host.Host{ID: id, SupportedPgEdgeVersions: versions}
	}
	hostsByID := map[string]*host.Host{
		"host-1": newHost("host-1",
			ds.MustParsePgEdgeVersion("17.6", ""),
			ds.MustParsePgEdgeVersion("17.6", "4"),
			ds.MustParsePgEdgeVersion("17.6", "5"),
			ds.MustParsePgEdgeVersion("18.1", "5"),
		),
		"host-2": newHost("host-2",
			ds.MustParsePgEdgeVersion("17.6", ""),
			ds.MustParsePgEdgeVersion("17.6", "4"),
			ds.MustParsePgEdgeVersion("18.1", "5"),
		),
		"host-3": newHost("host-3",
			ds.MustParsePgEdgeVersion("17.6", ""),
			ds.MustParsePgEdgeVersion("18.1", "5"),
		),
	}

	for _, tc := range []struct {
		name     string
		spec     *Spec
		expected string
	}{
		{
			name: "greatest version supported by every host",
			spec: &Spec{
				PostgresVersion: "17.6",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
			expected: "4",
		},
		{
			name: "greatest version on a single host",
			spec: &Spec{
				PostgresVersion: "17.6",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-1"}},
				},
			},
			expected: "5",
		},
		{
			name: "host without spock for the postgres version",
			spec: &Spec{
				PostgresVersion: "17.6",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-3"}},
				},
			},
			expected: "",
		},
		{
			name: "node-level postgres version",
			spec: &Spec{
				PostgresVersion: "17.6",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-2"}},
					{Name: "n2", HostIDs: []string{"host-3"}, PostgresVersion: "18.1"},
				},
			},
			expected: "",
		},
		{
			name: "unknown host",
			spec: &Spec{
				PostgresVersion: "18.1",
				Nodes: []*Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-4"}},
				},
			},
			expected: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, availableSpockVersion(tc.spec, hostsByID))
		})
	}
}
