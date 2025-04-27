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
	g.Attribute("extra_options", g.ArrayOf(g.String), func() {
		g.Description("Extra options for the backup.")
		g.Example([]string{"--option1", "--option2"})
	})

	g.Required("type")
})
