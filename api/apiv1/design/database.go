package design

import (
	g "goa.design/goa/v3/dsl"
)

var postgresVersions = []any{"15", "16", "17"}
var spockVersions = []any{"5"}

const (
	nodeNamePattern = `n[0-9]+`
	cpuPattern      = `^[0-9]+(\.[0-9]{1,3}|m)?$`
)

var HostIDs = g.ArrayOf(Identifier, func() {
	g.Example("host-1")
	g.Example("us-east-1")
	g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
})

var DatabaseNodeSpec = g.Type("DatabaseNodeSpec", func() {
	g.Attribute("name", g.String, func() {
		g.Description("The name of the database node.")
		g.Pattern(nodeNamePattern)
		g.Example("n1")
	})
	g.Attribute("host_ids", HostIDs, func() {
		g.Description("The IDs of the hosts that should run this node. When multiple hosts are specified, one host will chosen as a primary, and the others will be read replicas.")
		g.MinLength(1)
	})
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The major version of Postgres for this node. Overrides the Postgres version set in the DatabaseSpec.")
		g.Enum(postgresVersions...)
		g.Example("17")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database for this node. Overrides the Postgres port set in the DatabaseSpec.")
		g.Minimum(0)
		g.Maximum(65535)
		g.Example(5432)
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database on this node and to use for tuning Postgres. It can include the SI suffix 'm', e.g. '500m' for 500 millicpus. Cannot allocate units smaller than 1m. Defaults to the number of available CPUs on the host if 0 or unspecified. Cannot allocate more CPUs than are available on the host. Whether this limit is enforced depends on the orchestrator.")
		g.Pattern(cpuPattern)
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database on this node and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit is enforced depends on the orchestrator.")
		g.MaxLength(16)
		g.Example("16GiB")
		g.Example("500M")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings for this particular node. Will be merged with the settings provided by control-plane.")
		g.Example(map[string]any{
			"max_connections": 1000,
		})
	})
	g.Attribute("backup_config", BackupConfigSpec, func() {
		g.Description("The backup configuration for this node. Overrides the backup configuration set in the DatabaseSpec.")
	})
	g.Attribute("restore_config", RestoreConfigSpec, func() {
		g.Description("The restore configuration for this node. Overrides the restore configuration set in the DatabaseSpec.")
	})
	g.Attribute("orchestrator_opts", OrchestratorOpts, func() {
		g.Description("Orchestrator-specific configuration options.")
	})

	g.Required("name", "host_ids")
})

var DatabaseUserSpec = g.Type("DatabaseUserSpec", func() {
	g.Attribute("username", g.String, func() {
		g.Description("The username for this database user.")
		g.Example("admin")
		g.MinLength(1)
	})
	g.Attribute("password", g.String, func() {
		g.Description("The password for this database user. This field will be excluded from the response of all endpoints. It can also be omitted from update requests to keep the current value.")
		g.Example("secret")
		g.MinLength(1)
	})
	g.Attribute("db_owner", g.Boolean, func() {
		g.Description("If true, this user will be granted database ownership.")
	})
	g.Attribute("attributes", g.ArrayOf(g.String), func() {
		g.Description("The attributes to assign to this database user.")
		g.MaxLength(16)
		g.Example([]string{"LOGIN", "SUPERUSER"})
		g.Example([]string{"LOGIN", "CREATEDB", "CREATEROLE"})
	})
	g.Attribute("roles", g.ArrayOf(g.String), func() {
		g.Description("The roles to assign to this database user.")
		g.MaxLength(16)
		g.Example([]string{"application"})
		g.Example([]string{"application_read_only"})
		g.Example([]string{"pgedge_superuser"})
	})

	g.Required("username")
})

