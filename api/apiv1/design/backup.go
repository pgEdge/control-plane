package design

import (
	g "goa.design/goa/v3/dsl"
)

var BackupOptions = g.Type("BackupOptions", func() {
	g.Attribute("type", g.String, func() {
		g.Enum("full", "diff", "incr")
		g.Description("The type of backup.")
		g.Example("full")
		g.Meta("struct:tag:json", "type")
	})
	g.Attribute("annotations", g.MapOf(g.String, g.String), func() {
		g.Description("Annotations for the backup.")
		g.Example(map[string]string{
			"key": "value",
		})
		g.Meta("struct:tag:json", "annotations,omitempty")
	})
	g.Attribute("backup_options", g.MapOf(g.String, g.String), func() {
		g.Description("Options for the backup.")
		g.Example(map[string]string{
			"archive-check": "n",
		})
		g.Meta("struct:tag:json", "backup_options,omitempty")
	})

	g.Required("type")

	g.Example("Full backup", func() {
		g.Description("Example of taking a full backup.")
		g.Value(map[string]any{
			"type": "full",
		})
	})

	g.Example("Backup with annotations and options", func() {
		g.Description("Example of taking backup with annotations and additional backup options.")
		g.Value(map[string]any{
			"type": "full",
			"annotations": map[string]string{
				"initiated-by": "backup-cron-job",
			},
			"backup_options": map[string]string{
				"archive-check": "n",
			},
		})
	})
})
