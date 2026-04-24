package common

import (
	"fmt"
	"maps"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type PatroniConfigGenerator struct {
	// ArchiveCommand sets the Postgres archive command parameter.
	ArchiveCommand string `json:"archive_command,omitempty"`
	// ClusterSize is the number of nodes in the Spock cluster. This is used for
	// the tunable Postgres parameters.
	ClusterSize int `json:"cluster_size"`
	// CPUs is the number of CPUs allocated for this instance. This is used for
	// the tunable Postgres parameters.
	CPUs float64 `json:"cpus,omitempty"`
	// DatabaseID is the Database's ID.
	DatabaseID string `json:"database_id"`
	// DataDir is the Postgres data directory.
	DataDir string `json:"data_dir"`
	// EtcdCertsDir is the Etcd certificates directory.
	EtcdCertsDir string `json:"etcd_certs_dir"`
	// FQDN is the fully-qualified domain name for this instance. This name must
	// be reachable by sibling instances within the Spock node.
	FQDN string `json:"fqdn"`
	// InstanceID is this instance's ID.
	InstanceID string `json:"instance_id"`
	// MemoryBytes is the amount of memory that is allocated for this instance.
	// This is used for the tunable Postgres parameters.
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
	// NodeName is the Spock node name.
	NodeName string `json:"node_name"`
	// NodeOrdinal is the ordinal part of the Spock node name, e.g. for 'n1'
	// this would be '1'. This is used to configure the Snowflake and LOLOR
	// extensions.
	NodeOrdinal int `json:"node_ordinal"`
	// OrchestratorParameters are additional parameters to be provided by the
	// orchestrator implementation.
	OrchestratorParameters map[string]any `json:"orchestrator_parameters,omitempty"`
	// PatroniAllowlist is a user-specified list of addresses, hostnames, or
	// CIDR ranges to include in the allowlist for Patroni's REST API.
	PatroniAllowlist []string `json:"patroni_allowlist"`
	// PatroniPort is the port that Patroni will listen on.
	PatroniPort int `json:"patroni_port"`
	// PostgresCertsDir is the Postgres certificates directory.
	PostgresCertsDir string `json:"postgres_certs_dir"`
	// PostgresPort is the port that Postgres will listen on.
	PostgresPort int `json:"postgres_port"`
	// RestoreCommand is an alternate command to use to bootstrap this instance.
	RestoreCommand string `json:"restore_command"`
	// SpecParameters are user-specified Postgres parameters that are included
	// in the database spec.
	SpecParameters map[string]any `json:"spec_parameters,omitempty"`
	// TenantID is an optional tenant ID that is associated with this instance.
	TenantID *string `json:"tenant_id,omitempty"`
}

type PatroniConfigGeneratorOptions struct {
	// Instance is the instance spec for this instance.
	Instance *database.InstanceSpec
	// HostCPUs is the total number of CPUs available on the host. This is used for
	// the tunable Postgres parameters.
	HostCPUs float64
	// HostMemoryBytes is the total amount of memory available on the host. This
	// is used for the tunable Postgres parameters.
	HostMemoryBytes uint64
	// FQDN is the fully-qualified domain name for this instance. This name must
	// be reachable by sibling instances within the Spock node.
	FQDN string
	// OrchestratorParameters are additional parameters to be provided by the
	// orchestrator implementation.
	OrchestratorParameters map[string]any
	// PatroniPort is the port that Patroni will listen on.
	PatroniPort int
	// PostgresPort is the port that Postgres will listen on.
	PostgresPort int
	// Paths is used to compute the paths of directories and executables.
	Paths database.InstancePaths
}

func NewPatroniConfigGenerator(opts PatroniConfigGeneratorOptions) *PatroniConfigGenerator {
	cpus := opts.Instance.CPUs
	if cpus == 0 {
		cpus = opts.HostCPUs
	}
	memoryBytes := opts.Instance.MemoryBytes
	if memoryBytes == 0 {
		memoryBytes = opts.HostMemoryBytes
	}
	var archiveCommand string
	if opts.Instance.BackupConfig != nil {
		archiveCommand = opts.Paths.PgBackRestBackupCmd("archive-push", `"%p"`).String()
	}
	var restoreCommand string
	if opts.Instance.RestoreConfig != nil {
		if opts.Instance.InPlaceRestore {
			restoreCommand = strings.Join(opts.Paths.InstanceMvRestoreToDataCmd(), " ")
		} else {
			restoreOptions := utils.BuildOptionArgs(opts.Instance.RestoreConfig.RestoreOptions)
			for i, o := range restoreOptions {
				restoreOptions[i] = shellescape.Quote(o)
			}
			restoreCommand = opts.Paths.PgBackRestRestoreCmd("restore", restoreOptions...).String()
		}
	}
	return &PatroniConfigGenerator{
		ArchiveCommand:         archiveCommand,
		ClusterSize:            opts.Instance.ClusterSize,
		CPUs:                   cpus,
		DatabaseID:             opts.Instance.DatabaseID,
		DataDir:                opts.Paths.Instance.PgData(),
		EtcdCertsDir:           opts.Paths.Instance.EtcdCertificates(),
		FQDN:                   opts.FQDN,
		InstanceID:             opts.Instance.InstanceID,
		MemoryBytes:            memoryBytes,
		NodeName:               opts.Instance.NodeName,
		NodeOrdinal:            opts.Instance.NodeOrdinal,
		OrchestratorParameters: opts.OrchestratorParameters,
		PatroniPort:            opts.PatroniPort,
		PostgresCertsDir:       opts.Paths.Instance.PostgresCertificates(),
		PostgresPort:           opts.PostgresPort,
		RestoreCommand:         restoreCommand,
		SpecParameters:         opts.Instance.PostgreSQLConf,
		TenantID:               opts.Instance.TenantID,
		// TODO: Add allowlist field to instance and database types
		// PatroniAllowlist:       opts.Instance.PatroniAllowlist,
	}
}

type GenerateOptions struct {
	// SystemAddresses are IPs, hostnames, or CIDR ranges that pgedge or
	// patroni_replicator connections will originate from.
	SystemAddresses []string
	// ExtraHbaEntries are orchestrator-specific entries to include in the
	// pg_hba.conf.
	ExtraHbaEntries []hba.Entry
	// EnableFastBasebackup enables basebackup's "fast" checkpoint option when
	// bootstrapping this instance from another existing instance.
	EnableFastBasebackup bool
}

func (p *PatroniConfigGenerator) Generate(
	etcdHosts []string,
	etcdCreds *EtcdCreds,
	opts GenerateOptions,
) *patroni.Config {
	parameters := p.parameters()
	dcsParameters := patroni.ExtractPatroniControlledGUCs(parameters)

	return &patroni.Config{
		Name:      utils.PointerTo(p.InstanceID),
		Namespace: utils.PointerTo(patroni.Namespace()),
		Scope:     utils.PointerTo(patroni.ClusterName(p.DatabaseID, p.NodeName)),
		Log:       p.log(),
		Bootstrap: p.bootstrap(dcsParameters),
		Etcd3:     p.etcd(etcdHosts, etcdCreds),
		RestAPI:   p.restAPI(opts.SystemAddresses),
		Watchdog: &patroni.Watchdog{
			Mode: utils.PointerTo(patroni.WatchdogModeOff),
		},
		Postgresql: p.postgreSQL(
			opts.EnableFastBasebackup,
			parameters,
			opts.SystemAddresses,
			opts.ExtraHbaEntries,
		),
	}
}

func (p *PatroniConfigGenerator) parameters() map[string]any {
	parameters := postgres.DefaultGUCs()
	maps.Copy(parameters, postgres.Spock4DefaultGUCs())
	maps.Copy(parameters, postgres.DefaultTunableGUCs(p.MemoryBytes, p.CPUs, p.ClusterSize))
	maps.Copy(parameters, map[string]any{
		"ssl":           "on",
		"ssl_ca_file":   filepath.Join(p.PostgresCertsDir, database.PostgresCaCertName),
		"ssl_cert_file": filepath.Join(p.PostgresCertsDir, database.PostgresServerCertName),
		"ssl_key_file":  filepath.Join(p.PostgresCertsDir, database.PostgresServerKeyName),
	})
	maps.Copy(parameters, p.OrchestratorParameters)
	if p.ArchiveCommand != "" {
		maps.Copy(parameters, map[string]any{
			// It's safe to set this to "on" on every instance in the node
			// because "on" (as opposed to "always") will only archive from the
			// primary instance.
			"archive_mode":    "on",
			"archive_command": p.ArchiveCommand,
		})
	}
	maps.Copy(parameters, postgres.SnowflakeLolorGUCs(p.NodeOrdinal))
	maps.Copy(parameters, p.SpecParameters)

	return parameters
}

func (p *PatroniConfigGenerator) bootstrap(dcsParameters map[string]any) *patroni.Bootstrap {
	bootstrap := &patroni.Bootstrap{
		DCS: &patroni.DCS{
			Postgresql: &patroni.DCSPostgreSQL{
				Parameters: &dcsParameters,
			},
			IgnoreSlots: &[]patroni.IgnoreSlot{
				{Plugin: utils.PointerTo("spock_output")},
			},
			TTL:          utils.PointerTo(30),
			LoopWait:     utils.PointerTo(int(patroni.DefaultLoopWaitSeconds)),
			RetryTimeout: utils.PointerTo(10),
		},
		InitDB: utils.PointerTo([]string{"data-checksums"}),
	}

	if p.RestoreCommand != "" {
		bootstrap.Method = utils.PointerTo(patroni.BootstrapMethodNameRestore)
		bootstrap.Restore = &patroni.BootstrapMethodConf{
			Command:                  utils.PointerTo(p.RestoreCommand),
			NoParams:                 utils.PointerTo(true),
			KeepExistingRecoveryConf: utils.PointerTo(true),
		}
	}

	return bootstrap
}

func (p *PatroniConfigGenerator) log() *patroni.Log {
	fields := map[string]string{
		"database_id": p.DatabaseID,
		"instance_id": p.InstanceID,
		"node_name":   p.NodeName,
	}
	if p.TenantID != nil {
		fields["tenant_id"] = *p.TenantID
	}

	return &patroni.Log{
		Type:         utils.PointerTo(patroni.LogTypeJson),
		Level:        utils.PointerTo(patroni.LogLevelInfo),
		StaticFields: &fields,
	}
}

func (p *PatroniConfigGenerator) etcd(hosts []string, creds *EtcdCreds) *patroni.Etcd {
	return &patroni.Etcd{
		Hosts:    &hosts,
		CACert:   utils.PointerTo(filepath.Join(p.EtcdCertsDir, database.EtcdCaCertName)),
		Cert:     utils.PointerTo(filepath.Join(p.EtcdCertsDir, database.EtcdClientCertName)),
		Key:      utils.PointerTo(filepath.Join(p.EtcdCertsDir, database.EtcdClientKeyName)),
		Username: &creds.Username,
		Password: &creds.Password,
		Protocol: utils.PointerTo("https"),
	}
}

func (p *PatroniConfigGenerator) restAPI(systemAddresses []string) *patroni.RestAPI {
	combined := utils.PointerTo(slices.Concat(
		p.PatroniAllowlist,
		systemAddresses,
		[]string{
			"127.0.0.1", // Always allow local connections
			"localhost",
			"::1",
		},
	))

	return &patroni.RestAPI{
		ConnectAddress: utils.PointerTo(net.JoinHostPort(p.FQDN, strconv.Itoa(p.PatroniPort))),
		Listen:         utils.PointerTo(fmt.Sprintf("0.0.0.0:%d", p.PatroniPort)),
		Allowlist:      combined,
	}
}

func (p *PatroniConfigGenerator) postgreSQL(
	enableFastBasebackup bool,
	parameters map[string]any,
	systemAddresses []string,
	extraEntries []hba.Entry,
) *patroni.PostgreSQL {
	var basebackup *[]any
	if enableFastBasebackup {
		// Causes basebackup to request an immediate checkpoint. The tradeoff
		// is that the checkpoint operation can disrupt clients. We enable it
		// by default for new nodes because the primary shouldn't have any
		// clients outside the control plane.
		basebackup = &[]any{
			map[string]string{"checkpoint": "fast"},
		}
	}

	return &patroni.PostgreSQL{
		ConnectAddress:                         utils.PointerTo(net.JoinHostPort(p.FQDN, strconv.Itoa(p.PostgresPort))),
		DataDir:                                &p.DataDir,
		Parameters:                             &parameters,
		Listen:                                 utils.PointerTo(fmt.Sprintf("*:%d", p.PostgresPort)),
		BaseBackup:                             basebackup,
		UsePgRewind:                            utils.PointerTo(true),
		RemoveDataDirectoryOnRewindFailure:     utils.PointerTo(true),
		RemoveDataDirectoryOnDivergedTimelines: utils.PointerTo(true),
		Authentication:                         p.authentication(),
		PgHba:                                  p.pgHba(systemAddresses, extraEntries),
	}
}

func (p *PatroniConfigGenerator) authentication() *patroni.Authentication {
	return &patroni.Authentication{
		Superuser: &patroni.User{
			Username:    utils.PointerTo("pgedge"),
			SSLRootCert: utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresCaCertName)),
			SSLCert:     utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresSuperuserCertName)),
			SSLKey:      utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresSuperuserKeyName)),
			SSLMode:     utils.PointerTo("verify-full"),
		},
		Replication: &patroni.User{
			Username:    utils.PointerTo("patroni_replicator"),
			SSLRootCert: utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresCaCertName)),
			SSLCert:     utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresReplicatorCertName)),
			SSLKey:      utils.PointerTo(filepath.Join(p.PostgresCertsDir, database.PostgresReplicatorKeyName)),
			SSLMode:     utils.PointerTo("verify-full"),
		},
	}
}

