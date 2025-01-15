package design

import (
	g "goa.design/goa/v3/dsl"
)

var InstanceStatus = g.Type("InstanceStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Enum(
			"creating",
			"modifying",
			"backing_up",
			"available",
			"error",
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
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance status was last updated.")
	})

	g.Required("state")
})

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
})

var Instance = g.Type("Instance", func() {
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the instance.")
		g.Example("a67cbb36-c3c3-49c9-8aac-f4a0438a883d")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host this instance is running on.")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
	})
	g.Attribute("node_name", g.String, func() {
		g.Description("The Spock node name for this instance.")
		g.Example("n1")
	})
	g.Attribute("created_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance was created.")
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance was last updated.")
	})
	g.Attribute("status", InstanceStatus, func() {
		g.Description("Current status of the instance.")
	})
	g.Attribute("interfaces", g.ArrayOf(InstanceInterface), func() {
		g.Description("All interfaces that this instance serves on.")
	})

	g.Required("id", "status")
})