var BackupRepositorySpec = g.Type("BackupRepositorySpec", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("The unique identifier of this repository.")
		g.Example("my-app-1")
		g.Example("f6b84a99-5e91-4203-be1e-131fe82e5984")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of this repository.")
		g.Enum("s3", "gcs", "azure", "posix", "cifs")
		g.Example("s3")
	})
	g.Attribute("s3_bucket", g.String, func() {
		g.Description("The S3 bucket name for this repository. Only applies when type = 's3'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("s3_region", g.String, func() {
		g.Description("The region of the S3 bucket for this repository. Only applies when type = 's3'.")
		g.MinLength(1)
		g.MaxLength(32)
		g.Example("us-east-1")
	})
	g.Attribute("s3_endpoint", g.String, func() {
		g.Description("The optional S3 endpoint for this repository. Only applies when type = 's3'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("s3.us-east-1.amazonaws.com")
	})
	g.Attribute("s3_key", g.String, func() {
		g.Description("An optional AWS access key ID to use for this repository. If not provided, pgbackrest will use the default credential provider chain. This field will be excluded from the response of all endpoints. It can also be omitted from update requests to keep the current value.")
		g.MinLength(16)
		g.MaxLength(128)
		g.Example("AKIAIOSFODNN7EXAMPLE")
	})
	g.Attribute("s3_key_secret", g.String, func() {
		g.Description("The corresponding secret for the AWS access key ID in s3_key. This field will be excluded from the response of all endpoints. It can also be omitted from update requests to keep the current value.")
		g.MaxLength(128)
		g.Example("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	})
	g.Attribute("gcs_bucket", g.String, func() {
		g.Description("The GCS bucket name for this repository. Only applies when type = 'gcs'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("gcs_endpoint", g.String, func() {
		g.Description("The optional GCS endpoint for this repository. Only applies when type = 'gcs'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("localhost")
	})
	g.Attribute("gcs_key", g.String, func() {
		g.Description("Optional base64-encoded private key data. If omitted, pgbackrest will use the service account attached to the instance profile. This field will be excluded from the response of all endpoints. It can also be omitted from update requests to keep the current value.")
		g.MaxLength(1024)
		g.Example("ZXhhbXBsZSBnY3Mga2V5Cg==")
	})
	g.Attribute("azure_account", g.String, func() {
		g.Description("The Azure account name for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(24)
		g.Example("pgedge-backups")
	})
	g.Attribute("azure_container", g.String, func() {
		g.Description("The Azure container name for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("azure_endpoint", g.String, func() {
		g.Description("The optional Azure endpoint for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("blob.core.usgovcloudapi.net")
	})
	g.Attribute("azure_key", g.String, func() {
		g.Description("The Azure storage account access key to use for this repository. This field will be excluded from the response of all endpoints. It can also be omitted from update requests to keep the current value.")
		g.MaxLength(128)
		g.Example("YXpLZXk=")
	})
	g.Attribute("retention_full", g.Int, func() {
		g.Description("The count of full backups to retain or the time to retain full backups.")
		g.Minimum(1)
		g.Maximum(9999999)
		g.Example(2)
	})
	g.Attribute("retention_full_type", g.String, func() {
		g.Description("The type of measure used for retention_full.")
		g.Enum("time", "count")
		g.Example("count")
	})
	g.Attribute("base_path", g.String, func() {
		g.Description("The base path within the repository to store backups. Required for type = 'posix' and 'cifs'.")
		g.MaxLength(256)
		g.Example("/backups")
	})
	g.Attribute("custom_options", g.MapOf(g.String, g.String), func() {
		g.Description("Additional options to apply to this repository.")
		g.Example(map[string]any{
			"storage-upload-chunk-size": "5MiB",
			"s3-kms-key-id":             "1234abcd-12ab-34cd-56ef-1234567890ab",
		})
	})

	g.Required("type")
})

var BackupScheduleSpec = g.Type("BackupScheduleSpec", func() {
	g.Attribute("id", g.String, func() {
		g.Description("The unique identifier for this backup schedule.")
		g.MaxLength(64)
		g.Example("daily-full-backup")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of backup to take on this schedule.")
		g.Enum("full", "incr")
		g.Example("full")
	})
	g.Attribute("cron_expression", g.String, func() {
		g.Description("The cron expression for this schedule.")
		g.MaxLength(32)
		g.Example("0 6 * * ?")
	})

	g.Required("id", "type", "cron_expression")
})

var BackupConfigSpec = g.Type("BackupConfigSpec", func() {
	g.Attribute("repositories", g.ArrayOf(BackupRepositorySpec), func() {
		g.Description("The repositories for this backup configuration.")
		g.MinLength(1)
	})
	g.Attribute("schedules", g.ArrayOf(BackupScheduleSpec), func() {
		g.Description("The schedules for this backup configuration.")
		g.MaxLength(32)
	})

	g.Required("repositories")
})

var RestoreRepositorySpec = g.Type("RestoreRepositorySpec", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("The unique identifier of this repository.")
		g.Example("my-app-1")
		g.Example("f6b84a99-5e91-4203-be1e-131fe82e5984")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of this repository.")
		g.Enum("s3", "gcs", "azure", "posix", "cifs")
		g.Example("s3")
	})
	g.Attribute("s3_bucket", g.String, func() {
		g.Description("The S3 bucket name for this repository. Only applies when type = 's3'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("s3_region", g.String, func() {
		g.Description("The region of the S3 bucket for this repository. Only applies when type = 's3'.")
		g.MinLength(1)
		g.MaxLength(32)
		g.Example("us-east-1")
	})
	g.Attribute("s3_endpoint", g.String, func() {
		g.Description("The optional S3 endpoint for this repository. Only applies when type = 's3'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("s3.us-east-1.amazonaws.com")
	})
	g.Attribute("s3_key", g.String, func() {
		g.Description("An optional AWS access key ID to use for this repository. If not provided, pgbackrest will use the default credential provider chain.")
		g.MinLength(16)
		g.MaxLength(128)
		g.Example("AKIAIOSFODNN7EXAMPLE")
	})
	g.Attribute("s3_key_secret", g.String, func() {
		g.Description("The corresponding secret for the AWS access key ID in s3_key.")
		g.MaxLength(128)
		g.Example("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	})
	g.Attribute("gcs_bucket", g.String, func() {
		g.Description("The GCS bucket name for this repository. Only applies when type = 'gcs'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("gcs_endpoint", g.String, func() {
		g.Description("The optional GCS endpoint for this repository. Only applies when type = 'gcs'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("localhost")
	})
	g.Attribute("gcs_key", g.String, func() {
		g.Description("Optional base64-encoded private key data. If omitted, pgbackrest will use the service account attached to the instance profile.")
		g.MaxLength(1024)
		g.Example("ZXhhbXBsZSBnY3Mga2V5Cg==")
	})
	g.Attribute("azure_account", g.String, func() {
		g.Description("The Azure account name for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(24)
		g.Example("pgedge-backups")
	})
	g.Attribute("azure_container", g.String, func() {
		g.Description("The Azure container name for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(63)
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("azure_endpoint", g.String, func() {
		g.Description("The optional Azure endpoint for this repository. Only applies when type = 'azure'.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("blob.core.usgovcloudapi.net")
	})
	g.Attribute("azure_key", g.String, func() {
		g.Description("An optional Azure storage account access key to use for this repository. If not provided, pgbackrest will use the VM's managed identity.")
		g.MaxLength(128)
		g.Example("YXpLZXk=")
	})
	g.Attribute("base_path", g.String, func() {
		g.Description("The base path within the repository to store backups. Required for type = 'posix' and 'cifs'.")
		g.MaxLength(256)
		g.Example("/backups")
	})
	g.Attribute("custom_options", g.MapOf(g.String, g.String), func() {
		g.Description("Additional options to apply to this repository.")
		g.Example(map[string]any{
			"s3-kms-key-id": "1234abcd-12ab-34cd-56ef-1234567890ab",
		})
	})

	g.Required("type")
})

var RestoreConfigSpec = g.Type("RestoreConfigSpec", func() {
	g.Attribute("source_database_id", Identifier, func() {
		g.Description("The ID of the database to restore this database from.")
		g.Example("production")
		g.Example("my-app")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("source_node_name", g.String, func() {
		g.Description("The name of the node to restore this database from.")
		g.Pattern(nodeNamePattern)
		g.Example("n1")
	})
	g.Attribute("source_database_name", g.String, func() {
		g.Description("The name of the database in this repository. The database will be renamed to the database_name in the DatabaseSpec after it's restored.")
		g.MinLength(1)
		g.MaxLength(31)
		g.Example("northwind")
	})
	g.Attribute("repository", RestoreRepositorySpec, func() {
		g.Description("The repository to restore this database from.")
	})
	g.Attribute("restore_options", g.MapOf(g.String, g.String), func() {
		g.Description("Additional options to use when restoring this database. If omitted, the database will be restored to the latest point in the given repository.")
		g.MaxLength(32)
		g.Example(map[string]string{
			"type":   "time",
			"target": "2025-01-01T01:30:00Z",
		})
		g.Example(map[string]string{
			"type":   "lsn",
			"target": "0/30000000",
		})
		g.Example(map[string]string{
			"set":    "20250505-153628F",
			"type":   "xid",
			"target": "123456",
		})
	})

	g.Required("source_database_id", "source_node_name", "source_database_name", "repository")
})

var DatabaseSpec = g.Type("DatabaseSpec", func() {
	g.Attribute("database_name", g.String, func() {
		g.Description("The name of the Postgres database.")
		g.MinLength(1)
		g.MaxLength(31)
		g.Example("northwind")
	})
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The major version of the Postgres database.")
		g.Enum(postgresVersions...)
		g.Example("17")
	})
	g.Attribute("spock_version", g.String, func() {
		g.Description("The major version of the Spock extension.")
		g.Enum(spockVersions...)
		g.Example("5")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database. If the port is 0, each instance will be assigned a random port. If the port is unspecified, the database will not be exposed on any port, dependent on orchestrator support for that feature.")
		g.Minimum(0)
		g.Maximum(65535)
		g.Example(5432)
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database and to use for tuning Postgres. Defaults to the number of available CPUs on the host. Can include an SI suffix, e.g. '500m' for 500 millicpus. Whether this limit is enforced depends on the orchestrator.")
		g.Pattern(cpuPattern)
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit is enforced depends on the orchestrator.")
		g.MaxLength(16)
		g.Example("16GiB")
		g.Example("500M")
	})
	g.Attribute("nodes", g.ArrayOf(DatabaseNodeSpec), func() {
		g.Description("The Spock nodes for this database.")
		g.MinLength(1)
		g.MaxLength(9)
	})
	g.Attribute("database_users", g.ArrayOf(DatabaseUserSpec), func() {
		g.Description("The users to create for this database.")
		g.MaxLength(16)
	})
	g.Attribute("backup_config", BackupConfigSpec, func() {
		g.Description("The backup configuration for this database.")
	})
	g.Attribute("restore_config", RestoreConfigSpec, func() {
		g.Description("The restore configuration for this database.")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings. Will be merged with the settings provided by control-plane.")
		g.MaxLength(64)
		g.Example(map[string]any{
			"max_connections": 1000,
		})
	})
	g.Attribute("orchestrator_opts", OrchestratorOpts, func() {
		g.Description("Orchestrator-specific configuration options.")
	})

	g.Required("database_name", "nodes")
})

var Database = g.ResultType("Database", func() {
	g.Attributes(func() {
		g.Attribute("id", Identifier, func() {
			g.Description("Unique identifier for the database.")
			g.Example("production")
			g.Example("my-app")
			g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
		})
		g.Attribute("tenant_id", Identifier, func() {
			g.Description("Unique identifier for the databases's owner.")
			g.Example("engineering")
			g.Example("8210ec10-2dca-406c-ac4a-0661d2189954")
		})
		g.Attribute("created_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the database was created.")
			g.Example("2025-01-01T01:30:00Z")
		})
		g.Attribute("updated_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the database was last updated.")
			g.Example("2025-01-01T02:30:00Z")
		})
		g.Attribute("state", g.String, func() {
			g.Description("Current state of the database.")
			g.Enum(
				"creating",
				"modifying",
				"available",
				"deleting",
				"degraded",
				"failed",
				"backing_up",
				"restoring",
				"unknown",
			)
		})
		g.Attribute("instances", g.CollectionOf(Instance), func() {
			g.Description("All of the instances in the database.")
		})
		g.Attribute("spec", DatabaseSpec, func() {
			g.Description("The user-provided specification for the database.")
		})
	})

	g.View("default", func() {
		g.Attribute("id")
		g.Attribute("tenant_id")
		g.Attribute("created_at")
		g.Attribute("updated_at")
		g.Attribute("state")
		g.Attribute("instances", func() {
			g.View("default")
		})
		g.Attribute("spec")

		g.Example(exampleDatabase)
	})

	g.View("abbreviated", func() {
		g.Attribute("id")
		g.Attribute("tenant_id")
		g.Attribute("created_at")
		g.Attribute("updated_at")
		g.Attribute("state")
		g.Attribute("instances", func() {
			g.View("abbreviated")
		})
	})

	g.Required("id", "created_at", "updated_at", "state")
})

var CreateDatabaseRequest = g.Type("CreateDatabaseRequest", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("Unique identifier for the database.")
		g.Example("production")
		g.Example("my-app")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("tenant_id", Identifier, func() {
		g.Description("Unique identifier for the databases's owner.")
		g.Example("engineering")
		g.Example("8210ec10-2dca-406c-ac4a-0661d2189954")
	})
	g.Attribute("spec", DatabaseSpec, func() {
		g.Description("The specification for the database.")
	})

	g.Required("spec")

	g.Example("Minimal", func() {
		g.Description("A minimal configuration that relies on default values for most fields.")
		g.Value(map[string]any{
			"id": "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"port": 5432,
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})

	g.Example("With local backups", func() {
		g.Description("A configuration that includes backups to a locally-mounted NFS on n1.")
		g.Value(map[string]any{
			"id": "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"nodes": []map[string]any{
					{
						"name":     "n1",
						"host_ids": []string{"us-east-1"},
						"orchestrator_opts": map[string]any{
							"swarm": map[string]any{
								"extra_volumes": []map[string]any{
									{
										"host_path":        "/mnt/backups",
										"destination_path": "/backups",
									},
								},
							},
						},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "posix",
									"base_path": "/backups",
								},
							},
						},
					},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})

	g.Example("With custom networks, volumes, and labels", func() {
		g.Description("A configuration that mounts a local volume, attaches the container to multiple Swarm networks and applies custom labels.")
		g.Value(map[string]any{
			"id": "inventory-db",
			"spec": map[string]any{
				"database_name": "inventory",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "inv_admin",
						"password":   "securepass",
						"db_owner":   true,
						"attributes": []string{"LOGIN"},
					},
				},
				"nodes": []map[string]any{
					{
						"name":     "n1",
						"host_ids": []string{"host-1"},
						"orchestrator_opts": map[string]any{
							"swarm": map[string]any{
								"extra_volumes": []map[string]any{
									{
										"host_path":        "/mnt/backups",
										"destination_path": "/backups",
									},
								},
								"extra_networks": []map[string]any{
									{
										"id": "net-network",
									},
								},
								"extra_labels": map[string]string{
									"traefik.enable":              "true",
									"traefik.tcp.routers.db.rule": "HostSNI(`inventory.example.com`)",
									"environment":                 "staging",
								},
							},
						},
					},
					{"name": "n2", "host_ids": []string{"host-2"}},
				},
			},
		})
	})

	g.Example("With cloud backups", func() {
		g.Description("A configuration that includes backups to S3 from all nodes. Note that the S3 access key and secret can be excluded if EC2 instance profiles are configured for each host.")
		g.Value(map[string]any{
			"id": "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"nodes": []map[string]any{
					{
						"name":     "n1",
						"host_ids": []string{"us-east-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-us-east-1",
								},
							},
						},
					},
					{
						"name":     "n2",
						"host_ids": []string{"ap-south-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-ap-south-1",
								},
							},
						},
					},
					{
						"name":     "n3",
						"host_ids": []string{"eu-central-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-eu-central-1",
								},
							},
						},
					},
				},
			},
		})
	})

	g.Example("Creating from an existing backup", func() {
		g.Description("A configuration that creates a database from an existing backup in S3.")
		g.Value(map[string]any{
			"id": "storefront-staging",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"restore_config": map[string]any{
					"source_database_id":   "storefront",
					"source_database_name": "storefront",
					"source_node_name":     "n1",
					"repository": map[string]any{
						"type":      "s3",
						"s3_bucket": "storefront-db-backups-us-east-1",
					},
				},
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})

	g.Example("Built-in roles", func() {
		g.Description("The Control Plane can create multiple users on your behalf, and it includes some built-in roles that make it easy to assign limited permissions.")
		g.Value(map[string]any{
			"id": "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
					{
						"username":   "storefront_app",
						"password":   "password",
						"attributes": []string{"LOGIN"},
						"roles":      []string{"pgedge_application"},
					},
					{
						"username":   "business_intelligence_app",
						"password":   "password",
						"attributes": []string{"LOGIN"},
						"roles":      []string{"pgedge_application_read_only"},
					},
				},
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})

	g.Example("Customized PostgreSQL configuration", func() {
		g.Description("The Control Plane will automatically set and tune some parameters based on the resources available to the host. You can override these and other settings for the whole database or per-node.")
		g.Value(map[string]any{
			"id": "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"password":   "password",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"postgresql_conf": map[string]any{
					"max_connections": 5000,
				},
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})
})

var CreateDatabaseResponse = g.Type("CreateDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will create this database.")
	})
	g.Attribute("database", Database, func() {
		g.Description("The database being created.")
	})

	g.Required("task", "database")

	g.Example(map[string]any{
		"database": map[string]any{
			"created_at": "2025-06-18T16:52:05Z",
			"id":         "storefront",
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"attributes": []any{
							"SUPERUSER",
							"LOGIN",
						},
						"db_owner": true,
						"username": "admin",
					},
				},
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
				"postgres_version": "17",
				"spock_version":    "5",
			},
			"state":      "creating",
			"updated_at": "2025-06-18T16:52:05Z",
		},
		"task": map[string]any{
			"created_at":  "2025-06-18T16:52:05Z",
			"database_id": "storefront",
			"status":      "pending",
			"task_id":     "019783f4-75f4-71e7-85a3-c9b96b345d77",
			"type":        "create",
		},
	})
})

