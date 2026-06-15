package migrations_test

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v1_1_0"
	"github.com/stretchr/testify/require"
)

func TestVersion_1_2_0(t *testing.T) {
	databaseID := "database-1"

	for _, tc := range []struct {
		name string
		in   []*resource.ResourceData
	}{
		{
			name: "simple instance",
			in: []*resource.ResourceData{
				v1_1_0_node(t, "n1", "instance-1"),
				v1_1_0_instance(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_etcdCreds(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_patroniCluster(t, databaseID, "n1"),
				v1_1_0_patroniMember(t, databaseID, "instance-1", "n1"),
				v1_1_0_postgresCerts(t, "instance-1", "host-1"),
				v1_1_0_patroniConfig(t, databaseID, "instance-1", "host-1", "n1", false, false, false),
				v1_1_0_configsDir(t, "host-1"),
				v1_1_0_network(t, databaseID),
			},
		},
		{
			name: "with backup config",
			in: []*resource.ResourceData{
				v1_1_0_node(t, "n1", "instance-1"),
				v1_1_0_instance(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_etcdCreds(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_patroniCluster(t, databaseID, "n1"),
				v1_1_0_patroniMember(t, databaseID, "instance-1", "n1"),
				v1_1_0_postgresCerts(t, "instance-1", "host-1"),
				v1_1_0_patroniConfig(t, databaseID, "instance-1", "host-1", "n1", true, false, false),
				v1_1_0_pgBackRestConfig(t, databaseID, "instance-1", "host-1", "n1", pgbackrest.ConfigTypeBackup),
				v1_1_0_pgBackRestStanza(t, "n1"),
				v1_1_0_configsDir(t, "host-1"),
				v1_1_0_network(t, databaseID),
			},
		},
		{
			name: "with restore config",
			in: []*resource.ResourceData{
				v1_1_0_node(t, "n1", "instance-1"),
				v1_1_0_instance(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_etcdCreds(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_patroniCluster(t, databaseID, "n1"),
				v1_1_0_patroniMember(t, databaseID, "instance-1", "n1"),
				v1_1_0_postgresCerts(t, "instance-1", "host-1"),
				v1_1_0_patroniConfig(t, databaseID, "instance-1", "host-1", "n1", false, true, false),
				v1_1_0_pgBackRestConfig(t, databaseID, "instance-1", "host-1", "n1", pgbackrest.ConfigTypeRestore),
				v1_1_0_pgBackRestStanza(t, "n1"),
				v1_1_0_configsDir(t, "host-1"),
				v1_1_0_network(t, databaseID),
			},
		},
		{
			name: "with in-place restore config",
			in: []*resource.ResourceData{
				v1_1_0_node(t, "n1", "instance-1"),
				v1_1_0_instance(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_etcdCreds(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_patroniCluster(t, databaseID, "n1"),
				v1_1_0_patroniMember(t, databaseID, "instance-1", "n1"),
				v1_1_0_postgresCerts(t, "instance-1", "host-1"),
				v1_1_0_patroniConfig(t, databaseID, "instance-1", "host-1", "n1", false, true, true),
				v1_1_0_pgBackRestConfig(t, databaseID, "instance-1", "host-1", "n1", pgbackrest.ConfigTypeRestore),
				v1_1_0_pgBackRestStanza(t, "n1"),
				v1_1_0_configsDir(t, "host-1"),
				v1_1_0_network(t, databaseID),
			},
		},
		{
			name: "with backup and restore configs",
			in: []*resource.ResourceData{
				v1_1_0_node(t, "n1", "instance-1"),
				v1_1_0_instance(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_etcdCreds(t, databaseID, "instance-1", "host-1", "n1"),
				v1_1_0_patroniCluster(t, databaseID, "n1"),
				v1_1_0_patroniMember(t, databaseID, "instance-1", "n1"),
				v1_1_0_postgresCerts(t, "instance-1", "host-1"),
				v1_1_0_patroniConfig(t, databaseID, "instance-1", "host-1", "n1", true, true, false),
				v1_1_0_pgBackRestConfig(t, databaseID, "instance-1", "host-1", "n1", pgbackrest.ConfigTypeBackup),
				v1_1_0_pgBackRestConfig(t, databaseID, "instance-1", "host-1", "n1", pgbackrest.ConfigTypeRestore),
				v1_1_0_pgBackRestStanza(t, "n1"),
				v1_1_0_configsDir(t, "host-1"),
				v1_1_0_network(t, databaseID),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &resource.State{
				Version:   resource.StateVersion_1_1_0,
				Resources: map[resource.Type]map[string]*resource.ResourceData{},
			}
			state.Add(tc.in...)

			migration := &migrations.Version_1_2_0{}
			migration.Run(databaseID, state)
			state.Version = migration.Version()

			golden.Run(t, state, update)

			// Validate that the dependencies are correct.
			_, err := state.PlanRefresh()
			require.NoError(t, err)

			_, err = state.PlanAll(resource.PlanOptions{}, state)
			require.NoError(t, err)
		})
	}
}

func v1_1_0_node(t testing.TB, nodeName string, instanceIDs ...string) *resource.ResourceData {
	deps := make([]resource.Identifier, len(instanceIDs))
	for i, id := range instanceIDs {
		deps[i] = v1_1_0.InstanceResourceIdentifier(id)
	}

	return &resource.ResourceData{
		Executor:        resource.AnyExecutor(),
		Identifier:      v1_1_0.NodeResourceIdentifier(nodeName),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v1_1_0.NodeResource{
			Name:              nodeName,
			InstanceIDs:       instanceIDs,
			PrimaryInstanceID: instanceIDs[0],
		}),
		Dependencies: deps,
	}
}

func v1_1_0_instance(t testing.TB, databaseID, instanceID, hostID, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.InstanceResourceIdentifier(instanceID),
		ResourceVersion: "1",
		DiffIgnore: []string{
			"/primary_instance_id",
			"/connection_info",
		},
		// We don't need every property here. The instance isn't part of the
		// migration, we just need it for the dependencies in our test plan.
		Attributes: mustJSON(t, map[string]any{
			"spec": map[string]any{
				"database_id":   databaseID,
				"instance_id":   instanceID,
				"database_name": "test",
				"host_id":       hostID,
				"node_name":     nodeName,
				"database_users": []map[string]any{
					{
						"username": "admin",
						"db_owner": true,
					},
				},
			},
		}),
	}
}

func v1_1_0_etcdCreds(t testing.TB, databaseID, instanceID, hostID, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.EtcdCredsIdentifier(instanceID),
		ResourceVersion: "1",
		DiffIgnore: []string{
			"/username",
			"/password",
			"/ca_cert",
			"/client_cert",
			"/client_key",
		},
		Attributes: mustJSON(t, v1_1_0.EtcdCreds{
			InstanceID: instanceID,
			DatabaseID: databaseID,
			HostID:     hostID,
			NodeName:   nodeName,
			ParentID:   v1_1_0_configsDirID(hostID),
			OwnerUID:   123,
			OwnerGID:   124,
			Username:   "username",
			Password:   "password",
			CaCert:     []byte("ca_cert"),
			ClientCert: []byte("client_cert"),
			ClientKey:  []byte("client_key"),
		}),
		Dependencies: []resource.Identifier{
			v1_1_0.DirResourceIdentifier(v1_1_0_configsDirID(hostID)),
		},
	}
}

func v1_1_0_patroniCluster(t testing.TB, databaseID, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.AnyExecutor(),
		Identifier:      v1_1_0.PatroniClusterResourceIdentifier(nodeName),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.PatroniCluster{
			DatabaseID:           databaseID,
			PatroniClusterPrefix: "/patroni/" + databaseID + ":" + nodeName,
			NodeName:             nodeName,
		}),
	}
}

func v1_1_0_patroniMember(t testing.TB, databaseID, instanceID, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.AnyExecutor(),
		Identifier:      v1_1_0.PatroniMemberResourceIdentifier(nodeName),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.PatroniMember{
			DatabaseID: databaseID,
			InstanceID: instanceID,
			NodeName:   nodeName,
		}),
		Dependencies: []resource.Identifier{
			v1_1_0.PatroniClusterResourceIdentifier(nodeName),
		},
	}
}

func v1_1_0_pgBackRestConfig(t testing.TB, databaseID, instanceID, hostID, nodeName string, configType pgbackrest.ConfigType) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.PgBackRestConfigIdentifier(instanceID, configType),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.PgBackRestConfig{
			InstanceID: instanceID,
			HostID:     hostID,
			DatabaseID: databaseID,
			NodeName:   nodeName,
			ParentID:   v1_1_0_configsDirID(hostID),
			Type:       configType.String(),
			OwnerUID:   123,
			OwnerGID:   124,
			Repositories: []*struct {
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
			}{
				{
					ID:         "default",
					Type:       "s3",
					S3Bucket:   "bucket",
					S3Region:   "us-east-1",
					S3Endpoint: "s3.us-east-1.amazonaws.com",
				},
			},
		}),
		Dependencies: []resource.Identifier{
			v1_1_0.DirResourceIdentifier(v1_1_0_configsDirID(hostID)),
		},
	}
}

