// produced by schematool c2ccdd8969fbc7b26675a9ce183092ec9444d877 server/internal/orchestrator/swarm PatroniConfig EtcdCreds PatroniCluster PatroniMember PgBackRestConfig PgBackRestStanza PostgresCerts Network
package v1_1_0

import (
	"net/netip"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypePatroniConfig resource.Type = "swarm.patroni_config"

func PatroniConfigIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniConfig,
	}
}

type PatroniConfig struct {
	Spec *struct {
		InstanceID    string            `json:"instance_id"`
		TenantID      *string           `json:"tenant_id,omitempty"`
		DatabaseID    string            `json:"database_id"`
		HostID        string            `json:"host_id"`
		DatabaseName  string            `json:"database_name"`
		NodeName      string            `json:"node_name"`
		NodeOrdinal   int               `json:"node_ordinal"`
		PgEdgeVersion *ds.PgEdgeVersion `json:"pg_edge_version"`
		Port          *int              `json:"port"`
		PatroniPort   *int              `json:"patroni_port"`
		CPUs          float64           `json:"cpus"`
		MemoryBytes   uint64            `json:"memory"`
		DatabaseUsers []*struct {
			Username   string   `json:"username"`
			Password   string   `json:"password"`
			DBOwner    bool     `json:"db_owner,omitempty"`
			Attributes []string `json:"attributes,omitempty"`
			Roles      []string `json:"roles,omitempty"`
		} `json:"database_users"`
		BackupConfig *struct {
			Repositories []*struct {
				ID                string            `json:"id"`
				Type              string            `json:"type"`
				S3Bucket          string            `json:"s3_bucket,omitempty"`
				S3Region          string            `json:"s3_region,omitempty"`
				S3Endpoint        string            `json:"s3_endpoint,omitempty"`
				S3Key             string            `json:"s3_key,omitempty"`
				S3KeySecret       string            `json:"s3_key_secret,omitempty"`
				GCSBucket         string            `json:"gcs_bucket,omitempty"`
				GCSEndpoint       string            `json:"gcs_endpoint,omitempty"`
				GCSKey            string            `json:"gcs_key,omitempty"`
				AzureAccount      string            `json:"azure_account,omitempty"`
				AzureContainer    string            `json:"azure_container,omitempty"`
				AzureEndpoint     string            `json:"azure_endpoint,omitempty"`
				AzureKey          string            `json:"azure_key,omitempty"`
				RetentionFull     int               `json:"retention_full"`
				RetentionFullType string            `json:"retention_full_type"`
				BasePath          string            `json:"base_path,omitempty"`
				CustomOptions     map[string]string `json:"custom_options,omitempty"`
			} `json:"repositories"`
			Schedules []*struct {
				ID             string `json:"id"`
				Type           string `json:"type"`
				CronExpression string `json:"cron_expression"`
			} `json:"schedules"`
		} `json:"backup_config"`
		RestoreConfig *struct {
			SourceDatabaseID   string `json:"source_database_id"`
			SourceNodeName     string `json:"source_node_name"`
			SourceDatabaseName string `json:"source_database_name"`
			Repository         *struct {
				ID                string            `json:"id"`
				Type              string            `json:"type"`
				S3Bucket          string            `json:"s3_bucket,omitempty"`
				S3Region          string            `json:"s3_region,omitempty"`
				S3Endpoint        string            `json:"s3_endpoint,omitempty"`
				S3Key             string            `json:"s3_key,omitempty"`
				S3KeySecret       string            `json:"s3_key_secret,omitempty"`
				GCSBucket         string            `json:"gcs_bucket,omitempty"`
				GCSEndpoint       string            `json:"gcs_endpoint,omitempty"`
				GCSKey            string            `json:"gcs_key,omitempty"`
				AzureAccount      string            `json:"azure_account,omitempty"`
				AzureContainer    string            `json:"azure_container,omitempty"`
				AzureEndpoint     string            `json:"azure_endpoint,omitempty"`
				AzureKey          string            `json:"azure_key,omitempty"`
				RetentionFull     int               `json:"retention_full"`
				RetentionFullType string            `json:"retention_full_type"`
				BasePath          string            `json:"base_path,omitempty"`
				CustomOptions     map[string]string `json:"custom_options,omitempty"`
			} `json:"repository"`
			RestoreOptions map[string]string `json:"restore_options"`
		} `json:"restore_config"`
		PostgreSQLConf   map[string]any `json:"postgresql_conf"`
		PgHbaConf        []string       `json:"pg_hba_conf,omitempty"`
		PgIdentConf      []string       `json:"pg_ident_conf,omitempty"`
		ClusterSize      int            `json:"cluster_size"`
		NodeSize         int            `json:"node_size"`
		OrchestratorOpts *struct {
			Swarm *struct {
				ExtraVolumes []struct {
					HostPath        string `json:"host_path"`
					DestinationPath string `json:"destination_path"`
				} `json:"extra_volumes,omitempty"`
				ExtraNetworks []struct {
					ID         string            `json:"id"`
					Aliases    []string          `json:"aliases,omitempty"`
					DriverOpts map[string]string `json:"driver_opts,omitempty"`
				} `json:"extra_networks,omitempty"`
				ExtraLabels map[string]string `json:"extra_labels,omitempty"`
			} `json:"docker,omitempty"`
		} `json:"orchestrator_opts,omitempty"`
		InPlaceRestore bool     `json:"in_place_restore,omitempty"`
		AllHostIDs     []string `json:"all_host_ids"`
	} `json:"spec"`
	ParentID          string  `json:"parent_id"`
	HostCPUs          float64 `json:"host_cpus"`
	HostMemoryBytes   uint64  `json:"host_memory_bytes"`
	BridgeNetworkInfo *struct {
		Name    string
		ID      string
		Subnet  netip.Prefix
		Gateway netip.Addr
	} `json:"host_network_info"`
	DatabaseNetworkName string `json:"database_network_name"`
	OwnerUID            int    `json:"owner_uid"`
	OwnerGID            int    `json:"owner_gid"`
	InstanceHostname    string `json:"instance_hostname"`
}