var UpdateDatabaseRequest = g.Type("UpdateDatabaseRequest", func() {
	g.Attribute("tenant_id", Identifier, func() {
		g.Description("Unique identifier for the databases's owner.")
		g.Example("engineering")
		g.Example("8210ec10-2dca-406c-ac4a-0661d2189954")
	})
	g.Attribute("spec", DatabaseSpec, func() {
		g.Description("The specification for the database.")
	})

	g.Example("Minimal", func() {
		g.Value(map[string]any{
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"nodes": []map[string]any{
					{"name": "n1", "host_ids": []string{"us-east-1"}},
					{"name": "n2", "host_ids": []string{"ap-south-1"}},
					{"name": "n3", "host_ids": []string{"eu-central-1"}},
				},
			},
		})
	})

	g.Example("Adding a new node from a Cloud backup", func() {
		g.Description("This update request adds a new node, n3, to an existing two-node cluster using the latest backup of n1.")
		g.Value(map[string]any{
			"spec": map[string]any{
				"database_name": "storefront",
				"port":          5432,
				"database_users": []map[string]any{
					{
						"username":   "admin",
						"db_owner":   true,
						"attributes": []string{"LOGIN", "SUPERUSER"},
					},
				},
				"nodes": []map[string]any{
					{
						"name":     "n1",
						"host_ids": []string{"us-east-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-us-east-1",
								},
							},
						},
					},
					{
						"name": "n2", "host_ids": []string{"ap-south-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-ap-south-1",
								},
							},
						},
					},
					{
						"name":     "n3",
						"host_ids": []string{"eu-central-1"},
						"backup_config": map[string]any{
							"repositories": []map[string]any{
								{
									"type":      "s3",
									"s3_bucket": "storefront-db-backups-eu-central-1",
								},
							},
						},
						"restore_config": map[string]any{
							"source_database_id":   "storefront",
							"source_database_name": "storefront",
							"source_node_name":     "n1",
							"repository": map[string]any{
								"type":      "s3",
								"s3_bucket": "storefront-db-backups-us-east-1",
							},
						},
					},
				},
			},
		})
	})

	g.Required("spec")
})