func v1_1_0_pgBackRestStanza(t testing.TB, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(nodeName),
		Identifier:      v1_1_0.PgBackRestStanzaIdentifier(nodeName),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.PgBackRestStanza{
			NodeName: nodeName,
		}),
		Dependencies: []resource.Identifier{
			v1_1_0.NodeResourceIdentifier(nodeName),
		},
	}
}

func v1_1_0_postgresCerts(t testing.TB, instanceID, hostID string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.PostgresCertsIdentifier(instanceID),
		ResourceVersion: "1",
		DiffIgnore: []string{
			"/ca_cert",
			"/server_cert",
			"/server_key",
			"/superuser_cert",
			"/superuser_key",
			"/replication_cert",
			"/replication_key",
		},
		Attributes: mustJSON(t, v1_1_0.PostgresCerts{
			InstanceID:        instanceID,
			HostID:            hostID,
			InstanceAddresses: []string{"127.0.0.1", "localhost"},
			ParentID:          v1_1_0_configsDirID(hostID),
			OwnerUID:          123,
			OwnerGID:          124,
			CaCert:            []byte("ca_cert"),
			ServerCert:        []byte("server_cert"),
			ServerKey:         []byte("server_key"),
			SuperuserCert:     []byte("superuser_cert"),
			SuperuserKey:      []byte("superuser_key"),
			ReplicationCert:   []byte("replication_cert"),
			ReplicationKey:    []byte("replication_key"),
		}),
		Dependencies: []resource.Identifier{
			v1_1_0.DirResourceIdentifier(v1_1_0_configsDirID(hostID)),
		},
	}
}

