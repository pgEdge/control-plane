// produced by schematool ceff237ed3d249af64e61b8343cc37c859f17ac2 server/internal/database InstanceResource LagTrackerCommitTimestampResource PostgresDatabaseResource ReplicationSlotAdvanceFromCTSResource ReplicationSlotCreateResource ReplicationSlotResource SubscriptionResource SyncEventResource WaitForSyncEventResource
package v1_0_0

import (
	"fmt"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"time"
)

const ResourceTypeInstance resource.Type = "database.instance"

func InstanceResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeInstance,
	}
}

type InstanceResource struct {
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
		ClusterSize      int            `json:"cluster_size"`
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
		InPlaceRestore bool `json:"in_place_restore,omitempty"`
	} `json:"spec"`
	InstanceHostname         string `json:"instance_hostname"`
	PrimaryInstanceID        string `json:"primary_instance_id"`
	OrchestratorDependencies []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"dependencies"`
	ConnectionInfo *struct {
		AdminHost        string
		AdminPort        int
		PeerHost         string
		PeerPort         int
		PeerSSLCert      string
		PeerSSLKey       string
		PeerSSLRootCert  string
		PatroniPort      int
		ClientAddresses  []string
		ClientPort       int
		InstanceHostname string
	} `json:"connection_info"`
}

const ResourceTypeLagTrackerCommitTS resource.Type = "database.lag_tracker_commit_ts"

func LagTrackerCommitTSIdentifier(originNode, receiverNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeLagTrackerCommitTS,
		ID:   fmt.Sprintf("%s:%s:%s", originNode, receiverNode, databaseName),
	}
}

type LagTrackerCommitTimestampResource struct {
	OriginNode        string `json:"origin_node"`
	ReceiverNode      string `json:"receiver_node"`
	DatabaseName      string `json:"database_name"`
	ExtraDependencies []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"dependent_resources,omitempty"`
	CommitTimestamp *time.Time `json:"commit_timestamp,omitempty"`
}

const ResourceTypePostgresDatabase resource.Type = "database.postgres_database"

func PostgresDatabaseResourceIdentifier(nodeName, dbName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypePostgresDatabase,
		ID:   fmt.Sprintf("%s:%s", nodeName, dbName),
	}
}

type PostgresDatabaseResource struct {
	NodeName          string `json:"node_name"`
	DatabaseName      string `json:"database_name"`
	Owner             string `json:"owner"`
	RenameFrom        string `json:"rename_from"`
	HasRestoreConfig  bool   `json:"has_restore_config"`
	ExtraDependencies []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"extra_dependencies"`
}

const ResourceTypeReplicationSlotAdvanceFromCTS resource.Type = "database.replication_slot_advance_from_cts"

func ReplicationSlotAdvanceFromCTSResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationSlotAdvanceFromCTS,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type ReplicationSlotAdvanceFromCTSResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

const ResourceTypeReplicationSlotCreate resource.Type = "database.replication_slot_create"

func ReplicationSlotCreateResourceIdentifier(databaseName, providerNode, subscriberNode string) resource.Identifier {
	return resource.Identifier{
		ID:   postgres.ReplicationSlotName(databaseName, providerNode, subscriberNode),
		Type: ResourceTypeReplicationSlotCreate,
	}
}

type ReplicationSlotCreateResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

const ResourceTypeReplicationSlot resource.Type = "database.replication_slot"

func ReplicationSlotResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationSlot,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type ReplicationSlotResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

const ResourceTypeSubscription resource.Type = "database.subscription"

func SubscriptionResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeSubscription,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type SubscriptionResource struct {
	DatabaseName      string `json:"database_name"`
	SubscriberNode    string `json:"subscriber_node"`
	ProviderNode      string `json:"provider_node"`
	Disabled          bool   `json:"disabled"`
	SyncStructure     bool   `json:"sync_structure"`
	SyncData          bool   `json:"sync_data"`
	ExtraDependencies []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"extra_dependencies"`
	NeedsUpdate bool `json:"needs_update"`
}

const ResourceTypeSyncEvent resource.Type = "database.sync_event"

func SyncEventResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeSyncEvent,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type SyncEventResource struct {
	DatabaseName      string `json:"database_name"`
	ProviderNode      string `json:"provider_node"`
	SubscriberNode    string `json:"subscriber_node"`
	SyncEventLsn      string `json:"sync_event_lsn"`
	ExtraDependencies []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"extra_dependencies"`
}

const ResourceTypeWaitForSyncEvent resource.Type = "database.wait_for_sync_event"

func WaitForSyncEventResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeWaitForSyncEvent,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type WaitForSyncEventResource struct {
	DatabaseName   string `json:"database_name"`
	SubscriberNode string `json:"subscriber_node"`
	ProviderNode   string `json:"provider_node"`
}
