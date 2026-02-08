package design

import (
	g "goa.design/goa/v3/dsl"
)

var RestartInstanceResponse = g.Type("RestartInstanceResponse", func() {
	g.Description("Response containing the restart task")
	g.Attribute("task", Task, func() {
		g.Description("Task representing the restart operation")
		g.Meta("struct:tag:json", "task")
	})
	g.Required("task")
})

var StopInstanceResponse = g.Type("StopInstanceResponse", func() {
	g.Description("Response containing the stop task")
	g.Attribute("task", Task, func() {
		g.Description("Task representing the stop operation")
		g.Meta("struct:tag:json", "task")
	})
	g.Required("task")
})

var StartInstanceResponse = g.Type("StartInstanceResponse", func() {
	g.Description("Response containing the start task")
	g.Attribute("task", Task, func() {
		g.Description("Task representing the start operation")
		g.Meta("struct:tag:json", "task")
	})
	g.Required("task")
})

var InstanceConnectionInfo = g.Type("InstanceConnectionInfo", func() {
	g.Description("Connection information for a pgEdge instance.")
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of the host that's running this instance.")
		g.Example("i-0123456789abcdef.ec2.internal")
		g.Meta("struct:tag:json", "hostname,omitempty")
	})
	g.Attribute("ipv4_address", g.String, func() {
		g.Description("The IPv4 address of the host that's running this instance.")
		g.Format(g.FormatIPv4)
		g.Example("10.24.34.2")
		g.Meta("struct:tag:json", "ipv4_address,omitempty")
	})
	g.Attribute("port", g.Int, func() {
		g.Description("The host port that Postgres is listening on for this instance.")
		g.Example(5432)
		g.Meta("struct:tag:json", "port,omitempty")
	})
})

var InstancePostgresStatus = g.Type("InstancePostgresStatus", func() {
	g.Description("Postgres status information for a pgEdge instance.")
	g.Attribute("version", g.String, func() {
		g.Description("The version of Postgres for this instance.")
		g.Example("18.1")
		g.Meta("struct:tag:json", "version,omitempty")
	})
	g.Attribute("patroni_state", g.String, func() {
		g.Example("stopping")
		g.Example("stopped")
		g.Example("stop failed")
		g.Example("crashed")
		g.Example("running")
		g.Example("starting")
		g.Example("start failed")
		g.Example("restarting")
		g.Example("restart failed")
		g.Example("initializing new cluster")
		g.Example("initdb failed")
		g.Example("running custom bootstrap script")
		g.Example("custom bootstrap failed")
		g.Example("creating replica")
		g.Example("unknown")
		g.Meta("struct:tag:json", "patroni_state,omitempty")
	})
	g.Attribute("role", g.String, func() {
		g.Example("replica")
		g.Example("primary")
		g.Meta("struct:tag:json", "role,omitempty")
	})
	g.Attribute("pending_restart", g.Boolean, func() {
		g.Description("True if this instance has a pending restart from a configuration change.")
		g.Meta("struct:tag:json", "pending_restart,omitempty")
	})
	g.Attribute("patroni_paused", g.Boolean, func() {
		g.Description("True if Patroni is paused for this instance.")
		g.Meta("struct:tag:json", "patroni_paused,omitempty")
	})
})

var InstanceSubscription = g.Type("InstanceSubscription", func() {
	g.Description("Status information for a Spock subscription.")
	g.Attribute("provider_node", g.String, func() {
		g.Description("The Spock node name of the provider for this subscription.")
		g.Pattern(nodeNamePattern)
		g.Example("n2")
		g.Meta("struct:tag:json", "provider_node")
	})
	g.Attribute("name", g.String, func() {
		g.Description("The name of the subscription.")
		g.Example("sub_n1n2")
		g.Meta("struct:tag:json", "name")
	})
	g.Attribute("status", g.String, func() {
		g.Description("The current status of the subscription.")
		g.Example("replicating")
		g.Example("down")
		g.Meta("struct:tag:json", "status")
	})

	g.Required("provider_node", "name", "status")
})