func v1_1_0_patroniConfig(t testing.TB, databaseID, instanceID, hostID, nodeName string, hasBackupConfig, hasRestoreConfig, inPlaceRestore bool) *resource.ResourceData {
	var extraDeps []resource.Identifier
	var backupConfig *struct {
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
	}
	if hasBackupConfig {
		extraDeps = append(extraDeps, v1_1_0.PgBackRestConfigIdentifier(instanceID, pgbackrest.ConfigTypeBackup))
		backupConfig = &struct {
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
		}{
			Repositories: []*struct {
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
			}{
				{
					ID:         "default",
					Type:       "s3",
					S3Bucket:   "bucket",
					S3Region:   "us-east-1",
					S3Endpoint: "s3.us-east-1.amazonaws.com",
				},
			},
		}
	}
	var restoreConfig *struct {
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
	}
	if hasRestoreConfig {
		extraDeps = append(extraDeps, v1_1_0.PgBackRestConfigIdentifier(instanceID, pgbackrest.ConfigTypeRestore))
		restoreConfig = &struct {
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
		}{
			SourceDatabaseID:   "old-database",
			SourceNodeName:     "n1",
			SourceDatabaseName: "source_database",
			RestoreOptions: map[string]string{
				"type":   "time",
				"target": "2026-01-01T01:30:00Z",
			},
			Repository: &struct {
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
			}{
				ID:         "default",
				Type:       "s3",
				S3Bucket:   "bucket",
				S3Region:   "us-east-1",
				S3Endpoint: "s3.us-east-1.amazonaws.com",
			},
		}
	}
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.PatroniConfigIdentifier(instanceID),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.PatroniConfig{
			HostCPUs:        4,
			HostMemoryBytes: 1024 * 1024 * 1024,
			ParentID:        v1_1_0_configsDirID(hostID),
			OwnerUID:        123,
			OwnerGID:        124,
			BridgeNetworkInfo: &struct {
				Name    string
				ID      string
				Subnet  netip.Prefix
				Gateway netip.Addr
			}{
				Name:    "bridge",
				ID:      "bridge_id",
				Subnet:  netip.MustParsePrefix("172.16.0.0/12"),
				Gateway: netip.MustParseAddr("172.16.0.1"),
			},
			DatabaseNetworkName: databaseID,
			InstanceHostname:    databaseID + "-" + nodeName,
			Spec: &struct {
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
			}{
				InstanceID:   instanceID,
				DatabaseID:   databaseID,
				HostID:       hostID,
				DatabaseName: "test_database",
				NodeName:     nodeName,
				NodeOrdinal:  1,
				CPUs:         4,
				MemoryBytes:  1024 * 1024 * 1024,
				ClusterSize:  3,
				PostgreSQLConf: map[string]any{
					"max_connections": 1000,
				},
				PgHbaConf: []string{
					"hostssl all myapp_user 203.0.113.0/24 scram-sha-256",
				},
				PgIdentConf: []string{
					"ssl_users  CN=alice,O=example  alice",
				},
				NodeSize:       1,
				InPlaceRestore: inPlaceRestore,
				BackupConfig:   backupConfig,
				RestoreConfig:  restoreConfig,
			},
		}),
		Dependencies: slices.Concat(
			[]resource.Identifier{
				v1_1_0.DirResourceIdentifier(v1_1_0_configsDirID(hostID)),
				v1_1_0.NetworkResourceIdentifier(databaseID),
				v1_1_0.EtcdCredsIdentifier(instanceID),
				v1_1_0.PatroniMemberResourceIdentifier(instanceID),
				v1_1_0.PatroniClusterResourceIdentifier(nodeName),
			},
			extraDeps,
		),
	}
}

func v1_1_0_network(t testing.TB, databaseID string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.ManagerExecutor(),
		Identifier:      v1_1_0.NetworkResourceIdentifier(databaseID),
		ResourceVersion: "1",
		// We don't need all properties for this test.
		Attributes: mustJSON(t, v1_1_0.Network{
			Scope:  "swarm",
			Driver: "overlay",
			Name:   databaseID,
		}),
	}
}

func v1_1_0_configsDir(t testing.TB, hostID string) *resource.ResourceData {
	id := v1_1_0_configsDirID(hostID)
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v1_1_0.DirResourceIdentifier(id),
		ResourceVersion: "1",
		DiffIgnore:      nil,
		Attributes: mustJSON(t, v1_1_0.DirResource{
			ID:   id,
			Path: "/configs",
		}),
	}
}

func v1_1_0_configsDirID(hostID string) string {
	return hostID + "-configs"
}
