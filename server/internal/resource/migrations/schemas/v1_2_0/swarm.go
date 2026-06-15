// produced by schematool f1eff819726b8c2319bc80eca945b80ea811b8b5 server/internal/orchestrator/swarm PatroniConfig Network
package v1_2_0

import (
	"github.com/pgEdge/control-plane/server/internal/resource"
	"net/netip"
)

const ResourceTypePatroniConfig resource.Type = "swarm.patroni_config"

func PatroniConfigIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniConfig,
	}
}

type PatroniConfig struct {
	DatabaseID string `json:"database_id"`
	Base       *struct {
		InstanceID string `json:"instance_id"`
		HostID     string `json:"host_id"`
		NodeName   string `json:"node_name"`
		Generator  *struct {
			ArchiveCommand         string         `json:"archive_command,omitempty"`
			ClusterSize            int            `json:"cluster_size"`
			CPUs                   float64        `json:"cpus,omitempty"`
			DatabaseID             string         `json:"database_id"`
			DataDir                string         `json:"data_dir"`
			EtcdCertsDir           string         `json:"etcd_certs_dir"`
			FQDN                   string         `json:"fqdn"`
			InstanceID             string         `json:"instance_id"`
			LogType                string         `json:"log_type"`
			MemoryBytes            uint64         `json:"memory_bytes,omitempty"`
			NodeName               string         `json:"node_name"`
			NodeOrdinal            int            `json:"node_ordinal"`
			NodeSize               int            `json:"node_size"`
			OrchestratorParameters map[string]any `json:"orchestrator_parameters,omitempty"`
			PatroniAllowlist       []string       `json:"patroni_allowlist"`
			PatroniPort            int            `json:"patroni_port"`
			PgHbaConf              []string       `json:"pg_hba_conf,omitempty"`
			PgIdentConf            []string       `json:"pg_ident_conf,omitempty"`
			PostgresCertsDir       string         `json:"postgres_certs_dir"`
			PostgresPort           int            `json:"postgres_port"`
			RestoreCommand         string         `json:"restore_command"`
			SpecParameters         map[string]any `json:"spec_parameters,omitempty"`
			TenantID               *string        `json:"tenant_id,omitempty"`
		} `json:"generator"`
		ParentID string `json:"parent_id"`
		OwnerUID int    `json:"owner_uid"`
		OwnerGID int    `json:"owner_gid"`
	} `json:"base"`
	BridgeNetworkInfo *struct {
		Name    string       `json:"name"`
		ID      string       `json:"id"`
		Subnet  netip.Prefix `json:"subnet"`
		Gateway netip.Addr   `json:"gateway"`
	} `json:"host_network_info"`
	DatabaseNetworkName string `json:"database_network_name"`
}

const ResourceTypeNetwork resource.Type = "swarm.network"

func NetworkResourceIdentifier(name string) resource.Identifier {
	return resource.Identifier{
		ID:   name,
		Type: ResourceTypeNetwork,
	}
}

type Network struct {
	Scope     string `json:"scope"`
	Driver    string `json:"driver"`
	Allocator struct {
		Prefix netip.Prefix `json:"prefix"`
		Bits   int          `json:"bits"`
	} `json:"allocator"`
	Name      string       `json:"name"`
	NetworkID string       `json:"network_id"`
	Subnet    netip.Prefix `json:"subnet"`
	Gateway   netip.Addr   `json:"gateway"`
}
