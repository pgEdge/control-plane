package design

import (
	g "goa.design/goa/v3/dsl"
)

var ClusterStatus = g.Type("ClusterStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Description("The current state of the cluster.")
		g.Enum("available", "error")
	})

	g.Required("state")
})

var Cluster = g.Type("Cluster", func() {
	g.Attribute("id", g.String, func() {
		g.Description("Unique identifier for the cluster.")
		g.Example("a67cbb36-c3c3-49c9-8aac-f4a0438a883d")
	})
	g.Attribute("tenant_id", g.String, func() {
		g.Description("Unique identifier for the cluster's owner.")
		g.Example("8210ec10-2dca-406c-ac4a-0661d2189954")
	})
	g.Attribute("status", ClusterStatus, func() {
		g.Description("Current status of the cluster.")
	})
	g.Attribute("hosts", g.ArrayOf(Host), func() {
		g.Description("All of the hosts in the cluster.")
	})

	g.Required("id", "tenant_id", "status", "hosts")
})