var InstanceSpockStatus = g.Type("InstanceSpockStatus", func() {
	g.Description("Spock status information for a pgEdge instance.")
	g.Attribute("read_only", g.String, func() {
		g.Description("The current spock.readonly setting.")
		g.Example("off")
		g.Meta("struct:tag:json", "read_only,omitempty")
	})
	g.Attribute("version", g.String, func() {
		g.Description("The version of Spock for this instance.")
		g.Example("4.10.0")
		g.Meta("struct:tag:json", "version,omitempty")
	})
	g.Attribute("subscriptions", g.ArrayOf(InstanceSubscription), func() {
		g.Description("Status information for this instance's Spock subscriptions.")
		g.Meta("struct:tag:json", "subscriptions,omitempty")
	})
})

var Instance = g.Type("Instance", func() {
	g.Description("An instance of pgEdge Postgres running on a host.")
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the instance.")
		g.Example("a67cbb36-c3c3-49c9-8aac-f4a0438a883d")
		g.Meta("struct:tag:json", "id")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host this instance is running on.")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
		g.Meta("struct:tag:json", "host_id")
	})
	g.Attribute("node_name", g.String, func() {
		g.Description("The Spock node name for this instance.")
		g.Example("n1")
		g.Meta("struct:tag:json", "node_name")
	})
	g.Attribute("created_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance was created.")
		g.Meta("struct:tag:json", "created_at")
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance was last modified.")
		g.Meta("struct:tag:json", "updated_at")
	})
	g.Attribute("status_updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the instance status information was last updated.")
		g.Meta("struct:tag:json", "status_updated_at,omitempty")
	})
	g.Attribute("state", g.String, func() {
		g.Enum(
			"creating",
			"modifying",
			"backing_up",
			"available",
			"degraded",
			"failed",
			"stopped",
			"unknown",
		)
		g.Meta("struct:tag:json", "state")
	})
	g.Attribute("connection_info", InstanceConnectionInfo, func() {
		g.Description("Connection information for the instance.")
		g.Meta("struct:tag:json", "connection_info,omitempty")
	})
	g.Attribute("postgres", InstancePostgresStatus, func() {
		g.Description("Postgres status information for the instance.")
		g.Meta("struct:tag:json", "postgres,omitempty")
	})
	g.Attribute("spock", InstanceSpockStatus, func() {
		g.Description("Spock status information for the instance.")
		g.Meta("struct:tag:json", "spock,omitempty")
	})
	g.Attribute("error", g.String, func() {
		g.Description("An error message if the instance is in an error state.")
		g.Example("failed to get patroni status: connection refused")
		g.Meta("struct:tag:json", "error,omitempty")
	})

	g.Required("id", "host_id", "node_name", "created_at", "updated_at", "state")
})

var PortMapping = g.Type("PortMapping", func() {
	g.Description("Port mapping information for a service instance.")
	g.Attribute("name", g.String, func() {
		g.Description("The name of the port (e.g., 'http', 'web-client').")
		g.Example("http")
		g.Example("web-client")
		g.Meta("struct:tag:json", "name")
	})
	g.Attribute("container_port", g.Int, func() {
		g.Description("The port number inside the container.")
		g.Minimum(1)
		g.Maximum(65535)
		g.Example(8080)
		g.Meta("struct:tag:json", "container_port,omitempty")
	})
	g.Attribute("host_port", g.Int, func() {
		g.Description("The port number on the host (if port-forwarded).")
		g.Minimum(1)
		g.Maximum(65535)
		g.Example(8080)
		g.Meta("struct:tag:json", "host_port,omitempty")
	})

	g.Required("name")
})

var HealthCheckResult = g.Type("HealthCheckResult", func() {
	g.Description("Health check result for a service instance.")
	g.Attribute("status", g.String, func() {
		g.Description("The health status.")
		g.Enum("healthy", "unhealthy", "unknown")
		g.Example("healthy")
		g.Meta("struct:tag:json", "status")
	})
	g.Attribute("message", g.String, func() {
		g.Description("Optional message about the health status.")
		g.Example("Service responding normally")
		g.Example("Connection refused")
		g.Meta("struct:tag:json", "message,omitempty")
	})
	g.Attribute("checked_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time this health check was performed.")
		g.Example("2025-01-28T10:00:00Z")
		g.Meta("struct:tag:json", "checked_at")
	})

	g.Required("status", "checked_at")
})