var UpdateDatabaseResponse = g.Type("UpdateDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will update this database.")
	})
	g.Attribute("database", Database, func() {
		g.Description("The database being updated.")
	})

	g.Required("task", "database")

	g.Example(map[string]any{
		"database": exampleDatabase,
		"task": map[string]any{
			"created_at":  "2025-06-18T17:23:14Z",
			"database_id": "storefront",
			"status":      "pending",
			"task_id":     "01978410-fb5d-7cd2-bbd2-66c0bf929dc0",
			"type":        "update",
		},
	})
})

var DeleteDatabaseResponse = g.Type("DeleteDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will delete this database.")
	})

	g.Required("task")

	g.Example(map[string]any{
		"task": map[string]any{
			"created_at":  "2025-06-18T16:48:59Z",
			"database_id": "storefront",
			"status":      "pending",
			"task_id":     "019783f1-9f17-77e7-9a08-fa6ab39e3b29",
			"type":        "delete",
		},
	})
})

var BackupDatabaseNodeResponse = g.Type("BackupDatabaseNodeResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will backup this database node.")
	})

	g.Required("task")

	g.Example(map[string]any{
		"task": map[string]any{
			"created_at":  "2025-06-18T17:54:28Z",
			"database_id": "storefront",
			"status":      "pending",
			"task_id":     "0197842d-9082-7496-b787-77bd2e11809f",
			"type":        "node_backup",
		},
	})
})