func (p *PatroniConfigGenerator) pgHba(systemAddresses []string, extraEntries []hba.Entry) *[]string {
	entries := []string{
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
	}

	// Reject connections for system users except for SSL connections from the
	// given system addresses.
	for _, address := range systemAddresses {
		entries = append(entries,
			hba.Entry{
				Type:        hba.EntryTypeHostSSL,
				Database:    "all",
				User:        "pgedge,patroni_replicator",
				Address:     address,
				AuthMethod:  hba.AuthMethodCert,
				AuthOptions: "clientcert=verify-full",
			}.String(),
			hba.Entry{
				Type:        hba.EntryTypeHostSSL,
				Database:    "replication",
				User:        "pgedge,patroni_replicator",
				Address:     address,
				AuthMethod:  hba.AuthMethodCert,
				AuthOptions: "clientcert=verify-full",
			}.String(),
		)
	}
	entries = append(entries,
		hba.Entry{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "pgedge,patroni_replicator",
			Address:    "0.0.0.0/0",
			AuthMethod: hba.AuthMethodReject,
		}.String(),
		hba.Entry{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "pgedge,patroni_replicator",
			Address:    "::/0",
			AuthMethod: hba.AuthMethodReject,
		}.String(),
	)

	for _, entry := range extraEntries {
		entries = append(entries, entry.String())
	}

	// Use MD5 for non-system users from all other connections
	// TODO: Can we safely upgrade this to scram-sha-256?
	entries = append(entries,
		hba.Entry{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "all",
			Address:    "0.0.0.0/0",
			AuthMethod: hba.AuthMethodMD5,
		}.String(),
		hba.Entry{
			Type:       hba.EntryTypeHost,
			Database:   "all",
			User:       "all",
			Address:    "::/0",
			AuthMethod: hba.AuthMethodMD5,
		}.String(),
	)

	return &entries
}
