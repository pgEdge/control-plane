package swarm

import (
	"net/netip"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

// TestGeneratePatroniConfig golden-tests the Swarm patroni config generator,
// focused on the pg_hba/pg_ident wiring: the user zone sits between the
// bridge-subnet reject and the catch-all, system users are rejected over both
// IPv4 and IPv6, the catch-all auth method follows password_encryption, and
// pg_ident is populated from the spec.
func TestGeneratePatroniConfig(t *testing.T) {
	golden := &testutils.GoldenTest[patroni.Config]{
		Compare: func(t testing.TB, expected, actual patroni.Config) {
			// Number types are lost in the YAML round-trip, so normalize the
			// actual value the same way before comparing.
			raw, err := yaml.Marshal(actual)
			require.NoError(t, err)

			var roundTripped patroni.Config
			require.NoError(t, yaml.Unmarshal(raw, &roundTripped))

			require.Equal(t, expected, roundTripped)
		},
		FileExtension: ".yaml",
		Marshal:       yaml.Marshal,
		Unmarshal:     yaml.Unmarshal,
	}

	bridgeInfo := &docker.NetworkInfo{
		Name:    "bridge",
		Subnet:  netip.MustParsePrefix("172.17.0.0/16"),
		Gateway: netip.MustParseAddr("172.17.0.1"),
	}
	dbNetwork := &Network{
		Name:    "pghba-database",
		Subnet:  netip.MustParsePrefix("10.128.170.0/26"),
		Gateway: netip.MustParseAddr("10.128.170.1"),
	}
	etcdCreds := &EtcdCreds{
		Username: "instance.pghba-n1-689qacsi",
		Password: "password",
	}

	for _, tc := range []struct {
		name string
		spec *database.InstanceSpec
	}{
		{
			name: "user pg_hba pg_ident and scram",
			spec: &database.InstanceSpec{
				InstanceID:    "pghba-n1-689qacsi",
				DatabaseID:    "pghba",
				HostID:        "host-1",
				DatabaseName:  "testdb",
				NodeName:      "n1",
				NodeOrdinal:   1,
				PgEdgeVersion: ds.MustParsePgEdgeVersion("18.4", "5.0.8"),
				ClusterSize:   3,
				CPUs:          4,
				MemoryBytes:   1024 * 1024 * 1024 * 8,
				// password_encryption drives the gateway and catch-all auth method.
				PostgreSQLConf: map[string]any{
					"password_encryption": "scram-sha-256",
				},
				// Node-level entries are already prepended ahead of
				// database-level entries by NodeInstances() before reaching the
				// generator.
				PgHbaConf: []string{
					"host testdb myapp_user 10.0.0.0/8 scram-sha-256",
					"hostssl all myapp_user 203.0.113.0/24 scram-sha-256",
				},
				PgIdentConf: []string{"ssl_users CN=alice,O=example alice"},
			},
		},
		{
			name: "no user entries defaults to md5",
			spec: &database.InstanceSpec{
				InstanceID:    "plain-n1-689qacsi",
				DatabaseID:    "plain",
				HostID:        "host-1",
				DatabaseName:  "testdb",
				NodeName:      "n1",
				NodeOrdinal:   1,
				PgEdgeVersion: ds.MustParsePgEdgeVersion("18.4", "5.0.8"),
				ClusterSize:   3,
				CPUs:          4,
				MemoryBytes:   1024 * 1024 * 1024 * 8,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := generatePatroniConfig(
				tc.spec,
				"postgres-"+tc.spec.InstanceID,
				4,
				1024*1024*1024*8,
				[]string{"https://10.0.0.1:2379"},
				etcdCreds,
				bridgeInfo,
				dbNetwork,
				false,
			)
			require.NoError(t, err)

			golden.Run(t, *cfg, update)
		})
	}
}
