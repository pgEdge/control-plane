// produced by schematool 20b82249f8734cd7aa4ed88b2a0e60a68c7bf058 server/internal/orchestrator/common EtcdCreds PatroniCluster PatroniMember PgBackRestConfig PgBackRestStanza PostgresCerts
package v1_2_0

import (
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypeEtcdCreds resource.Type = "common.etcd_creds"

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
	ClientCert []byte `json:"client_cert"`
	ClientKey  []byte `json:"client_key"`
}

const ResourceTypePatroniCluster resource.Type = "common.patroni_cluster"

func PatroniClusterResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePatroniCluster,
	}
}

type PatroniCluster struct {
	DatabaseID string `json:"database_id"`
	NodeName   string `json:"node_name"`
}

const ResourceTypePatroniMember resource.Type = "common.patroni_member"

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

const ResourceTypePgBackRestConfig resource.Type = "common.pgbackrest_config"

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
	Paths    struct {
		Instance struct {
			BaseDir string `json:"base_dir"`
		} `json:"instance"`
		Host struct {
			BaseDir string `json:"base_dir"`
		} `json:"host"`
		PgBackRestPath string `json:"pg_backrest_path"`
		PatroniPath    string `json:"patroni_path"`
	} `json:"paths"`
	Port int `json:"port"`
}

const ResourceTypePgBackRestStanza resource.Type = "common.pgbackrest_stanza"

func PgBackRestStanzaIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePgBackRestStanza,
	}
}

type PgBackRestStanza struct {
	DatabaseID string `json:"database_id"`
	NodeName   string `json:"node_name"`
}

const ResourceTypePostgresCerts resource.Type = "common.postgres_certs"

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