var RestoreDatabaseRequest = g.Type("RestoreDatabaseRequest", func() {
	g.Attribute("restore_config", RestoreConfigSpec, func() {
		g.Description("Configuration for the restore process.")
	})
	g.Attribute("target_nodes", g.ArrayOf(g.String), func() {
		g.Description("The nodes to restore. Defaults to all nodes if empty or unspecified.")
		g.MaxLength(9)
		g.Example([]string{"n1", "n2"})
		g.Example([]string{"n1"})
	})

	g.Required("restore_config")

	g.Example("Restore all nodes", func() {
		g.Description("If the target_nodes field is omitted from the request, all nodes will be restored using the given restore_config.")
		g.Value(map[string]any{
			"restore_config": map[string]any{
				"source_database_id":   "storefront",
				"source_node_name":     "n1",
				"source_database_name": "storefront",
				"repository": map[string]any{
					"type":      "posix",
					"base_path": "/backups",
				},
			},
		})
	})

	g.Example("Restore n1 only", func() {
		g.Description("Example of restoring a single target node.")
		g.Value(map[string]any{
			"target_nodes": []string{"n1"},
			"restore_config": map[string]any{
				"source_database_id":   "storefront",
				"source_node_name":     "n1",
				"source_database_name": "storefront",
				"repository": map[string]any{
					"type":      "posix",
					"base_path": "/backups",
				},
			},
		})
	})
})

