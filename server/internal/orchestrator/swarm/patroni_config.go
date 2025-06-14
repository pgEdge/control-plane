package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"

	"github.com/samber/do"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*PatroniConfig)(nil)

const ResourceTypePatroniConfig resource.Type = "swarm.patroni_config"

func PatroniConfigIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniConfig,
	}
}

type PatroniConfig struct {
	Spec                *database.InstanceSpec `json:"spec"`
	ParentID            string                 `json:"parent_id"`
	HostCPUs            float64                `json:"host_cpus"`
	HostMemoryBytes     uint64                 `json:"host_memory_bytes"`
	BridgeNetworkInfo   *docker.NetworkInfo    `json:"host_network_info"`
	DatabaseNetworkName string                 `json:"database_network_name"`
	OwnerUID            int                    `json:"owner_uid"`
	OwnerGID            int                    `json:"owner_gid"`
	InstanceHostname    string                 `json:"instance_hostname"`
}

func (c *PatroniConfig) ResourceVersion() string {
	return "1"
}

func (c *PatroniConfig) DiffIgnore() []string {
	return nil
}

func (c *PatroniConfig) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   c.Spec.HostID,
	}
}

func (c *PatroniConfig) Identifier() resource.Identifier {
	return PatroniConfigIdentifier(c.Spec.InstanceID)
}

func (c *PatroniConfig) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		filesystem.DirResourceIdentifier(c.ParentID),
		NetworkResourceIdentifier(c.DatabaseNetworkName),
		EtcdCredsIdentifier(c.Spec.InstanceID),
		PatroniMemberResourceIdentifier(c.Spec.InstanceID),
		PatroniClusterResourceIdentifier(c.Spec.NodeName),
	}
	if c.Spec.RestoreConfig != nil {
		deps = append(deps, PgBackRestConfigIdentifier(c.Spec.InstanceID, PgBackRestConfigTypeRestore))
	}
	if c.Spec.BackupConfig != nil {
		deps = append(deps, PgBackRestConfigIdentifier(c.Spec.InstanceID, PgBackRestConfigTypeBackup))
	}
	return deps
}

func (c *PatroniConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	contents, err := readResourceFile(fs, filepath.Join(parentFullPath, "patroni.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read patroni config: %w", err)
	}

	var config *patroni.Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return fmt.Errorf("failed to unmarshal patroni config: %w", err)
	}

	return nil
}

func (c *PatroniConfig) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}
	etcdClient, err := do.Invoke[*clientv3.Client](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	network, err := resource.FromContext[*Network](rc, NetworkResourceIdentifier(c.DatabaseNetworkName))
	if err != nil {
		return fmt.Errorf("failed to get database network from state: %w", err)
	}
	etcdCreds, err := resource.FromContext[*EtcdCreds](rc, EtcdCredsIdentifier(c.Spec.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get etcd creds from state: %w", err)
	}

	members, err := etcdClient.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd cluster members: %w", err)
	}
	var endpoints []string
	for _, member := range members.Members {
		endpoints = append(endpoints, member.GetClientURLs()...)
	}

	config, err := generatePatroniConfig(
		c.Spec,
		c.InstanceHostname,
		c.HostCPUs,
		c.HostMemoryBytes,
		endpoints,
		etcdCreds,
		c.BridgeNetworkInfo,
		network,
	)
	if err != nil {
		return fmt.Errorf("failed to generate patroni config: %w", err)
	}

	content, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal patroni config: %w", err)
	}

	configPath := filepath.Join(parentFullPath, "patroni.yaml")
	if err := afero.WriteFile(fs, configPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := fs.Chown(configPath, c.OwnerUID, c.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for %s: %w", configPath, err)
	}

	return nil
}

func (c *PatroniConfig) Update(ctx context.Context, rc *resource.Context) error {
	return c.Create(ctx, rc)
}

func (c *PatroniConfig) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, c.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	err = fs.Remove(filepath.Join(parentFullPath, "patroni.yaml"))
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove patroni.yaml: %w", err)
	}

	return nil
}

