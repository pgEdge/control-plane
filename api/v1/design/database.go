package design

import (
	g "goa.design/goa/v3/dsl"
)

var postgresVersions = []any{"15", "16", "17"}
var spockVersions = []any{"4"}

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
	// TODO: Validation to enforce that node names are unique within a database.
	g.Attribute("name", g.String, func() {
		g.Description("The name of the database node.")
		g.Pattern(nodeNamePattern)
		g.Example("n1")
	})
	g.Attribute("host_ids", HostIDs, func() {
		g.Description("The IDs of the hosts that should run this node. When multiple hosts are specified, one host will chosen as a primary and the others will be read replicas.")
		g.MinLength(1)
	})
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The major version of Postgres for this node. Overrides the Postgres version set in the DatabaseSpec.")
		g.Enum(postgresVersions...)
		g.Example("17")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database for this node. Overrides the Postgres port set in the DatabaseSpec.")
		g.Minimum(1)
		g.Maximum(65535)
		g.Example(5432)
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database on this node and to use for tuning Postgres. Can include the SI suffix 'm', e.g. '500m' for 500 millicpus. Cannot allocate units smaller than 1m. Defaults to the number of available CPUs on the host if 0 or unspecified. Cannot allocate more CPUs than are available on the host. Whether this limit will be enforced depends on the orchestrator.")
		g.Pattern(cpuPattern)
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database on this node and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit will be enforced depends on the orchestrator.")
		g.MaxLength(16)
		g.Example("16GiB")
		g.Example("500M")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings for this particular node. Will be merged with the settings provided by control-plane.")
		g.MaxLength(64)
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
	g.Attribute("extra_volumes", g.ArrayOf(ExtraVolumesSpec), func() {
		g.Description("Optional list of external volumes to mount for this node only.")
		g.MaxLength(16)
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
		g.Description("The password for this database user. This field will be excluded from the response of all endpoints.")
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
		g.Description("An optional AWS access key ID to use for this repository. If not provided, pgbackrest will use the default credential provider chain. This field will be excluded from the response of all endpoints.")
		g.MinLength(16)
		g.MaxLength(128)
		g.Example("AKIAIOSFODNN7EXAMPLE")
	})
	g.Attribute("s3_key_secret", g.String, func() {
		g.Description("The corresponding secret for the AWS access key ID in s3_key. This field will be excluded from the response of all endpoints.")
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
		g.Description("Optional base64-encoded private key data. If omitted, pgbackrest will use the service account attached to the instance profile. This field will be excluded from the response of all endpoints.")
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
		g.Description("The Azure storage account access key to use for this repository. This field will be excluded from the response of all endpoints.")
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
		g.MaxLength(32)
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
		g.Description("The name of the database in this repository. This database will be renamed to the database_name in the DatabaseSpec.")
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
		g.Example("4")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database.")
		g.Minimum(1)
		g.Maximum(65535)
		g.Example(5432)
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database and to use for tuning Postgres. Defaults to the number of available CPUs on the host. Can include an SI suffix, e.g. '500m' for 500 millicpus. Whether this limit will be enforced depends on the orchestrator.")
		g.Pattern(cpuPattern)
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit will be enforced depends on the orchestrator.")
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
	g.Attribute("extra_volumes", g.ArrayOf(ExtraVolumesSpec), func() {
		g.Description("A list of extra volumes to mount. Each entry defines a host and container path.")
		g.MaxLength(16)
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
})

var CreateDatabaseResponse = g.Type("CreateDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will create this database.")
	})
	g.Attribute("database", Database, func() {
		g.Description("The database being created.")
	})

	g.Required("task", "database")
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
})

var DeleteDatabaseResponse = g.Type("DeleteDatabaseResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will delete this database.")
	})

	g.Required("task")
})

var BackupDatabaseNodeResponse = g.Type("BackupDatabaseNodeResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that will backup this database node.")
	})

	g.Required("task")
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
})

var ExtraVolumesSpec = g.Type("ExtraVolumesSpec", func() {
	g.Description("Defines an extra volumes mapping between host and container.")

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
