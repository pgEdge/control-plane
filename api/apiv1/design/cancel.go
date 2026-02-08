package design

import (
	g "goa.design/goa/v3/dsl"
)

var CancelDatabaseTaskPayload = g.Type("CancelDatabaseTaskPayload", func() {
	g.Attribute("database_id", Identifier, func() {
		g.Description("The ID of the database that the task belongs to.")
	})
	g.Attribute("task_id", Identifier, func() {
		g.Description("The ID of task to cancel.")
	})
	g.Required("database_id", "task_id")
})
