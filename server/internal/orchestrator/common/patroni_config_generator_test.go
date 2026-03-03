package common_test

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestPatroniConfigGenerator(t *testing.T) {
	golden := &testutils.GoldenTest[*patroni.Config]{
		Compare: func(t testing.TB, expected, actual *patroni.Config) {
			// The specific number types (e.g. int64) get lost in the conversion
			// to and from YAML, so we marshal and unmarshal the actual value
			// before comparison to normalize it.
			raw, err := yaml.Marshal(actual)
			require.NoError(t, err)

			var roundTrippedActual *patroni.Config
			require.NoError(t, yaml.Unmarshal(raw, &roundTrippedActual))

			require.Equal(t, expected, roundTrippedActual)
		},
		FileExtension: ".yaml",
		Marshal:       yaml.Marshal,
		Unmarshal:     yaml.Unmarshal,
	}

	for _, tc := range []struct {
		name            string
		options         common.PatroniConfigGeneratorOptions
		etcdHosts       []string
		etcdCreds       *common.EtcdCreds
		generateOptions common.GenerateOptions
	}{
		{
			name: "minimal swarm",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "storefront-n1-689qacsi",
					DatabaseID:    "storefront",
					HostID:        "host-1",
					DatabaseName:  "app",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/opt/pgedge"},
					Host:           common.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{
					"172.17.0.1/32",
					"10.128.165.128/26",
				},
				ExtraHbaEntries: []hba.Entry{
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/32",
						AuthMethod: hba.AuthMethodMD5,
					},
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/16",
						AuthMethod: hba.AuthMethodReject,
					},
				},
			},
		},
		{
			name: "with backup config",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "storefront-n1-689qacsi",
					DatabaseID:    "storefront",
					HostID:        "host-1",
					DatabaseName:  "app",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
					BackupConfig:  &database.BackupConfig{},
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/opt/pgedge"},
					Host:           common.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{
					"172.17.0.1/32",
					"10.128.165.128/26",
				},
				ExtraHbaEntries: []hba.Entry{
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/32",
						AuthMethod: hba.AuthMethodMD5,
					},
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/16",
						AuthMethod: hba.AuthMethodReject,
					},
				},
			},
		},
		{
			name: "with restore config",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "storefront-n1-689qacsi",
					DatabaseID:    "storefront",
					HostID:        "host-1",
					DatabaseName:  "app",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
					RestoreConfig: &database.RestoreConfig{},
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/opt/pgedge"},
					Host:           common.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{
					"172.17.0.1/32",
					"10.128.165.128/26",
				},
				ExtraHbaEntries: []hba.Entry{
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/32",
						AuthMethod: hba.AuthMethodMD5,
					},
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/16",
						AuthMethod: hba.AuthMethodReject,
					},
				},
			},
		},
		{
			name: "in-place restore",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:     "storefront-n1-689qacsi",
					DatabaseID:     "storefront",
					HostID:         "host-1",
					DatabaseName:   "app",
					NodeName:       "n1",
					NodeOrdinal:    1,
					PgEdgeVersion:  host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:    3,
					RestoreConfig:  &database.RestoreConfig{},
					InPlaceRestore: true,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/opt/pgedge"},
					Host:           common.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{
					"172.17.0.1/32",
					"10.128.165.128/26",
				},
				ExtraHbaEntries: []hba.Entry{
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/32",
						AuthMethod: hba.AuthMethodMD5,
					},
					{
						Type:       hba.EntryTypeHost,
						Database:   "all",
						User:       "all",
						Address:    "172.17.0.1/16",
						AuthMethod: hba.AuthMethodReject,
					},
				},
			},
		},
		{
			name: "minimal systemd",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "storefront-n1-689qacsi",
					DatabaseID:    "storefront",
					HostID:        "host-1",
					DatabaseName:  "app",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,spock",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					Host:           common.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{
					"10.10.0.2",
					"10.10.0.3",
					"10.10.0.4",
				},
			},
		},
		{
			name: "enable fast basebackup",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "storefront-n1-689qacsi",
					DatabaseID:    "storefront",
					HostID:        "host-1",
					DatabaseName:  "app",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: host.MustPgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,spock",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/var/lib/pgsql/storefront-n1-689qacsi"},
					Host:           common.Paths{BaseDir: "/var/lib/pgsql/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{
				"i-0123456789abcdef.ec2.internal:2379",
			},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.storefront-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				// This is a newly-created node, so there are no clients to
				// disrupt by enabling this option.
				EnableFastBasebackup: true,
				SystemAddresses: []string{
					"10.10.0.2",
					"10.10.0.3",
					"10.10.0.4",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := common.NewPatroniConfigGenerator(tc.options)
			out := gen.Generate(tc.etcdHosts, tc.etcdCreds, tc.generateOptions)

			golden.Run(t, out, update)
		})
	}
}