var ServiceInstanceStatus = g.Type("ServiceInstanceStatus", func() {
	g.Description("Runtime status information for a service instance.")
	g.Attribute("container_id", g.String, func() {
		g.Description("The Docker container ID.")
		g.Example("a1b2c3d4e5f6")
		g.Meta("struct:tag:json", "container_id,omitempty")
	})
	g.Attribute("image_version", g.String, func() {
		g.Description("The container image version currently running.")
		g.Example("1.0.0")
		g.Meta("struct:tag:json", "image_version,omitempty")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of the service instance.")
		g.Example("mcp-server-host-1.internal")
		g.Meta("struct:tag:json", "hostname,omitempty")
	})
	g.Attribute("ipv4_address", g.String, func() {
		g.Description("The IPv4 address of the service instance.")
		g.Format(g.FormatIPv4)
		g.Example("10.0.1.5")
		g.Meta("struct:tag:json", "ipv4_address,omitempty")
	})
	g.Attribute("ports", g.ArrayOf(PortMapping), func() {
		g.Description("Port mappings for this service instance.")
		g.Meta("struct:tag:json", "ports,omitempty")
	})
	g.Attribute("health_check", HealthCheckResult, func() {
		g.Description("Most recent health check result.")
		g.Meta("struct:tag:json", "health_check,omitempty")
	})
	g.Attribute("last_health_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time of the last health check attempt.")
		g.Example("2025-01-28T10:00:00Z")
		g.Meta("struct:tag:json", "last_health_at,omitempty")
	})
	g.Attribute("service_ready", g.Boolean, func() {
		g.Description("Whether the service is ready to accept requests.")
		g.Example(true)
		g.Meta("struct:tag:json", "service_ready,omitempty")
	})
})

var ServiceInstance = g.Type("ServiceInstance", func() {
	g.Description("A service instance running on a host alongside the database.")

	g.Attribute("service_instance_id", g.String, func() {
		g.Description("Unique identifier for the service instance.")
		g.Example("mcp-server-host-1")
		g.Meta("struct:tag:json", "service_instance_id")
	})
	g.Attribute("service_id", g.String, func() {
		g.Description("The service ID from the DatabaseSpec.")
		g.Example("mcp-server")
		g.Meta("struct:tag:json", "service_id")
	})
	g.Attribute("database_id", Identifier, func() {
		g.Description("The ID of the database this service belongs to.")
		g.Example("production")
		g.Meta("struct:tag:json", "database_id")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host this service instance is running on.")
		g.Example("host-1")
		g.Meta("struct:tag:json", "host_id")
	})
	g.Attribute("state", g.String, func() {
		g.Description("Current state of the service instance.")
		g.Enum(
			"creating",
			"running",
			"failed",
			"deleting",
		)
		g.Example("running")
		g.Meta("struct:tag:json", "state")
	})
	g.Attribute("status", ServiceInstanceStatus, func() {
		g.Description("Runtime status information for the service instance.")
		g.Meta("struct:tag:json", "status,omitempty")
	})
	g.Attribute("created_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the service instance was created.")
		g.Example("2025-01-28T10:00:00Z")
		g.Meta("struct:tag:json", "created_at")
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time that the service instance was last updated.")
		g.Example("2025-01-28T10:05:00Z")
		g.Meta("struct:tag:json", "updated_at")
	})
	g.Attribute("error", g.String, func() {
		g.Description("An error message if the service instance is in an error state.")
		g.Example("failed to start container: image not found")
		g.Meta("struct:tag:json", "error,omitempty")
	})

	g.Required("service_instance_id", "service_id", "database_id", "host_id", "state", "created_at", "updated_at")
})
