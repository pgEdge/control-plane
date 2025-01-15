package design

import (
	g "goa.design/goa/v3/dsl"
)

var DatabaseStatus = g.Type("DatabaseStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Enum(
			"creating",
			"modifying",
			"available",
			"error",
		)
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the database status was last updated.")
		g.Example("2025-01-01T10:30:37Z")
	})
})

var DatabaseReplicaSpec = g.Type("DatabaseReplicaSpec", func() {
	g.Attribute("instance_id", g.String, func() {
		g.Description("A unique identifier for the instance that will be created from this replica specification.")
		g.Example("5ec51c55-0921-445e-9d5b-32f5fb5dfbae")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host that should run this read replica.")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
	})

	g.Required("instance_id", "host_id")
})

var DatabaseNodeSpec = g.Type("DatabaseNodeSpec", func() {
	g.Attribute("name", g.String, func() {
		g.Description("The name of the database node.")
		g.Example("n1")
	})
	g.Attribute("instance_id", g.String, func() {
		g.Description("A unique identifier for the instance that will be created from this node specification.")
		g.Example("a67cbb36-c3c3-49c9-8aac-f4a0438a883d")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host that should run this node.")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
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
	g.Attribute("read_replicas", DatabaseReplicaSpec, func() {
		g.Description("Read replicas for this database node.")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings for this particular node. Will be merged with the settings provided by control-plane.")
		g.Example(map[string]any{
			"max_connections": 1000,
		})
	})

	g.Required("name", "instance_id", "host_id")
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
	g.Attribute("roles", g.ArrayOf(g.String), func() {
		g.Description("The roles to assign to this database user.")
		g.Example([]string{"application"})
		g.Example([]string{"application_read_only"})
	})
	g.Attribute("superuser", g.Boolean, func() {
		g.Description("Enables SUPERUSER for this database user when true.")
		g.Example(true)
	})

	g.Required("username", "password")
})

var DatabaseExtensionSpec = g.Type("DatabaseExtensionSpec", func() {
	g.Attribute("name", g.String, func() {
		g.Description("The name of the extension to install in this database.")
		g.Example("postgis")
	})
	g.Attribute("version", g.String, func() {
		g.Description("The version of the extension to install in this database.")
		g.Example("1.2.3")
	})

	g.Required("name")
})

var BackupRepositorySpec = g.Type("BackupRepositorySpec", func() {
	g.Attribute("id", g.String, func() {
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
	g.Attribute("node_names", g.ArrayOf(g.String), func() {
		g.Description("The names of the nodes where this backup configuration should be applied. The configuration will apply to all nodes when this field is empty or unspecified.")
		g.Example([]string{"n1", "n3"})
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
	g.Attribute("nodes", g.ArrayOf(DatabaseNodeSpec), func() {
		g.Description("The Spock nodes for this database.")
	})
	g.Attribute("database_users", g.ArrayOf(DatabaseUserSpec), func() {
		g.Description("The users to create for this database.")
	})
	g.Attribute("extensions", g.ArrayOf(DatabaseExtensionSpec), func() {
		g.Description("The extensions to install for this database.")
	})
	g.Attribute("features", g.MapOf(g.String, g.String), func() {
		g.Description("The feature flags for this database.")
		g.Example(map[string]string{
			"some_feature": "enabled",
		})
	})
	g.Attribute("backup_configs", g.ArrayOf(BackupConfigSpec), func() {
		g.Description("The backup configurations for this database.")
	})
	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional postgresql.conf settings. Will be merged with the settings provided by control-plane.")
		g.Example(map[string]any{
			"max_connections": 1000,
		})
	})

	g.Required("database_name", "nodes")
})

var Database = g.Type("Database", func() {
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the database.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("tenant_id", g.String, func() {
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
	g.Attribute("status", DatabaseStatus, func() {
		g.Description("Current status of the database.")
	})
	g.Attribute("instances", Instance, func() {
		g.Description("All of the instances in the database.")
	})
	g.Attribute("spec", DatabaseSpec, func() {
		g.Description("The user-provided specification for the database.")
	})

	g.Required("id", "status", "instances")
})

var CreateDatabaseRequest = g.Type("CreateDatabaseRequest", func() {
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the database.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("tenant_id", g.String, func() {
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