var RestoreDatabaseResponse = g.Type("RestoreDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will restore this database.")
	})
	g.Attribute("node_tasks", g.ArrayOf(Task), func() {
		g.Description("The tasks that will restore each database node.")
	})
	g.Attribute("database", Database, func() {
		g.Description("The database being restored.")
	})

	g.Required("task", "node_tasks", "database")

	g.Example(map[string]any{
		"database": exampleDatabase,
		"node_tasks": []map[string]any{
			{
				"created_at":  "2025-06-18T17:58:59Z",
				"database_id": "storefront",
				"node_name":   "n1",
				"parent_id":   "01978431-b628-758a-aec6-03b331fa1a17",
				"status":      "pending",
				"task_id":     "01978431-b62b-723b-a09c-e4072cd64bdb",
				"type":        "node_restore",
			},
			{
				"created_at":  "2025-06-18T17:58:59Z",
				"database_id": "storefront",
				"node_name":   "n2",
				"parent_id":   "01978431-b628-758a-aec6-03b331fa1a17",
				"status":      "pending",
				"task_id":     "01978431-b62c-7593-aad8-43b03df2031b",
				"type":        "node_restore",
			},
			{
				"created_at":  "2025-06-18T17:58:59Z",
				"database_id": "storefront",
				"node_name":   "n3",
				"parent_id":   "01978431-b628-758a-aec6-03b331fa1a17",
				"status":      "pending",
				"task_id":     "01978431-b62d-7b65-ab09-272d0b2fea91",
				"type":        "node_restore",
			},
		},
		"task": map[string]any{
			"created_at":  "2025-06-18T17:58:59Z",
			"database_id": "storefront",
			"status":      "pending",
			"task_id":     "01978431-b628-758a-aec6-03b331fa1a17",
			"type":        "restore",
		},
	})
})

