package design

import (
	g "goa.design/goa/v3/dsl"
)

var BackupOptions = g.Type("BackupOptions", func() {
	g.Attribute("type", g.String, func() {
		g.Enum("full", "diff", "incr")
		g.Description("The type of backup.")
		g.Example("full")
	})
	g.Attribute("annotations", g.MapOf(g.String, g.String), func() {
		g.Description("Annotations for the backup.")
		g.Example(map[string]string{
			"key": "value",
		})
	})
	g.Attribute("backup_options", g.MapOf(g.String, g.String), func() {
		g.Description("Options for the backup.")
		g.Example(map[string]string{
			"archive-check": "n",
		})
	})

	g.Required("type")
})