const ResourceTypeEtcdCreds resource.Type = "swarm.etcd_creds"

func EtcdCredsIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeEtcdCreds,
	}
}

type EtcdCreds struct {
	InstanceID string `json:"instance_id"`
	DatabaseID string `json:"database_id"`
	HostID     string `json:"host_id"`
	NodeName   string `json:"node_name"`
	ParentID   string `json:"parent_id"`
	OwnerUID   int    `json:"owner_uid"`
	OwnerGID   int    `json:"owner_gid"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	CaCert     []byte `json:"ca_cert"`
	ClientCert []byte `json:"server_cert"`
	ClientKey  []byte `json:"server_key"`
}

const ResourceTypePatroniCluster resource.Type = "swarm.patroni_cluster"

func PatroniClusterResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePatroniCluster,
	}
}

type PatroniCluster struct {
	DatabaseID           string `json:"database_id"`
	NodeName             string `json:"node_name"`
	PatroniClusterPrefix string `json:"patroni_namespace"`
}

const ResourceTypePatroniMember resource.Type = "swarm.patroni_member"

func PatroniMemberResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePatroniMember,
	}
}

type PatroniMember struct {
	DatabaseID string `json:"database_id"`
	NodeName   string `json:"node_name"`
	InstanceID string `json:"instance_id"`
}

const ResourceTypePgBackRestConfig resource.Type = "swarm.pgbackrest_config"

func PgBackRestConfigIdentifier(instanceID string, configType pgbackrest.ConfigType) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID + "-" + configType.String(),
		Type: ResourceTypePgBackRestConfig,
	}
}

type PgBackRestConfig struct {
	InstanceID   string `json:"instance_id"`
	HostID       string `json:"host_id"`
	DatabaseID   string `json:"database_id"`
	NodeName     string `json:"node_name"`
	Repositories []*struct {
		ID                string            `json:"id"`
		Type              string            `json:"type"`
		S3Bucket          string            `json:"s3_bucket,omitempty"`
		S3Region          string            `json:"s3_region,omitempty"`
		S3Endpoint        string            `json:"s3_endpoint,omitempty"`
		S3Key             string            `json:"s3_key,omitempty"`
		S3KeySecret       string            `json:"s3_key_secret,omitempty"`
		GCSBucket         string            `json:"gcs_bucket,omitempty"`
		GCSEndpoint       string            `json:"gcs_endpoint,omitempty"`
		GCSKey            string            `json:"gcs_key,omitempty"`
		AzureAccount      string            `json:"azure_account,omitempty"`
		AzureContainer    string            `json:"azure_container,omitempty"`
		AzureEndpoint     string            `json:"azure_endpoint,omitempty"`
		AzureKey          string            `json:"azure_key,omitempty"`
		RetentionFull     int               `json:"retention_full"`
		RetentionFullType string            `json:"retention_full_type"`
		BasePath          string            `json:"base_path,omitempty"`
		CustomOptions     map[string]string `json:"custom_options,omitempty"`
	} `json:"repositories"`
	ParentID string `json:"parent_id"`
	Type     string `json:"type"`
	OwnerUID int    `json:"owner_uid"`
	OwnerGID int    `json:"owner_gid"`
}

const ResourceTypePgBackRestStanza resource.Type = "swarm.pgbackrest_stanza"

func PgBackRestStanzaIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePgBackRestStanza,
	}
}

type PgBackRestStanza struct {
	NodeName string `json:"node_name"`
}

const ResourceTypePostgresCerts resource.Type = "swarm.postgres_certs"

func PostgresCertsIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePostgresCerts,
	}
}

type PostgresCerts struct {
	InstanceID        string   `json:"instance_id"`
	HostID            string   `json:"host_id"`
	InstanceAddresses []string `json:"instance_addresses"`
	ParentID          string   `json:"parent_id"`
	OwnerUID          int      `json:"owner_uid"`
	OwnerGID          int      `json:"owner_gid"`
	CaCert            []byte   `json:"ca_cert"`
	ServerCert        []byte   `json:"server_cert"`
	ServerKey         []byte   `json:"server_key"`
	SuperuserCert     []byte   `json:"superuser_cert"`
	SuperuserKey      []byte   `json:"superuser_key"`
	ReplicationCert   []byte   `json:"replication_cert"`
	ReplicationKey    []byte   `json:"replication_key"`
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
