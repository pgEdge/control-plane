package design

import (
	g "goa.design/goa/v3/dsl"
)

var VersionInfo = g.Type("VersionInfo", func() {
	g.Attribute("version", g.String, func() {
		g.Description("The version of the API server.")
		g.Example("1.0.0")
	})
	g.Attribute("revision", g.String, func() {
		g.Description("The VCS revision of the API server.")
		g.Example("abc123")
	})
	g.Attribute("revision_time", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The timestamp associated with the revision.")
		g.Example("2025-01-01T01:30:00Z")
	})
	g.Attribute("arch", g.String, func() {
		g.Description("The CPU architecture of the API server.")
		g.Example("amd64")
	})

	g.Required("version", "revision", "revision_time", "arch")
})
