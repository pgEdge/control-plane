package design

import (
	g "goa.design/goa/v3/dsl"
)

var CancelDatabaseTaskPayload = g.Type("CancelDatabaseTaskPayload", func() {
	g.Attribute("database_id", Identifier)
	g.Attribute("task_id", Identifier)
	g.Required("database_id", "task_id")
})
