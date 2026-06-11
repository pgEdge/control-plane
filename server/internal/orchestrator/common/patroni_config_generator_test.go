package common_test

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestPatroniConfigGenerator(t *testing.T) {
	// goccy/go-yaml won't unmarshal into a pointer-to-a-pointer, so this needs
	// to be patroni.Config and not *patroni.Config.
	golden := &testutils.GoldenTest[patroni.Config]{
		Compare: func(t testing.TB, expected, actual patroni.Config) {
			// The specific number types (e.g. int64) get lost in the conversion
			// to and from YAML, so we marshal and unmarshal the actual value
			// before comparison to normalize it.
			raw, err := yaml.Marshal(actual)
			require.NoError(t, err)

			var roundTrippedActual patroni.Config
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
					NodeSize:      1,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				LogType:         patroni.LogTypeJson,
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/opt/pgedge"},
					Host:           database.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
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
					NodeSize:      2,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
					BackupConfig:  &database.BackupConfig{},
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				LogType:         patroni.LogTypeJson,
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/opt/pgedge"},
					Host:           database.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
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
					NodeSize:      2,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:   3,
					RestoreConfig: &database.RestoreConfig{},
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				LogType:         patroni.LogTypeJson,
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/opt/pgedge"},
					Host:           database.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
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
					NodeSize:       2,
					PgEdgeVersion:  ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
					ClusterSize:    3,
					RestoreConfig:  &database.RestoreConfig{},
					InPlaceRestore: true,
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "storefront-n1-689qacsi.storefront-database",
				LogType:         patroni.LogTypeJson,
				OrchestratorParameters: map[string]any{
					"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3",
				},
				PatroniPort:  8888,
				PostgresPort: 5432,
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/opt/pgedge"},
					Host:           database.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
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
					NodeSize:      2,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
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
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					Host:           database.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/bin/patroni",
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
					NodeSize:      2,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.1", "5.0.4"),
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
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					Host:           database.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/bin/patroni",
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
		{
			name: "user pg_hba pg_ident and scram",
			options: common.PatroniConfigGeneratorOptions{
				Instance: &database.InstanceSpec{
					InstanceID:    "pghba-n1-689qacsi",
					DatabaseID:    "pghba",
					HostID:        "host-1",
					DatabaseName:  "testdb",
					NodeName:      "n1",
					NodeOrdinal:   1,
					PgEdgeVersion: ds.MustParsePgEdgeVersion("18.4", "5.0.8"),
					ClusterSize:   3,
					// password_encryption drives the catch-all auth method.
					PostgreSQLConf: map[string]any{
						"password_encryption": "scram-sha-256",
					},
					// Node-level entries are already prepended ahead of
					// database-level entries by NodeInstances() before reaching
					// the generator.
					PgHbaConf: []string{
						"host testdb myapp_user 10.0.0.0/8 scram-sha-256",
						"hostssl all myapp_user 203.0.113.0/24 scram-sha-256",
					},
					PgIdentConf: []string{"ssl_users CN=alice,O=example alice"},
				},
				HostCPUs:        4,
				HostMemoryBytes: 1024 * 1024 * 1024 * 8,
				FQDN:            "pghba-n1-689qacsi.pghba-database",
				LogType:         patroni.LogTypeJson,
				PatroniPort:     8888,
				PostgresPort:    5432,
				Paths: database.InstancePaths{
					Instance:       database.Paths{BaseDir: "/opt/pgedge"},
					Host:           database.Paths{BaseDir: "/data/control-plane/instances/pghba-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/local/bin/patroni",
				},
			},
			etcdHosts: []string{"i-0123456789abcdef.ec2.internal:2379"},
			etcdCreds: &common.EtcdCreds{
				Username: "instance.pghba-n1-689qacsi",
				Password: "password",
			},
			generateOptions: common.GenerateOptions{
				SystemAddresses: []string{"172.17.0.1/32", "10.128.165.128/26"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := common.NewPatroniConfigGenerator(tc.options)
			out := gen.Generate(tc.etcdHosts, tc.etcdCreds, tc.generateOptions)

			golden.Run(t, *out, update)
		})
	}
}