func generatePatroniConfig(
	spec *database.InstanceSpec,
	instanceHostname string,
	hostCPUs float64,
	hostMemoryBytes uint64,
	etcdEndpoints []string,
	etcdCreds *EtcdCreds,
	bridgeInfo *docker.NetworkInfo,
	dbNetworkInfo *Network,
) (*patroni.Config, error) {
	memoryBytes := spec.MemoryBytes
	if memoryBytes == 0 {
		memoryBytes = hostMemoryBytes
	}
	cpus := spec.CPUs
	if cpus == 0 {
		cpus = hostCPUs
	}

	parameters := postgres.DefaultGUCs()
	maps.Copy(parameters, postgres.Spock4DefaultGUCs())
	maps.Copy(parameters, postgres.DefaultTunableGUCs(memoryBytes, cpus, spec.ClusterSize))
	maps.Copy(parameters, map[string]any{
		"shared_preload_libraries": "pg_stat_statements,snowflake,spock,postgis-3", // The docker image includes postgis-3
		"ssl":                      "on",
		"ssl_ca_file":              "/opt/pgedge/certificates/postgres/ca.crt",
		"ssl_cert_file":            "/opt/pgedge/certificates/postgres/server.crt",
		"ssl_key_file":             "/opt/pgedge/certificates/postgres/server.key",
	})
	if spec.BackupConfig != nil {
		maps.Copy(parameters, map[string]any{
			// It's safe to set this to "on" on every instance in the node
			// because "on" (as opposed to "always") will only archive from the
			// primary instance.
			"archive_mode":    "on",
			"archive_command": PgBackRestBackupCmd("archive-push", `"%p"`).String(),
		})
	}
	snowflakeLolorGUCs, err := postgres.SnowflakeLolorGUCs(spec.NodeOrdinal)
	if err != nil {
		return nil, fmt.Errorf("failed to generate snowflake/lolor GUCs: %w", err)
	}
	maps.Copy(parameters, snowflakeLolorGUCs)
	maps.Copy(parameters, spec.PostgreSQLConf)
	dcsParameters := patroni.ExtractPatroniControlledGUCs(parameters)

	staticLogFields := map[string]string{
		"database_id": spec.DatabaseID,
		"instance_id": spec.InstanceID,
		"node_name":   spec.NodeName,
	}
	if spec.TenantID != nil {
		staticLogFields["tenant_id"] = *spec.TenantID
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

	cfg := &patroni.Config{
		Name:      utils.PointerTo(spec.InstanceID),
		Namespace: utils.PointerTo(patroni.Namespace(spec.DatabaseID, spec.NodeName)),
		Scope:     utils.PointerTo(spec.DatabaseID + ":" + spec.NodeName),
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
			Username: &etcdCreds.Username,
			Password: &etcdCreds.Password,
			Protocol: utils.PointerTo("https"),
		},
		RestAPI: &patroni.RestAPI{
			ConnectAddress: utils.PointerTo(fmt.Sprintf("%s.%s-database:8888", instanceHostname, spec.DatabaseID)),
			Listen:         utils.PointerTo("0.0.0.0:8888"),
			Allowlist: &[]string{
				bridgeInfo.Gateway.String(),   // Control plane will connect from this address
				dbNetworkInfo.Subnet.String(), // Other cluster members will come from this CIDR
				"127.0.0.1",                   // Local connections for docker exec use
				"localhost",
			},
		},
		Watchdog: &patroni.Watchdog{
			Mode: utils.PointerTo(patroni.WatchdogModeOff),
		},
		Postgresql: &patroni.PostgreSQL{
			ConnectAddress: utils.PointerTo(fmt.Sprintf("%s:5432", instanceHostname)),
			DataDir:        utils.PointerTo("/opt/pgedge/data/pgdata"),
			Parameters:     &parameters,
			Listen:         utils.PointerTo("*:5432"),
			Callbacks:      &patroni.Callbacks{},
			Authentication: &patroni.Authentication{
				Superuser: &patroni.User{
					Username:    utils.PointerTo("pgedge"),
					SSLRootCert: utils.PointerTo("/opt/pgedge/certificates/postgres/ca.crt"),
					SSLCert:     utils.PointerTo("/opt/pgedge/certificates/postgres/superuser.crt"),
					SSLKey:      utils.PointerTo("/opt/pgedge/certificates/postgres/superuser.key"),
					SSLMode:     utils.PointerTo("verify-full"),
				},
				Replication: &patroni.User{
					Username:    utils.PointerTo("patroni_replicator"),
					SSLRootCert: utils.PointerTo("/opt/pgedge/certificates/postgres/ca.crt"),
					SSLCert:     utils.PointerTo("/opt/pgedge/certificates/postgres/replication.crt"),
					SSLKey:      utils.PointerTo("/opt/pgedge/certificates/postgres/replication.key"),
					SSLMode:     utils.PointerTo("verify-full"),
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
				hba.Entry{
					Type:       hba.EntryTypeLocal,
					Database:   "replication",
					User:       "all",
					AuthMethod: hba.AuthMethodTrust,
				}.String(),
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "replication",
					User:       "all",
					Address:    "127.0.0.1/32",
					AuthMethod: hba.AuthMethodTrust,
				}.String(),
				hba.Entry{
					Type:       hba.EntryTypeHost,
					Database:   "replication",
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
					Address:     dbNetworkInfo.Subnet.String(),
					AuthMethod:  hba.AuthMethodCert,
					AuthOptions: "clientcert=verify-full",
				}.String(),
				hba.Entry{
					Type:        hba.EntryTypeHostSSL,
					Database:    "replication",
					User:        "pgedge,patroni_replicator",
					Address:     fmt.Sprintf("%s/32", bridgeInfo.Gateway.String()),
					AuthMethod:  hba.AuthMethodCert,
					AuthOptions: "clientcert=verify-full",
				}.String(),
				hba.Entry{
					Type:        hba.EntryTypeHostSSL,
					Database:    "replication",
					User:        "pgedge,patroni_replicator",
					Address:     dbNetworkInfo.Subnet.String(),
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
	}

	if spec.RestoreConfig != nil {
		restoreOptions := utils.BuildOptionArgs(spec.RestoreConfig.RestoreOptions)
		cfg.Bootstrap.Method = utils.PointerTo(patroni.BootstrapMethodNameRestore)
		cfg.Bootstrap.Restore = &patroni.BootstrapMethodConf{
			Command:                  utils.PointerTo(PgBackRestRestoreCmd("restore", restoreOptions...).String()),
			NoParams:                 utils.PointerTo(true),
			KeepExistingRecoveryConf: utils.PointerTo(true),
		}
	}

	return cfg, nil
}
