package design

import (
	g "goa.design/goa/v3/dsl"
)

var CancelDatabaseTaskPayload = g.Type("CancelDatabaseTaskPayload", func() {
	g.Attribute("database_id", Identifier, func() {
		g.Description("The ID of the database that the task belongs to.")
		g.Meta("struct:tag:json", "database_id")
	})
	g.Attribute("task_id", Identifier, func() {
		g.Description("The ID of task to cancel.")
		g.Meta("struct:tag:json", "task_id")
	})
	g.Required("database_id", "task_id")
})
