package design

import (
	g "goa.design/goa/v3/dsl"
)

var Task = g.Type("Task", func() {
	g.Attribute("parent_id", g.String, func() {
		g.Format(g.FormatUUID)
		g.Description("The parent task ID of the task.")
		g.Example("439eb515-e700-4740-b508-4a3f12ec4f83")
	})
	g.Attribute("scope", g.String, func() {
		g.Enum("database", "host")
		g.Description("The scope of the task (database or host).")
		g.Example("database")
	})
	g.Attribute("entity_id", g.String, func() {
		g.Description("The entity ID (database_id or host_id) that this task belongs to.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("database_id", g.String, func() {
		g.Description("The database ID of the task.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("node_name", g.String, func() {
		g.Description("The name of the node that the task is operating on.")
		g.Example("n1")
	})
	g.Attribute("instance_id", g.String, func() {
		g.Description("The ID of the instance that the task is operating on.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("host_id", g.String, func() {
		g.Description("The ID of the host that the task is running on.")
		g.Example("2e52dcde-86d8-4f71-b58e-8dc3a10c936a")
	})
	g.Attribute("task_id", g.String, func() {
		g.Format(g.FormatUUID)
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
		g.Example("backup", "restore")
	})
	g.Attribute("status", g.String, func() {
		g.Enum("pending", "running", "completed", "canceled", "canceling", "failed", "unknown")
		g.Description("The status of the task.")
		g.Example("pending")
	})
	g.Attribute("error", g.String, func() {
		g.Description("The error message if the task failed.")
		g.Example("failed to connect to database")
	})

	g.Required("scope", "entity_id", "task_id", "created_at", "type", "status")

	g.Example(map[string]any{
		"scope":        "database",
		"entity_id":    "storefront",
		"completed_at": "2025-06-18T16:52:35Z",
		"created_at":   "2025-06-18T16:52:05Z",
		"database_id":  "storefront",
		"status":       "completed",
		"task_id":      "019783f4-75f4-71e7-85a3-c9b96b345d77",
		"type":         "create",
	})
})

var TaskLogEntry = g.Type("TaskLogEntry", func() {
	g.Attribute("timestamp", g.String, func() {
		g.Format(g.FormatDateTime)
		g.Description("The timestamp of the log entry.")
		g.Example("2025-05-29T15:43:13Z")
	})
	g.Attribute("message", g.String, func() {
		g.Description("The log message.")
		g.Example("task started")
	})
	g.Attribute("fields", g.MapOf(g.String, g.Any), func() {
		g.Description("Additional fields for the log entry.")
		g.Example(map[string]any{
			"status":         "creating",
			"option.enabled": true,
		})
	})
	g.Required("timestamp", "message")
})

var TaskLog = g.Type("TaskLog", func() {
	g.Attribute("scope", g.String, func() {
		g.Enum("database", "host")
		g.Description("The scope of the task (database or host).")
		g.Example("database")
	})
	g.Attribute("entity_id", g.String, func() {
		g.Description("The entity ID (database_id or host_id) that this task log belongs to.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("database_id", g.String, func() {
		g.Description("The database ID of the task log. Deprecated: use entity_id instead.")
		g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
	})
	g.Attribute("task_id", g.String, func() {
		g.Description("The unique ID of the task log.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("task_status", g.String, func() {
		g.Enum("pending", "running", "completed", "failed", "unknown", "canceled", "canceling")
		g.Description("The status of the task.")
		g.Example("pending")
	})
	g.Attribute("last_entry_id", g.String, func() {
		g.Description("The ID of the last entry in the task log.")
		g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
	})
	g.Attribute("entries", g.ArrayOf(TaskLogEntry), func() {
		g.Description("Entries in the task log.")
	})

	g.Required("scope", "entity_id", "task_id", "task_status", "entries")

	g.Example("node_backup task log", func() {
		g.Description("The task log from a 'node_backup' task. These messages are produced by pgbackrest.")
		g.Value(map[string]any{
			"scope":     "database",
			"entity_id": "storefront",
			"entries": []map[string]any{
				{
					"message":   "P00   INFO: backup command begin 2.55.1: --config=/opt/pgedge/configs/pgbackrest.backup.conf --exec-id=198-b17fae6e --log-level-console=info --no-log-timestamp --pg1-path=/opt/pgedge/data/pgdata --pg1-user=pgedge --repo1-cipher-type=none --repo1-path=/backups/databases/storefront/n1 --repo1-retention-full=7 --repo1-retention-full-type=time --repo1-type=posix --stanza=db --start-fast --type=full",
					"timestamp": "2025-06-18T17:54:34Z",
				},
				{
					"message":   "P00   INFO: execute non-exclusive backup start: backup begins after the requested immediate checkpoint completes",
					"timestamp": "2025-06-18T17:54:34Z",
				},
				{
					"message":   "P00   INFO: backup start archive = 000000020000000000000004, lsn = 0/4000028",
					"timestamp": "2025-06-18T17:54:34Z",
				},
				{
					"message":   "P00   INFO: check archive for prior segment 000000020000000000000003",
					"timestamp": "2025-06-18T17:54:34Z",
				},
				{
					"message":   "P00   INFO: execute non-exclusive backup stop and wait for all WAL segments to archive",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: backup stop archive = 000000020000000000000004, lsn = 0/4000120",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: check archive for segment(s) 000000020000000000000004:000000020000000000000004",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: new backup label = 20250618-175434F",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: full backup size = 30.6MB, file total = 1342",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: backup command end: completed successfully",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: expire command begin 2.55.1: --config=/opt/pgedge/configs/pgbackrest.backup.conf --exec-id=198-b17fae6e --log-level-console=info --no-log-timestamp --repo1-cipher-type=none --repo1-path=/backups/databases/storefront/n1 --repo1-retention-full=7 --repo1-retention-full-type=time --repo1-type=posix --stanza=db",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: repo1: time-based archive retention not met - archive logs will not be expired",
					"timestamp": "2025-06-18T17:54:36Z",
				},
				{
					"message":   "P00   INFO: expire command end: completed successfully",
					"timestamp": "2025-06-18T17:54:36Z",
				},
			},
			"last_entry_id": "0197842d-b14d-7c69-86c1-c006a7c65318",
			"task_id":       "0197842d-9082-7496-b787-77bd2e11809f",
			"task_status":   "completed",
		})
	})

	g.Example("update task log", func() {
		g.Description("This is the task log of an update task. This example excludes many entries for brevity.")
		g.Value(map[string]any{
			"scope":     "database",
			"entity_id": "storefront",
			"entries": []map[string]any{
				{
					"message":   "refreshing current state",
					"timestamp": "2025-06-18T17:53:19Z",
				},
				{
					"fields": map[string]any{
						"duration_ms": 8972,
					},
					"message":   "finished refreshing current state (took 8.972080116s)",
					"timestamp": "2025-06-18T17:53:28Z",
				},
				{
					"fields": map[string]any{
						"host_id":       "host-1",
						"resource_id":   "storefront-n1-689qacsi-backup",
						"resource_type": "swarm.pgbackrest_config",
					},
					"message":   "creating resource swarm.pgbackrest_config::storefront-n1-689qacsi-backup",
					"timestamp": "2025-06-18T17:53:29Z",
				},
				{
					"fields": map[string]any{
						"host_id":       "host-2",
						"resource_id":   "storefront-n2-9ptayhma-backup",
						"resource_type": "swarm.pgbackrest_config",
					},
					"message":   "creating resource swarm.pgbackrest_config::storefront-n2-9ptayhma-backup",
					"timestamp": "2025-06-18T17:53:29Z",
				},
				{
					"fields": map[string]any{
						"duration_ms":   383,
						"host_id":       "host-3",
						"resource_id":   "n3",
						"resource_type": "swarm.pgbackrest_stanza",
						"success":       true,
					},
					"message":   "finished creating resource swarm.pgbackrest_stanza::n3 (took 383.568613ms)",
					"timestamp": "2025-06-18T17:54:02Z",
				},
				{
					"fields": map[string]any{
						"duration_ms":   1181,
						"host_id":       "host-1",
						"resource_id":   "n1",
						"resource_type": "swarm.pgbackrest_stanza",
						"success":       true,
					},
					"message":   "finished creating resource swarm.pgbackrest_stanza::n1 (took 1.181454868s)",
					"timestamp": "2025-06-18T17:54:03Z",
				},
			},
			"last_entry_id": "0197842d-303b-7251-b814-6d12c98e7d25",
			"task_id":       "0197842c-7c4f-7a8c-829e-7405c2a41c8c",
			"task_status":   "completed",
		})
	})
})

var ListDatabaseTasksResponse = g.Type("ListDatabaseTasksResponse", func() {
	g.Attribute("tasks", g.ArrayOf(Task), func() {
		g.Description("The tasks for the given database.")
	})
	g.Required("tasks")

	g.Example(map[string]any{
		"tasks": []map[string]any{
			{
				"completed_at": "2025-06-18T17:54:36Z",
				"created_at":   "2025-06-18T17:54:28Z",
				"scope":        "database",
				"entity_id":    "storefront",
				"database_id":  "storefront",
				"instance_id":  "storefront-n1-689qacsi",
				"status":       "completed",
				"task_id":      "0197842d-9082-7496-b787-77bd2e11809f",
				"type":         "node_backup",
			},
			{
				"completed_at": "2025-06-18T17:54:04Z",
				"created_at":   "2025-06-18T17:53:17Z",
				"scope":        "database",
				"entity_id":    "storefront",
				"database_id":  "storefront",
				"status":       "completed",
				"task_id":      "0197842c-7c4f-7a8c-829e-7405c2a41c8c",
				"type":         "update",
			},
			{
				"completed_at": "2025-06-18T17:23:28Z",
				"created_at":   "2025-06-18T17:23:14Z",
				"scope":        "database",
				"entity_id":    "storefront",
				"database_id":  "storefront",
				"status":       "completed",
				"task_id":      "01978410-fb5d-7cd2-bbd2-66c0bf929dc0",
				"type":         "update",
			},
			{
				"completed_at": "2025-06-18T16:52:35Z",
				"created_at":   "2025-06-18T16:52:05Z",
				"scope":        "database",
				"entity_id":    "storefront",
				"database_id":  "storefront",
				"status":       "completed",
				"task_id":      "019783f4-75f4-71e7-85a3-c9b96b345d77",
				"type":         "create",
			},
		},
	})
})

var ListHostTasksResponse = g.Type("ListHostTasksResponse", func() {
	g.Attribute("tasks", g.ArrayOf(Task), func() {
		g.Description("The tasks for the given host.")
	})
	g.Required("tasks")

	g.Example(map[string]any{
		"tasks": []map[string]any{
			{
				"completed_at": "2025-06-18T17:54:36Z",
				"created_at":   "2025-06-18T17:54:28Z",
				"scope":        "host",
				"entity_id":    "host-1",
				"host_id":      "host-1",
				"status":       "completed",
				"task_id":      "0197842d-9082-7496-b787-77bd2e11809f",
				"type":         "remove_host",
			},
		},
	})
})

var ListTasksResponse = g.Type("ListTasksResponse", func() {
	g.Attribute("tasks", g.ArrayOf(Task), func() {
		g.Description("The tasks for the given entity.")
	})
	g.Required("tasks")

	g.Example(map[string]any{
		"tasks": []map[string]any{
			{
				"completed_at": "2025-06-18T17:54:36Z",
				"created_at":   "2025-06-18T17:54:28Z",
				"scope":        "database",
				"entity_id":    "storefront",
				"database_id":  "storefront",
				"instance_id":  "storefront-n1-689qacsi",
				"status":       "completed",
				"task_id":      "0197842d-9082-7496-b787-77bd2e11809f",
				"type":         "node_backup",
			},
			{
				"completed_at": "2025-06-18T17:54:36Z",
				"created_at":   "2025-06-18T17:54:28Z",
				"scope":        "host",
				"entity_id":    "host-1",
				"host_id":      "host-1",
				"status":       "completed",
				"task_id":      "0197842d-9082-7496-b787-77bd2e11809f",
				"type":         "remove_host",
			},
		},
	})
})