var ExtraVolumesSpec = g.Type("ExtraVolumesSpec", func() {
	g.Description("Extra volumes to mount from the host to the database container.")

	g.Attribute("host_path", g.String, func() {
		g.Description("The host path for the volume.")
		g.MaxLength(256)
		g.Example("/Users/user/backups/host")
	})

	g.Attribute("destination_path", g.String, func() {
		g.Description("The path inside the container where the volume will be mounted.")
		g.MaxLength(256)
		g.Example("/backups/container")
	})

	g.Required("host_path", "destination_path")
})

var ListDatabasesResponse = g.ResultType("ListDatabasesResponse", func() {
	g.TypeName("ListDatabasesResponse")
	g.Attributes(func() {
		g.Attribute("databases", g.CollectionOf(Database), func() {
			g.View("abbreviated")
		})

		g.Example(map[string]any{
			"databases": []map[string]any{
				{
					"created_at": "2025-06-17T20:05:10Z",
					"id":         "inventory",
					"instances": []map[string]any{
						{
							"host_id":   "us-east-1",
							"id":        "inventory-n1-689qacsi",
							"node_name": "n1",
							"state":     "available",
						},
						{
							"host_id":   "ap-south-1",
							"id":        "inventory-n2-9ptayhma",
							"node_name": "n2",
							"state":     "available",
						},
						{
							"host_id":   "eu-central-1",
							"id":        "inventory-n3-ant97dj4",
							"node_name": "n3",
							"state":     "available",
						},
					},
					"state":      "available",
					"updated_at": "2025-06-17T20:05:10Z",
				},
				{
					"created_at": "2025-06-17T20:05:10Z",
					"id":         "storefront",
					"instances": []map[string]any{
						{
							"host_id":   "us-east-1",
							"id":        "storefront-n1-689qacsi",
							"node_name": "n1",
							"state":     "available",
						},
						{
							"host_id":   "ap-south-1",
							"id":        "storefront-n2-9ptayhma",
							"node_name": "n2",
							"state":     "available",
						},
						{
							"host_id":   "eu-central-1",
							"id":        "storefront-n3-ant97dj4",
							"node_name": "n3",
							"state":     "available",
						},
					},
					"state":      "available",
					"updated_at": "2025-06-12T15:10:05Z",
				},
			},
		})
	})
})

