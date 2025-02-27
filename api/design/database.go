package design

import (
	g "goa.design/goa/v3/dsl"
)

var DatabaseReplicaSpec = g.Type("DatabaseReplicaSpec", func() {
	g.Attribute("host_id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("The ID of the host that should run this read replica.")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
	})

	g.Required("host_id")
})

var HostIDs = g.ArrayOf(g.String, func() {
	g.Format(g.FormatUUID)
	g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
})

var DatabaseNodeSpec = g.Type("DatabaseNodeSpec", func() {
	// TODO: Validation to enforce that node names are unique within a database.
	g.Attribute("name", g.String, func() {
		g.Description("The name of the database node.")
		g.Pattern("n[0-9]+")
		g.Example("n1")
	})
	g.Attribute("host_ids", HostIDs, func() {
		g.Description("The IDs of the hosts that should run this node. When multiple hosts are specified, one host will chosen as a primary and the others will be read replicas.")
		g.MinLength(1)
	})
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The major version of Postgres for this node. Overrides the Postgres version set in the DatabaseSpec.")
		g.Enum("16", "17")
		g.Example("17")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database for this node. Overrides the Postgres port set in the DatabaseSpec.")
		g.Example(5432)
	})
	g.Attribute("storage_class", g.String, func() {
		g.Description("The storage class to use for the database on this node. The possible values and defaults depend on the orchestrator.")
		g.Example("host")
		g.Example("loop_device")
	})
	g.Attribute("storage_size", g.String, func() {
		g.Description("The size of the storage for this node in SI or IEC notation. Support for this value depends on the orchestrator and storage class.")
		g.Example("500GiB")
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database on this node and to use for tuning Postgres. Defaults to the number of available CPUs on the host. Can include an SI suffix, e.g. '500m' for 500 millicpus. Whether this limit will be enforced depends on the orchestrator.")
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database on this node and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit will be enforced depends on the orchestrator.")
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

	g.Required("name", "host_ids")
})

var DatabaseUserSpec = g.Type("DatabaseUserSpec", func() {
	g.Attribute("username", g.String, func() {
		g.Description("The username for this database user.")
		g.Example("admin")
	})
	g.Attribute("password", g.String, func() {
		g.Description("The password for this database user.")
		g.Example("secret")
	})
	g.Attribute("db_owner", g.Boolean, func() {
		g.Description("If true, this user will be granted database ownership.")
	})
	g.Attribute("attributes", g.ArrayOf(g.String), func() {
		g.Description("The attributes to assign to this database user.")
		g.Example([]string{"LOGIN", "SUPERUSER"})
		g.Example([]string{"LOGIN", "CREATEDB", "CREATEROLE"})
	})
	g.Attribute("roles", g.ArrayOf(g.String), func() {
		g.Description("The roles to assign to this database user.")
		g.Example([]string{"application"})
		g.Example([]string{"application_read_only"})
		g.Example([]string{"pgedge_superuser"})
	})

	g.Required("username", "password")
})

