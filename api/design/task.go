package design

import (
	g "goa.design/goa/v3/dsl"
)

var Task = g.Type("Task", func() {
	g.Attribute("database_id", g.String, func() {
		g.Description("The database ID of the task.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("task_id", g.String, func() {
		g.Description("The unique ID of the task.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("created_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time when the task was created.")
		g.Example("2025-01-01T01:30:00Z")
	})
	g.Attribute("completed_at", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The time when the task was completed.")
		g.Example("2025-01-01T02:30:00Z")
	})
	g.Attribute("type", g.String, func() {
		g.Description("The type of the task.")
		g.Example("backup")
	})
	g.Attribute("status", g.String, func() {
		g.Enum("pending", "running", "completed", "failed", "unknown")
		g.Description("The status of the task.")
		g.Example("pending")
	})
	g.Attribute("error", g.String, func() {
		g.Description("The error message if the task failed.")
		g.Example("failed to connect to database")
	})

	g.Required("database_id", "task_id", "created_at", "type", "status")
})

var TaskLog = g.Type("TaskLog", func() {
	g.Attribute("database_id", g.String, func() {
		g.Description("The database ID of the task log.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("task_id", g.String, func() {
		g.Description("The unique ID of the task log.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("task_status", g.String, func() {
		g.Enum("pending", "running", "completed", "failed", "unknown")
		g.Description("The status of the task.")
		g.Example("pending")
	})
	g.Attribute("last_line_id", g.String, func() {
		g.Description("The ID of the last line in the task log.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("lines", g.ArrayOf(g.String), func() {
		g.Description("The lines of the task log.")
		g.Example([]string{"Task started", "Task completed"})
	})

	g.Required("database_id", "task_id", "task_status", "lines")
})