// example of the Database response type to be reused in different examples
var exampleDatabase = map[string]any{
	"created_at": "2025-06-18T16:52:05Z",
	"id":         "storefront",
	"instances": []map[string]any{
		{
			"connection_info": map[string]any{
				"hostname":     "i-0123456789abcdef.ec2.internal",
				"ipv4_address": "10.24.34.2",
				"port":         5432,
			},
			"created_at": "2025-06-18T16:52:22Z",
			"host_id":    "us-east-1",
			"id":         "storefront-n1-689qacsi",
			"node_name":  "n1",
			"postgres": map[string]any{
				"patroni_state": "running",
				"role":          "primary",
				"version":       "17.5",
			},
			"spock": map[string]any{
				"read_only": "off",
				"subscriptions": []any{
					map[string]any{
						"name":          "sub_n1n3",
						"provider_node": "n3",
						"status":        "replicating",
					},
					map[string]any{
						"name":          "sub_n1n2",
						"provider_node": "n2",
						"status":        "replicating",
					},
				},
				"version": "4.0.10",
			},
			"state":             "available",
			"status_updated_at": "2025-06-18T17:58:56Z",
			"updated_at":        "2025-06-18T17:54:36Z",
		},
		{
			"connection_info": map[string]any{
				"hostname":     "i-058731542fee493f.ec2.internal",
				"ipv4_address": "10.24.35.2",
				"port":         5432,
			},
			"created_at": "2025-06-18T16:52:22Z",
			"host_id":    "ap-south-1",
			"id":         "storefront-n2-9ptayhma",
			"node_name":  "n2",
			"postgres": map[string]any{
				"patroni_state": "running",
				"role":          "primary",
				"version":       "17.5",
			},
			"spock": map[string]any{
				"read_only": "off",
				"subscriptions": []any{
					map[string]any{
						"name":          "sub_n2n1",
						"provider_node": "n1",
						"status":        "replicating",
					},
					map[string]any{
						"name":          "sub_n2n3",
						"provider_node": "n3",
						"status":        "replicating",
					},
				},
				"version": "4.0.10",
			},
			"state":             "available",
			"status_updated_at": "2025-06-18T17:58:56Z",
			"updated_at":        "2025-06-18T17:54:01Z",
		},
		{
			"connection_info": map[string]any{
				"hostname":     "i-494027b7b53f6a23.ec2.internal",
				"ipv4_address": "10.24.36.2",
				"port":         5432,
			},
			"created_at": "2025-06-18T16:52:22Z",
			"host_id":    "eu-central-1",
			"id":         "storefront-n3-ant97dj4",
			"node_name":  "n3",
			"postgres": map[string]any{
				"patroni_state": "running",
				"role":          "primary",
				"version":       "17.5",
			},
			"spock": map[string]any{
				"read_only": "off",
				"subscriptions": []any{
					map[string]any{
						"name":          "sub_n3n1",
						"provider_node": "n1",
						"status":        "replicating",
					},
					map[string]any{
						"name":          "sub_n3n2",
						"provider_node": "n2",
						"status":        "replicating",
					},
				},
				"version": "4.0.10",
			},
			"state":             "available",
			"status_updated_at": "2025-06-18T17:58:56Z",
			"updated_at":        "2025-06-18T17:54:01Z",
		},
	},
	"spec": map[string]any{
		"database_name": "storefront",
		"port":          5432,
		"database_users": []map[string]any{
			{
				"attributes": []any{
					"SUPERUSER",
					"LOGIN",
				},
				"db_owner": true,
				"username": "admin",
			},
		},
		"nodes": []map[string]any{
			{"host_ids": []any{"us-east-1"}, "name": "n1"},
			{"host_ids": []any{"ap-south-1"}, "name": "n2"},
			{"host_ids": []any{"eu-central-1"}, "name": "n3"},
		},
		"postgres_version": "17",
		"spock_version":    "5",
	},
	"state":      "restoring",
	"updated_at": "2025-06-18T17:58:59Z",
}

var ExtraNetworkSpec = g.Type("ExtraNetworkSpec", func() {
	g.Description("Describes an additional Docker network to attach the container to.")
	g.Attribute("id", g.String, func() {
		g.Description("The name or ID of the network to connect to.")
		g.Example("storefront")
		g.Example("traefik-public")
	})
	g.Attribute("aliases", g.ArrayOf(g.String), func() {
		g.Description("Optional network-scoped aliases for the container.")
		g.MaxLength(8)
		g.Example([]string{"pg-db", "db-alias"})
	})
	g.Attribute("driver_opts", g.MapOf(g.String, g.String), func() {
		g.Description("Optional driver options for the network connection.")
		g.Example(map[string]string{
			"com.docker.network.endpoint.expose": "true",
		})
	})
	g.Required("id")
})

var SwarmOpts = g.Type("SwarmOpts", func() {
	g.Description("Docker Swarm-specific options.")

	g.Attribute("extra_volumes", g.ArrayOf(ExtraVolumesSpec), func() {
		g.Description("A list of extra volumes to mount. Each entry defines a host and container path.")
		g.MaxLength(16)
	})

	g.Attribute("extra_networks", g.ArrayOf(ExtraNetworkSpec), func() {
		g.Description("A list of additional Docker Swarm networks to attach containers in this database to.")
		g.MaxLength(8)
	})
	g.Attribute("extra_labels", g.MapOf(g.String, g.String), func() {
		g.Description("Arbitrary labels to apply to the Docker Swarm service")
		g.Example(map[string]string{
			"traefik.enable":                "true",
			"traefik.tcp.routers.mydb.rule": "HostSNI(`mydb.example.com`)",
		})
	})
})

var OrchestratorOpts = g.Type("OrchestratorOpts", func() {
	g.Description("Options specific to the selected orchestrator.")

	g.Attribute("swarm", SwarmOpts, func() {
		g.Description("Swarm-specific configuration.")
	})
})