var BackupRepositorySpec = g.Type("BackupRepositorySpec", func() {
	g.Attribute("id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("The unique identifier of this repository.")
		g.Example("f6b84a99-5e91-4203-be1e-131fe82e5984")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of this repository.")
		g.Enum("s3", "gcs", "azure")
		g.Example("s3")
	})
	g.Attribute("s3_bucket", g.String, func() {
		g.Description("The S3 bucket name for this repository. Only applies when type = 's3'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("s3_region", g.String, func() {
		g.Description("The region of the S3 bucket for this repository. Only applies when type = 's3'.")
		g.Example("us-east-1")
	})
	g.Attribute("s3_endpoint", g.String, func() {
		g.Description("The optional S3 endpoint for this repository. Only applies when type = 's3'.")
		g.Example("s3.us-east-1.amazonaws.com")
	})
	g.Attribute("gcs_bucket", g.String, func() {
		g.Description("The GCS bucket name for this repository. Only applies when type = 'gcs'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("gcs_endpoint", g.String, func() {
		g.Description("The optional GCS endpoint for this repository. Only applies when type = 'gcs'.")
		g.Example("localhost")
	})
	g.Attribute("azure_account", g.String, func() {
		g.Description("The Azure account name for this repository. Only applies when type = 'azure'.")
		g.Example("pgedge-backups")
	})
	g.Attribute("azure_container", g.String, func() {
		g.Description("The Azure container name for this repository. Only applies when type = 'azure'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("azure_endpoint", g.String, func() {
		g.Description("The optional Azure endpoint for this repository. Only applies when type = 'azure'.")
		g.Example("blob.core.usgovcloudapi.net")
	})
	g.Attribute("retention_full", g.Int, func() {
		g.Description("The count of full backups to retain or the time to retain full backups.")
		g.Example(2)
	})
	g.Attribute("retention_full_type", g.String, func() {
		g.Description("The type of measure used for retention_full.")
		g.Enum("time", "count")
		g.Example("count")
	})
	g.Attribute("base_path", g.String, func() {
		g.Description("The base path within the repository to store backups.")
		g.Example("/backups")
	})

	g.Required("type")
})

var BackupScheduleSpec = g.Type("BackupScheduleSpec", func() {
	g.Attribute("id", g.String, func() {
		g.Description("The unique identifier for this backup schedule.")
		g.Example("daily-full-backup")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of backup to take on this schedule.")
		g.Enum("full", "incr")
		g.Example("full")
	})
	g.Attribute("cron_expression", g.String, func() {
		g.Description("The cron expression for this schedule.")
		g.Example("0 6 * * ?")
	})

	g.Required("id", "type", "cron_expression")
})

var BackupConfigSpec = g.Type("BackupConfigSpec", func() {
	g.Attribute("id", g.String, func() {
		g.Description("The unique identifier for this backup configuration.")
		g.Example("default")
	})
	g.Attribute("provider", g.String, func() {
		g.Description("The backup provider for this backup configuration.")
		g.Enum("pgbackrest", "pg_dump")
		g.Example("pgbackrest")
	})
	g.Attribute("repositories", g.ArrayOf(BackupRepositorySpec), func() {
		g.Description("The repositories for this backup configuration.")
	})
	g.Attribute("schedules", g.ArrayOf(BackupScheduleSpec), func() {
		g.Description("The schedules for this backup configuration.")
	})

	g.Required("id", "provider")
})

var RestoreRepositorySpec = g.Type("RestoreRepositorySpec", func() {
	g.Attribute("id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("The unique identifier of this repository.")
		g.Example("f6b84a99-5e91-4203-be1e-131fe82e5984")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of this repository.")
		g.Enum("s3", "gcs", "azure")
		g.Example("s3")
	})
	g.Attribute("s3_bucket", g.String, func() {
		g.Description("The S3 bucket name for this repository. Only applies when type = 's3'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("s3_region", g.String, func() {
		g.Description("The region of the S3 bucket for this repository. Only applies when type = 's3'.")
		g.Example("us-east-1")
	})
	g.Attribute("s3_endpoint", g.String, func() {
		g.Description("The optional S3 endpoint for this repository. Only applies when type = 's3'.")
		g.Example("s3.us-east-1.amazonaws.com")
	})
	g.Attribute("gcs_bucket", g.String, func() {
		g.Description("The GCS bucket name for this repository. Only applies when type = 'gcs'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("gcs_endpoint", g.String, func() {
		g.Description("The optional GCS endpoint for this repository. Only applies when type = 'gcs'.")
		g.Example("localhost")
	})
	g.Attribute("azure_account", g.String, func() {
		g.Description("The Azure account name for this repository. Only applies when type = 'azure'.")
		g.Example("pgedge-backups")
	})
	g.Attribute("azure_container", g.String, func() {
		g.Description("The Azure container name for this repository. Only applies when type = 'azure'.")
		g.Example("pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1")
	})
	g.Attribute("azure_endpoint", g.String, func() {
		g.Description("The optional Azure endpoint for this repository. Only applies when type = 'azure'.")
		g.Example("blob.core.usgovcloudapi.net")
	})
	g.Attribute("base_path", g.String, func() {
		g.Description("The base path within the repository where backups are stored.")
		g.Example("/backups")
	})

	g.Required("id", "type")
})

var RestoreConfigSpec = g.Type("RestoreConfigSpec", func() {
	g.Attribute("provider", g.String, func() {
		g.Description("The backup provider for this restore configuration.")
		g.Enum("pgbackrest", "pg_dump")
		g.Example("pgbackrest")
	})
	g.Attribute("node_name", g.String, func() {
		g.Description("The name of the node to restore this database from.")
		g.Example("n1")
	})
	g.Attribute("repository", RestoreRepositorySpec, func() {
		g.Description("The repository to restore this database from.")
	})

	g.Required("provider", "node_name", "repository")
})

var DatabaseSpec = g.Type("DatabaseSpec", func() {
	g.Attribute("database_name", g.String, func() {
		g.Description("The name of the Postgres database.")
		g.Example("northwind")
	})
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The major version of the Postgres database.")
		g.Enum("16", "17")
		g.Example("17")
	})
	g.Attribute("spock_version", g.String, func() {
		g.Description("The major version of the Spock extension.")
		g.Enum("4")
		g.Example("4")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The port used by the Postgres database.")
		g.Example(5432)
	})
	g.Attribute("deletion_protection", g.Boolean, func() {
		g.Description("Prevents deletion when true.")
		g.Example(true)
	})
	g.Attribute("storage_class", g.String, func() {
		g.Description("The storage class to use for the database. The possible values and defaults depend on the orchestrator.")
		g.Example("host")
		g.Example("loop_device")
	})
	g.Attribute("storage_size", g.String, func() {
		g.Description("The size of the storage in SI or IEC notation. Support for this value depends on the orchestrator and storage class.")
		g.Example("500GiB")
	})
	g.Attribute("cpus", g.String, func() {
		g.Description("The number of CPUs to allocate for the database and to use for tuning Postgres. Defaults to the number of available CPUs on the host. Can include an SI suffix, e.g. '500m' for 500 millicpus. Whether this limit will be enforced depends on the orchestrator.")
		g.Example("14")
		g.Example("0.5")
		g.Example("500m")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory in SI or IEC notation to allocate for the database and to use for tuning Postgres. Defaults to the total available memory on the host. Whether this limit will be enforced depends on the orchestrator.")
		g.Example("16GiB")
		g.Example("500M")
	})
	g.Attribute("nodes", g.ArrayOf(DatabaseNodeSpec), func() {
		g.Description("The Spock nodes for this database.")
	})
	g.Attribute("database_users", g.ArrayOf(DatabaseUserSpec), func() {
		g.Description("The users to create for this database.")
	})
	g.Attribute("features", g.MapOf(g.String, g.String), func() {
		g.Description("The feature flags for this database.")
		g.Example(map[string]string{
			"some_feature": "enabled",
		})
	})
	g.Attribute("backup_config", BackupConfigSpec, func() {
		g.Description("The backup configuration for this database.")
	})
	g.Attribute("restore_config", RestoreConfigSpec, func() {
		g.Description("The restore configuration for this database.")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings. Will be merged with the settings provided by control-plane.")
		g.Example(map[string]any{
			"max_connections": 1000,
		})
	})

	g.Required("database_name", "nodes")
})

var Database = g.ResultType("Database", func() {
	g.Attributes(func() {
		g.Attribute("id", g.String, func() {
			g.Format(g.FormatUUID)
			g.Description("Unique identifier for the database.")
			g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
		})
		g.Attribute("tenant_id", g.String, func() {
			g.Format(g.FormatUUID)
			g.Description("Unique identifier for the databases's owner.")
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
			g.View("abbreviated")
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
		g.Attribute("instances")
		g.Attribute("spec")
	})

	g.View("abbreviated", func() {
		g.Attribute("id")
		g.Attribute("tenant_id")
		g.Attribute("created_at")
		g.Attribute("updated_at")
		g.Attribute("state")
		g.Attribute("instances")
	})

	g.Required("id", "created_at", "updated_at", "state")
})

var CreateDatabaseRequest = g.Type("CreateDatabaseRequest", func() {
	g.Attribute("id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("Unique identifier for the database.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("tenant_id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("Unique identifier for the databases's owner.")
		g.Example("8210ec10-2dca-406c-ac4a-0661d2189954")
	})
	g.Attribute("spec", DatabaseSpec, func() {
		g.Description("The specification for the database.")
	})
})

var UpdateDatabaseRequest = g.Type("UpdateDatabaseRequest", func() {
	g.Attribute("spec", DatabaseSpec, func() {
		g.Description("The specification for the database.")
	})
})
