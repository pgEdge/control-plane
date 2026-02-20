package design

import (
	g "goa.design/goa/v3/dsl"
)

var ListHostsResponse = g.Type("ListHostsResponse", func() {
	g.Description("Response containing the list of hosts")
	g.Attribute("hosts", g.ArrayOf(Host), func() {
		g.Description("List of hosts in the cluster")
		g.Meta("struct:tag:json", "hosts")
	})
	g.Required("hosts")
})

var ComponentStatus = g.Type("ComponentStatus", func() {
	g.Attribute("healthy", g.Boolean, func() {
		g.Description("Indicates if the component is healthy.")
		g.Example(false)
		g.Meta("struct:tag:json", "healthy")
	})
	g.Attribute("error", g.String, func() {
		g.Description("Error message from any errors that occurred during the health check.")
		g.Example("failed to connect to etcd")
		g.Meta("struct:tag:json", "error,omitempty")
	})
	g.Attribute("details", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional details about the component.")
		g.Example(map[string]any{
			"alarms": []string{"3: NOSPACE"},
		})
		g.Meta("struct:tag:json", "details,omitempty")
	})

	g.Required("healthy")
})

var HostStatus = g.Type("HostStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Enum("healthy", "unreachable", "degraded", "unknown")
		g.Example("available")
		g.Meta("struct:tag:json", "state")
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The last time the host status was updated.")
		g.Example("2021-07-01T12:34:56Z")
		g.Meta("struct:tag:json", "updated_at")
	})
	g.Attribute("components", g.MapOf(g.String, ComponentStatus), func() {
		g.Description("The status of each component of the host.")
		g.Meta("struct:tag:json", "components")
	})

	g.Required("state", "updated_at", "components")
})

var PgEdgeVersion = g.Type("PgEdgeVersion", func() {
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The Postgres major and minor version.")
		g.Example("17.6")
		g.Meta("struct:tag:json", "postgres_version")
	})
	g.Attribute("spock_version", g.String, func() {
		g.Description("The Spock major version.")
		g.Example("5")
		g.Meta("struct:tag:json", "spock_version")
	})

	g.Required("postgres_version", "spock_version")
})

var HostCohort = g.Type("HostCohort", func() {
	g.Attribute("type", g.String, func() {
		g.Description("The type of cohort that the host belongs to.")
		g.Example("swarm")
		g.Meta("struct:tag:json", "type")
	})
	g.Attribute("member_id", g.String, func() {
		g.Description("The member ID of the host within the cohort.")
		g.Example("lah4bsznw6kc0hp7biylmmmll")
		g.Meta("struct:tag:json", "member_id")
	})
	g.Attribute("control_available", g.Boolean, func() {
		g.Description("Indicates if the host is a control node in the cohort.")
		g.Example(true)
		g.Meta("struct:tag:json", "control_available")
	})

	g.Required("type", "member_id", "control_available")
})

var Host = g.Type("Host", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("Unique identifier for the host.")
		g.Example("host-1")
		g.Example("us-east-1")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
		g.Meta("struct:tag:json", "id")
	})
	g.Attribute("orchestrator", g.String, func() {
		g.Description("The orchestrator used by this host.")
		g.Example("swarm")
		g.Meta("struct:tag:json", "orchestrator")
	})
	g.Attribute("data_dir", g.String, func() {
		g.Description("The data directory for the host.")
		g.Example("/data")
		g.Meta("struct:tag:json", "data_dir")
	})
	g.Attribute("cohort", HostCohort, func() {
		g.Description("The cohort that this host belongs to.")
		g.Meta("struct:tag:json", "cohort,omitempty")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of this host.")
		g.Example("i-0123456789abcdef.ec2.internal")
		g.Meta("struct:tag:json", "hostname")
	})
	g.Attribute("ipv4_address", func() {
		g.Description("The IPv4 address of this host.")
		g.Format(g.FormatIPv4)
		g.Example("10.24.34.2")
		g.Meta("struct:tag:json", "ipv4_address")
	})
	g.Attribute("cpus", g.Int, func() {
		g.Description("The number of CPUs on this host.")
		g.Example(4)
		g.Meta("struct:tag:json", "cpus,omitempty")
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory available on this host.")
		g.Example("16GiB")
		g.Meta("struct:tag:json", "memory,omitempty")
	})
	g.Attribute("status", HostStatus, func() {
		g.Description("Current status of the host.")
		g.Meta("struct:tag:json", "status")
	})
	g.Attribute("default_pgedge_version", PgEdgeVersion, func() {
		g.Description("The default PgEdge version for this host.")
		g.Meta("struct:tag:json", "default_pgedge_version,omitempty")
	})
	g.Attribute("supported_pgedge_versions", g.ArrayOf(PgEdgeVersion), func() {
		g.Description("The PgEdge versions supported by this host.")
		g.Meta("struct:tag:json", "supported_pgedge_versions,omitempty")
	})
	g.Attribute("etcd_mode", g.String, func() {
		g.Description("The etcd mode for this host.")
		g.Enum("server", "client")
		g.Example("server")
		g.Meta("struct:tag:json", "etcd_mode,omitempty")
	})

	g.Required(
		"id",
		"orchestrator",
		"data_dir",
		"hostname",
		"ipv4_address",
		"status",
	)
})

