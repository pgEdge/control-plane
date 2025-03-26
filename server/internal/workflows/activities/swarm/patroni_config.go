package swarm

import (
	"fmt"
	"maps"
	"net/url"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func PatroniConfig(input *WriteInstanceConfigsInput, bridgeInfo *docker.NetworkInfo, etcdEndpoints []string, etcdUsername string) (*patroni.Config, error) {
	memoryBytes := input.Spec.MemoryBytes
	if memoryBytes == 0 {
		memoryBytes = input.Host.MemBytes
	}
	cpus := input.Spec.CPUs
	if cpus == 0 {
		cpus = float64(input.Host.CPUs)
	}

	parameters := postgres.DefaultGUCs()
	maps.Copy(parameters, postgres.Spock4DefaultGUCs())
	maps.Copy(parameters, postgres.DefaultTunableGUCs(memoryBytes, cpus, input.ClusterSize))
	maps.Copy(parameters, input.Spec.PostgreSQLConf)
	maps.Copy(parameters, map[string]any{
		"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3", // The docker image includes postgis-3
		"ssl":                      "on",
		"ssl_ca_file":              "/opt/pgedge/certificates/postgres/ca.crt",
		"ssl_cert_file":            "/opt/pgedge/certificates/postgres/server.crt",
		"ssl_key_file":             "/opt/pgedge/certificates/postgres/server.key",
	})
	snowflakeLolorGUCs, err := postgres.SnowflakeLolorGUCs(input.Spec.NodeOrdinal)
	if err != nil {
		return nil, fmt.Errorf("failed to generate snowflake/lolor GUCs: %w", err)
	}
	maps.Copy(parameters, snowflakeLolorGUCs)
	dcsParameters := patroni.ExtractPatroniControlledGUCs(parameters)

	staticLogFields := map[string]string{
		"database_id": input.Spec.DatabaseID.String(),
		"instance_id": input.Spec.InstanceID.String(),
		"node_name":   input.Spec.NodeName,
	}
	if input.Spec.TenantID != nil {
		staticLogFields["tenant_id"] = input.Spec.TenantID.String()
	}

	// Patroni requires the etcd endpoints to be in the format "host:port"
	etcdHosts := make([]string, len(etcdEndpoints))
	for i, endpoint := range etcdEndpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("got invalid etcd endpoint %q: %w", endpoint, err)
		}
		etcdHosts[i] = u.Host
	}

	return &patroni.Config{
		Name:      utils.PointerTo(input.Spec.InstanceID.String()),
		Namespace: utils.PointerTo(patroni.Namespace(input.Spec.DatabaseID, input.Spec.NodeName)),
		Scope:     utils.PointerTo(input.Spec.DatabaseID.String() + ":" + input.Spec.NodeName),
		Log: &patroni.Log{
			Type:         utils.PointerTo(patroni.LogTypeJson),
			Level:        utils.PointerTo(patroni.LogLevelInfo),
			StaticFields: &staticLogFields,
		},
		Bootstrap: &patroni.Bootstrap{
			DCS: &patroni.DCS{
				Postgresql: &patroni.DCSPostgreSQL{
					Parameters: &dcsParameters,
				},
				IgnoreSlots: &[]patroni.IgnoreSlot{
					{Plugin: utils.PointerTo("spock_output")},
				},
				TTL:          utils.PointerTo(30),
				LoopWait:     utils.PointerTo(10),
				RetryTimeout: utils.PointerTo(10),
			},
		},
		Etcd3: &patroni.Etcd{
			Hosts:    &etcdHosts,
			CACert:   utils.PointerTo("/opt/pgedge/certificates/etcd/ca.crt"),
			Cert:     utils.PointerTo("/opt/pgedge/certificates/etcd/client.crt"),
			Key:      utils.PointerTo("/opt/pgedge/certificates/etcd/client.key"),
			Username: &etcdUsername,
			Password: utils.PointerTo(input.Spec.InstanceID.String()), // TODO
			Protocol: utils.PointerTo("https"),
		},
		RestAPI: &patroni.RestAPI{
			ConnectAddress: utils.PointerTo(fmt.Sprintf("%s:8888", input.InstanceHostname)),
			Listen:         utils.PointerTo("0.0.0.0:8888"),
			Allowlist: &[]string{
				bridgeInfo.Gateway.String(),           // Control plane will connect from this address
				input.DatabaseNetwork.Subnet.String(), // Other cluster members will come from this CIDR
				"127.0.0.1",                           // Local connections for docker exec use
				"localhost",
			},
		},
		Watchdog: &patroni.Watchdog{
			Mode: utils.PointerTo(patroni.WatchdogModeOff),
		},
		Postgresql: &patroni.PostgreSQL{
			ConnectAddress: utils.PointerTo(fmt.Sprintf("%s:5432", input.InstanceHostname)),
			DataDir:        utils.PointerTo("/opt/pgedge/data"),
			Parameters:     &parameters,
			Listen:         utils.PointerTo("*:5432"),
			Authentication: &patroni.Authentication{
				Superuser: &patroni.User{
					Username:    utils.PointerTo("pgedge"),
					SSLRootCert: utils.PointerTo("/opt/pgedge/certificates/postgres/ca.crt"),
					SSLCert:     utils.PointerTo("/opt/pgedge/certificates/postgres/superuser.crt"),
					SSLKey:      utils.PointerTo("/opt/pgedge/certificates/postgres/superuser.key"),
				},
				Replication: &patroni.User{
					Username:    utils.PointerTo("patroni_replicator"),
					SSLRootCert: utils.PointerTo("/opt/pgedge/certificates/postgres/ca.crt"),
					SSLCert:     utils.PointerTo("/opt/pgedge/certificates/postgres/patroni-replicator.crt"),
					SSLKey:      utils.PointerTo("/opt/pgedge/certificates/postgres/patroni-replicator.key"),
				},
			},
			PgHba: &[]string{
				// Trust local connections
				hba.Entry{
					Type:       hba.EntryTypeLocal,
					Database:   "all",
					User:       "all",
					AuthMethod: hba.AuthMethodTrust,
				}.String(),
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "all",
					Address:    "127.0.0.1/32",
					AuthMethod: hba.AuthMethodTrust,
				}.String(),
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "all",
					Address:    "::1/128",
					AuthMethod: hba.AuthMethodTrust,
				}.String(),
				// Reject connections for system users except for SSL
				// connections from the bridge network gateway (the control
				// plane server) or SSL connections from peers.
				hba.Entry{
					Type:        hba.EntryTypeHostSSL,
					Database:    "all",
					User:        "pgedge,patroni_replicator",
					Address:     fmt.Sprintf("%s/32", bridgeInfo.Gateway.String()),
					AuthMethod:  hba.AuthMethodCert,
					AuthOptions: "clientcert=verify-full",
				}.String(),
				hba.Entry{
					Type:        hba.EntryTypeHostSSL,
					Database:    "all",
					User:        "pgedge,patroni_replicator",
					Address:     input.DatabaseNetwork.Subnet.String(),
					AuthMethod:  hba.AuthMethodCert,
					AuthOptions: "clientcert=verify-full",
				}.String(),
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "pgedge,patroni_replicator",
					Address:    "0.0.0.0/0",
					AuthMethod: hba.AuthMethodReject,
				}.String(),
				// Use MD5 for non-system users from the gateway. External
				// connections will originate from this address when we publish
				// a host port.
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "all",
					Address:    fmt.Sprintf("%s/32", bridgeInfo.Gateway.String()),
					AuthMethod: hba.AuthMethodMD5,
				}.String(),
				// Reject all other connections on the bridge network to prevent
				// connections from other databases.
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "all",
					Address:    bridgeInfo.Subnet.String(),
					AuthMethod: hba.AuthMethodReject,
				}.String(),
				// Use MD5 for non-system users from all other connections
				// TODO: Can we upgrade this to scram-sha-256?
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "all",
					User:       "all",
					Address:    "0.0.0.0/0",
					AuthMethod: hba.AuthMethodMD5,
				}.String(),
			},
		},
	}, nil
}
