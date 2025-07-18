package design

import (
	g "goa.design/goa/v3/dsl"
)

var ComponentStatus = g.Type("ComponentStatus", func() {
	g.Attribute("healthy", g.Boolean, func() {
		g.Description("Indicates if the component is healthy.")
		g.Example(false)
	})
	g.Attribute("error", g.String, func() {
		g.Description("Error message from any errors that occurred during the health check.")
		g.Example("failed to connect to etcd")
	})
	g.Attribute("details", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional details about the component.")
		g.Example(map[string]any{
			"alarms": []string{"3: NOSPACE"},
		})
	})

	g.Required("healthy")
})

var HostStatus = g.Type("HostStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Enum("healthy", "unreachable", "degraded", "unknown")
		g.Example("available")
	})
	g.Attribute("updated_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The last time the host status was updated.")
		g.Example("2021-07-01T12:34:56Z")
	})
	g.Attribute("components", g.MapOf(g.String, ComponentStatus), func() {
		g.Description("The status of each component of the host.")
	})

	g.Required("state", "updated_at", "components")
})

var PgEdgeVersion = g.Type("PgEdgeVersion", func() {
	g.Attribute("postgres_version", g.String, func() {
		g.Description("The Postgres major version.")
		g.Example("16")
	})
	g.Attribute("spock_version", g.String, func() {
		g.Description("The Spock major version.")
		g.Example("5")
	})

	g.Required("postgres_version", "spock_version")
})

var HostCohort = g.Type("HostCohort", func() {
	g.Attribute("type", g.String, func() {
		g.Description("The type of cohort that the host belongs to.")
		g.Example("swarm")
	})
	g.Attribute("cohort_id", g.String, func() {
		g.Description("The cohort ID that the host belongs to.")
		g.Example("pps1n11hqijn9rbee4cjil453")
	})
	g.Attribute("member_id", g.String, func() {
		g.Description("The member ID of the host within the cohort.")
		g.Example("lah4bsznw6kc0hp7biylmmmll")
	})
	g.Attribute("control_available", g.Boolean, func() {
		g.Description("Indicates if the host is a control node in the cohort.")
		g.Example(true)
	})

	g.Required("type", "cohort_id", "member_id", "control_available")
})

var Host = g.Type("Host", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("Unique identifier for the host.")
		g.Example("host-1")
		g.Example("us-east-1")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
	})
	g.Attribute("orchestrator", g.String, func() {
		g.Description("The orchestrator used by this host.")
		g.Example("swarm")
	})
	g.Attribute("cohort", HostCohort, func() {
		g.Description("The cohort that this host belongs to.")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of this host.")
		g.Example("i-0123456789abcdef.ec2.internal")
	})
	g.Attribute("ipv4_address", func() {
		g.Description("The IPv4 address of this host.")
		g.Format(g.FormatIPv4)
		g.Example("10.24.34.2")
	})
	g.Attribute("cpus", g.Int, func() {
		g.Description("The number of CPUs on this host.")
		g.Example(4)
	})
	g.Attribute("memory", g.String, func() {
		g.Description("The amount of memory available on this host.")
		g.Example("16GiB")
	})
	g.Attribute("status", HostStatus, func() {
		g.Description("Current status of the host.")
	})
	g.Attribute("default_pgedge_version", PgEdgeVersion, func() {
		g.Description("The default PgEdge version for this host.")
	})
	g.Attribute("supported_pgedge_versions", g.ArrayOf(PgEdgeVersion), func() {
		g.Description("The PgEdge versions supported by this host.")
	})

	g.Required(
		"id",
		"orchestrator",
		"hostname",
		"ipv4_address",
		"status",
	)
})

var HostsExample = []map[string]any{
	{
		"cohort": map[string]any{
			"cohort_id":         "zdjfu3tfxg1cihv3146ro3hy2",
			"control_available": true,
			"member_id":         "lah4bsznw6kc0hp7biylmmmll",
			"type":              "swarm",
		},
		"cpus": 16,
		"default_pgedge_version": map[string]any{
			"postgres_version": "17",
			"spock_version":    "5",
		},
		"hostname":     "i-0123456789abcdef.ec2.internal",
		"id":           "us-east-1",
		"ipv4_address": "10.24.34.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16",
				"spock_version":    "5",
			},
			{
				"postgres_version": "15",
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
			"postgres_version": "17.0.0",
			"spock_version":    "4.0.0",
		},
		"hostname":     "i-058731542fee493f.ec2.internal",
		"id":           "ap-south-1",
		"ipv4_address": "10.24.35.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16",
				"spock_version":    "5",
			},
			{
				"postgres_version": "15",
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
			"postgres_version": "17",
			"spock_version":    "5",
		},
		"hostname":     "i-494027b7b53f6a23.ec2.internal",
		"id":           "eu-central-1",
		"ipv4_address": "10.24.36.2",
		"memory":       "16GB",
		"orchestrator": "swarm",
		"status": map[string]any{
			"components": map[string]any{},
			"state":      "healthy",
			"updated_at": "2025-06-17T00:00:00Z",
		},
		"supported_pgedge_versions": []map[string]any{
			{
				"postgres_version": "17",
				"spock_version":    "5",
			},
			{
				"postgres_version": "16",
				"spock_version":    "5",
			},
			{
				"postgres_version": "15",
				"spock_version":    "5",
			},
		},
	},
}