var HostsArrayExample = []map[string]any{
	{
		"cohort": map[string]any{
			"cohort_id":         "zdjfu3tfxg1cihv3146ro3hy2",
			"control_available": true,
			"member_id":         "lah4bsznw6kc0hp7biylmmmll",
			"type":              "swarm",
		},
		"cpus": 16,
		"default_pgedge_version": map[string]any{
			"postgres_version": "17.6",
			"spock_version":    "5",
		},
		"hostname":     "i-0123456789abcdef.ec2.internal",
		"id":           "us-east-1",
		"ipv4_address": "10.24.34.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"data_dir":     "/data",
		"etcd_mode":    "server",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17.6",
				"spock_version":    "5",
			},
			{
				"postgres_version": "18.1",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16.10",
				"spock_version":    "5",
			},
		},
	},
	{
		"cohort": map[string]any{
			"cohort_id":         "zdjfu3tfxg1cihv3146ro3hy2",
			"control_available": true,
			"member_id":         "cb88u9jael2psnepep5iuzb4r",
			"type":              "swarm",
		},
		"cpus": 16,
		"default_pgedge_version": map[string]any{
			"postgres_version": "17.6",
			"spock_version":    "5",
		},
		"hostname":     "i-058731542fee493f.ec2.internal",
		"id":           "ap-south-1",
		"ipv4_address": "10.24.35.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"data_dir":     "/data",
		"etcd_mode":    "server",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17.6",
				"spock_version":    "5",
			},
			{
				"postgres_version": "18.1",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16.10",
				"spock_version":    "5",
			},
		},
	},
	{
		"cohort": map[string]any{
			"cohort_id":         "zdjfu3tfxg1cihv3146ro3hy2",
			"control_available": true,
			"member_id":         "u7u9i3nhqunxc4wj577l6ecb0",
			"type":              "swarm",
		},
		"cpus": 16,
		"default_pgedge_version": map[string]any{
			"postgres_version": "17.6",
			"spock_version":    "5",
		},
		"hostname":     "i-494027b7b53f6a23.ec2.internal",
		"id":           "eu-central-1",
		"ipv4_address": "10.24.36.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"data_dir":     "/data",
		"etcd_mode":    "client",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17.6",
				"spock_version":    "5",
			},
			{
				"postgres_version": "18.1",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16.10",
				"spock_version":    "5",
			},
		},
	},
}

var ListHostsResponseExample = map[string]any{
	"hosts": HostsArrayExample,
}

var RemoveHostResponse = g.Type("RemoveHostResponse", func() {
	g.Attribute("task", Task, func() {
		g.Description("The task that tracks the overall host removal operation.")
		g.Meta("struct:tag:json", "task")
	})
	g.Attribute("update_database_tasks", g.ArrayOf(Task), func() {
		g.Description("The tasks that will update databases affected by the host removal.")
		g.Meta("struct:tag:json", "update_database_tasks")
	})

	g.Required("task", "update_database_tasks")
})
