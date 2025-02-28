package design

import (
	g "goa.design/goa/v3/dsl"
)

var InstanceInterface = g.Type("InstanceInterface", func() {
	g.Attribute("network_type", g.String, func() {
		g.Description("The type of network for this interface.")
		g.Enum("docker", "host")
		g.Example("docker")
	})
	g.Attribute("network_id", g.String, func() {
		g.Description("The unique identifier of the network for this interface.")
		g.Example("l5imrq28sh6s")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of the instance on this interface.")
		g.Example("postgres-n1")
	})
	g.Attribute("ipv4_address", g.String, func() {
		g.Format(g.FormatIPv4)
		g.Description("The IPv4 address of the instance on this interface.")
		g.Example("10.1.0.113")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The Postgres port for the instance on this interface.")
		g.Example(5432)
	})

	g.Required("network_type", "port")
})

// var InstanceSpec = g.Type("InstanceSpec", func() {
// 	g.Attribute("database_name", g.String, func() {
// 		g.Description("The name of the database for this instance.")
// 		g.Example("mydb")
// 	})
// 	g.Attribute("node_name", g.String, func() {
// 		g.Description("The Spock node name for this instance.")
// 		g.Example("n1")
// 	})
// 	g.Attribute("replica_name", g.String, func() {
// 		g.Description("The read replica name of this instance.")
// 		g.Example("r1")
// 	})
// 	g.Attribute("postgres_version", g.String, func() {
// 		g.Description("The version of Postgres for this instance.")
// 		g.Example("17.1")
// 	})
// 	g.Attribute("spock_version", g.String, func() {
// 		g.Description("The version of Spock for this instance.")
// 		g.Example("4.0.9")
// 	})
// 	g.Attribute("port", g.Int, func() {
// 		g.Description("The Postgres port for this instance.")
// 		g.Example(5432)
// 	})
// 	g.Attribute("storage_class", g.String, func() {
// 		g.Description("The storage class for this instance.")
// 		g.Example("loop_device")
// 	})
// 	g.Attribute("storage_size", g.String, func() {
// 		g.Description("The size of the storage for this instance.")
// 		g.Example("10GiB")
// 	})
// 	g.Attribute("cpus", g.String, func() {
// 		g.Description("The number of CPUs for this instance.")
// 		g.Example("0.5")
// 	})
// 	g.Attribute("memory", g.String, func() {
// 		g.Description("The amount of memory for this instance.")
// 		g.Example("1GiB")
// 	})
// 	g.Attribute("database_users", g.ArrayOf(DatabaseUserSpec), func() {
// 		g.Description("All users that have access to this instance.")
// 	})
// 	g.Attribute("features", g.MapOf(g.String, g.String), func() {
// 		g.Description("All features enabled for this instance.")
// 	})
// 	g.Attribute("backup_config", g.ArrayOf(BackupConfigSpec), func() {
// 		g.Description("All backup configurations for this instance.")
// 	})
// 	g.Attribute("restore_config", RestoreConfigSpec, func() {
// 		g.Description("The restore configuration for this instance.")
// 	})
// 	g.Attribute("postgresql_conf", g.MapOf(g.String, g.Any), func() {
// 		g.Description("The Postgres configuration for this instance.")
// 	})
// })

var Instance = g.ResultType("Instance", func() {
	g.Attributes(func() {
		g.Attribute("id", g.String, func() {
			g.Format(g.FormatUUID)
			g.Description("Unique identifier for the instance.")
			g.Example("a67cbb36-c3c3-49c9-8aac-f4a0438a883d")
		})
		g.Attribute("host_id", g.String, func() {
			g.Format(g.FormatUUID)
			g.Description("The ID of the host this instance is running on.")
			g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
		})
		g.Attribute("node_name", g.String, func() {
			g.Description("The Spock node name for this instance.")
			g.Example("n1")
		})
		g.Attribute("replica_name", g.String, func() {
			g.Description("The read replica name of this instance.")
			g.Example("r1")
		})
		g.Attribute("created_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the instance was created.")
		})
		g.Attribute("updated_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the instance was last updated.")
		})
		g.Attribute("state", g.String, func() {
			g.Enum(
				"creating",
				"modifying",
				"backing_up",
				"restoring",
				"deleting",
				"available",
				"degraded",
				"unknown",
			)
		})
		g.Attribute("patroni_state", g.String, func() {
			g.Enum(
				"stopping",
				"stopped",
				"stop failed",
				"crashed",
				"running",
				"starting",
				"start failed",
				"restarting",
				"restart failed",
				"initializing new cluster",
				"initdb failed",
				"running custom bootstrap script",
				"custom bootstrap failed",
				"creating replica",
				"unknown",
			)
		})
		g.Attribute("role", g.String, func() {
			g.Enum("replica", "primary")
		})
		g.Attribute("read_only", g.Boolean, func() {
			g.Description("True if this instance is in read-only mode.")
		})
		g.Attribute("pending_restart", g.Boolean, func() {
			g.Description("True if this instance is pending to be restarted from a configuration change.")
		})
		g.Attribute("patroni_paused", g.Boolean, func() {
			g.Description("True if Patroni has been paused for this instance.")
		})
		g.Attribute("postgres_version", g.String, func() {
			g.Description("The version of Postgres for this instance.")
			g.Example("17.1")
		})
		g.Attribute("spock_version", g.String, func() {
			g.Description("The version of Spock for this instance.")
			g.Example("4.0.9")
		})
		g.Attribute("interfaces", g.ArrayOf(InstanceInterface), func() {
			g.Description("All interfaces that this instance serves on.")
		})
		// g.Attribute("spec", InstanceSpec, func() {
		// 	g.Description("The specification for this instance.")
		// })
	})

	g.View("default", func() {
		g.Attribute("id")
		g.Attribute("host_id")
		g.Attribute("node_name")
		g.Attribute("replica_name")
		g.Attribute("created_at")
		g.Attribute("updated_at")
		g.Attribute("state")
		g.Attribute("patroni_state")
		g.Attribute("role")
		g.Attribute("read_only")
		g.Attribute("pending_restart")
		g.Attribute("patroni_paused")
		g.Attribute("postgres_version")
		g.Attribute("spock_version")
		g.Attribute("interfaces")
		// g.Attribute("spec")
	})

	g.View("abbreviated", func() {
		g.Attribute("id")
		g.Attribute("host_id")
		g.Attribute("node_name")
		g.Attribute("replica_name")
		g.Attribute("state")
	})

	g.Required("id", "host_id", "node_name", "created_at", "updated_at", "state")
})
