package design

import (
	g "goa.design/goa/v3/dsl"
)

var HostStatus = g.Type("HostStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Enum("available", "unreachable", "error")
		g.Example("available")
	})

	g.Required("state")
})

var HostConfiguration = g.Type("HostConfiguration", func() {
	g.Attribute("vector_enabled", g.Boolean, func() {
		g.Description("Enables the Vector service for metrics and log collection")
	})
	g.Attribute("traefik_enabled", g.Boolean, func() {
		g.Description("Enables the Treafik load balancer")
	})
})

var Host = g.Type("Host", func() {
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the host")
		g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of this host")
		g.Enum("swarm", "systemd")
	})
	g.Attribute("cohort", g.String, func() {
		g.Description("The cohort that this host belongs to")
		g.Example("pps1n11hqijn9rbee4cjil453")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of this host.")
		g.Example("i-0123456789abcdef.ec2.internal")
	})
	g.Attribute("ipv4_address", func() {
		g.Description("The IPv4 address of this host.")
		g.Format(g.FormatIPv4)
		g.Example("10.24.34.0")
	})
	g.Attribute("config", HostConfiguration, func() {
		g.Description("The configuration for this host")
	})
	g.Attribute("status", HostStatus, func() {
		g.Description("Current status of the host")
	})

	g.Required("id", "status", "hostname", "ipv4_address")
})
