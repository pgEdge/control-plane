package design

import (
	g "goa.design/goa/v3/dsl"
)

var InstanceSubscription = g.Type("InstanceSubscription", func() {
	g.Description("Status information for a Spock subscription.")
	g.Attribute("provider_node", g.String, func() {
		g.Example("n2")
		g.Description("The Spock node name of the provider for this subscription.")
	})
	g.Attribute("name", g.String, func() {
		g.Example("sub_n1n2")
		g.Description("The name of the subscription.")
	})
	g.Attribute("status", g.String, func() {
		g.Example("replicating")
		g.Example("down")
		g.Description("The current status of the subscription.")
	})

	g.Required("provider_node", "name", "status")
})

var Instance = g.ResultType("Instance", func() {
	g.Description("An instance of pgEdge Postgres running on a host.")
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
		g.Attribute("created_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the instance was created.")
		})
		g.Attribute("updated_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the instance was last modified.")
		})
		g.Attribute("status_updated_at", g.String, func() {
			g.Format(g.FormatDateTime)
			g.Description("The time that the instance status information was last updated.")
		})
		g.Attribute("state", g.String, func() {
			g.Enum(
				"creating",
				"modifying",
				"backing_up",
				"available",
				"degraded",
				"failed",
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
		g.Attribute("read_only", g.String, func() {
			g.Description("The current spock.readonly setting.")
			g.Example("off")
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
		g.Attribute("hostname", g.String, func() {
			g.Description("The hostname of the host that's running this instance.")
			g.Example("i-0123456789abcdef.ec2.internal")
		})
		g.Attribute("ipv4_address", g.String, func() {
			g.Description("The IPv4 address of the host that's running this instance.")
			g.Format(g.FormatIPv4)
			g.Example("10.24.34.0")
		})
		g.Attribute("port", g.Int, func() {
			g.Description("The host port that Postgres is listening on for this instance.")
			g.Example(5432)
		})
		g.Attribute("subscriptions", g.ArrayOf(InstanceSubscription), func() {
			g.Description("Status information for this instance's Spock subscriptions.")
		})
		g.Attribute("error", g.String, func() {
			g.Description("An error message if the instance is in an error state.")
			g.Example("failed to get patroni status: connection refused")
		})
	})

	g.View("default", func() {
		g.Attribute("id")
		g.Attribute("host_id")
		g.Attribute("node_name")
		g.Attribute("created_at")
		g.Attribute("updated_at")
		g.Attribute("status_updated_at")
		g.Attribute("state")
		g.Attribute("patroni_state")
		g.Attribute("role")
		g.Attribute("read_only")
		g.Attribute("pending_restart")
		g.Attribute("patroni_paused")
		g.Attribute("postgres_version")
		g.Attribute("spock_version")
		g.Attribute("hostname")
		g.Attribute("ipv4_address")
		g.Attribute("port")
		g.Attribute("subscriptions")
		g.Attribute("error")
	})

	g.View("abbreviated", func() {
		g.Attribute("id")
		g.Attribute("host_id")
		g.Attribute("node_name")
		g.Attribute("state")
	})

	g.Required("id", "host_id", "node_name", "created_at", "updated_at", "state")
})
