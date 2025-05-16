package design

import (
	"net/http"

	g "goa.design/goa/v3/dsl"
)

var _ = g.API("control-plane", func() {
	g.Title("pgEdge Control Plane API")
	g.Description("Service for creating, modifying, and operating pgEdge databases.")
	g.Server("control-plane", func() {
		g.Host("localhost", func() {
			g.URI("http://localhost:3000")
		})
	})
	g.Meta("openapi:operationId", "{method}")

	// Common errors
	g.Error("cluster_already_initialized")
	g.Error("cluster_not_initialized")
	g.Error("invalid_join_token")
	g.Error("invalid_input")
	g.Error("not_found")
	g.Error("database_not_modifiable")
	g.HTTP(func() {
		g.Response("cluster_already_initialized", http.StatusConflict)
		g.Response("cluster_not_initialized", http.StatusConflict)
		g.Response("invalid_join_token", http.StatusUnauthorized)
		g.Response("invalid_input", http.StatusBadRequest)
		g.Response("not_found", http.StatusNotFound)
		g.Response("database_not_modifiable", http.StatusConflict)
	})
})

var _ = g.Service("control-plane", func() {
	g.Method("init-cluster", func() {
		g.Description("Initializes a new cluster.")
		g.Result(ClusterJoinToken)
		g.Error("cluster_already_initialized")

		g.HTTP(func() {
			g.GET("/cluster/init")
		})
	})

	g.Method("join-cluster", func() {
		g.Description("Join this host to an existing cluster.")
		g.Payload(ClusterJoinToken)
		g.Error("cluster_already_initialized")

		g.HTTP(func() {
			g.POST("/cluster/join")
		})
	})

	g.Method("get-join-token", func() {
		g.Description("Gets the join token for this cluster.")
		g.Result(ClusterJoinToken)
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/cluster/join-token")
		})
	})

	g.Method("get-join-options", func() {
		g.Meta("openapi:generate", "false") // This is an internal operation
		g.Description("Internal endpoint for other cluster members seeking to join this cluster.")
		g.Result(ClusterJoinOptions)
		g.Payload(ClusterJoinRequest)
		g.Error("cluster_not_initialized")
		g.Error("invalid_join_token")

		g.HTTP(func() {
			g.POST("/internal/cluster/join-options")
		})
	})

	g.Method("inspect-cluster", func() {
		g.Description("Returns information about the cluster.")
		g.Result(Cluster)
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/cluster")
		})
	})

	g.Method("list-hosts", func() {
		g.Description("Lists all hosts within the cluster.")
		g.Result(g.ArrayOf(Host))
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/hosts")
		})
	})

	g.Method("inspect-host", func() {
		g.Description("Returns information about a particular host in the cluster.")
		g.Payload(func() {
			g.Attribute("host_id", g.String, func() {
				g.Description("ID of the host to inspect.")
				g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
			})
		})
		g.Result(Host)
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/hosts/{host_id}")
		})
	})

	g.Method("remove-host", func() {
		g.Description("Removes a host from the cluster.")
		g.Payload(func() {
			g.Attribute("host_id", g.String, func() {
				g.Description("ID of the host to remove.")
				g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
			})
		})
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.DELETE("/hosts/{host_id}")
		})
	})

	g.Method("list-databases", func() {
		g.Description("Lists all databases in the cluster.")
		g.Result(g.CollectionOf(Database), func() {
			g.View("abbreviated")
		})
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/databases")
		})
	})

	g.Method("create-database", func() {
		g.Description("Creates a new database in the cluster.")
		g.Payload(CreateDatabaseRequest)
		g.Result(Database, func() {
			g.View("default")
		})
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("database_already_exists")

		g.HTTP(func() {
			g.POST("/databases")
			g.Response("database_already_exists", http.StatusConflict)
		})
	})

	g.Method("inspect-database", func() {
		g.Description("Returns information about a particular database in the cluster.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Description("ID of the database to inspect.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
		})
		g.Result(Database, func() {
			g.View("default")
		})
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/databases/{database_id}")
		})
	})

	g.Method("update-database", func() {
		g.Description("Updates a database with the given specification.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Description("ID of the database to update.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("force_update", g.Boolean, func() {
				g.Description("Force update the database even if the spec is the same.")
				g.Example(true)
			})
			g.Attribute("request", UpdateDatabaseRequest)
		})
		g.Result(Database, func() {
			g.View("default")
		})
		g.Error("cluster_not_initialized")
		g.Error("not_found")
		g.Error("database_not_modifiable")

		g.HTTP(func() {
			g.POST("/databases/{database_id}")
			g.Param("force_update")
			g.Body("request")
		})
	})

	g.Method("delete-database", func() {
		g.Description("Deletes a database from the cluster.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to delete.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})

			g.Required("database_id")
		})
		g.Error("cluster_not_initialized")
		g.Error("not_found")
		g.Error("database_not_modifiable")

		g.HTTP(func() {
			g.DELETE("/databases/{database_id}")
		})
	})

	g.Method("initiate-database-backup", func() {
		g.Description("Initiates a backup for a database.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to back up.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("node_name", g.String, func() {
				g.Description("Name of the node to back up.")
				g.Example("n1")
			})
			g.Attribute("options", BackupOptions)

			g.Required("database_id", "node_name", "options")
		})
		g.Result(Task)
		g.Error("cluster_not_initialized")
		g.Error("not_found")
		g.Error("database_not_modifiable")
		g.Error("backup_already_in_progress")

		g.HTTP(func() {
			g.POST("/databases/{database_id}/nodes/{node_name}/backups")
			g.Body("options")
			g.Response("backup_already_in_progress", http.StatusConflict)
		})
	})

	g.Method("list-database-tasks", func() {
		g.Description("Lists all tasks for a database.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to list tasks for.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("after_task_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the task to start from.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})
			g.Attribute("limit", g.Int, func() {
				g.Description("Maximum number of tasks to return.")
				g.Example(100)
			})
			g.Attribute("sort_order", g.String, func() {
				g.Enum("asc", "ascend", "ascending", "desc", "descend", "descending")
				g.Description("Sort order for the tasks.")
				g.Example("ascend")
			})

			g.Required("database_id")
		})
		g.Result(g.ArrayOf(Task))
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/databases/{database_id}/tasks")
			g.Param("after_task_id")
			g.Param("limit")
			g.Param("sort_order")
		})
	})

	g.Method("inspect-database-task", func() {
		g.Description("Returns information about a particular task for a database.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to inspect tasks for.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("task_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the task to inspect.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})

			g.Required("database_id", "task_id")
		})
		g.Result(Task)
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/databases/{database_id}/tasks/{task_id}")
		})
	})

	g.Method("get-database-task-log", func() {
		g.Description("Returns the log of a particular task for a database.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to get task log for.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("task_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the task to get log for.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})
			g.Attribute("after_line_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the line to start from.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})
			g.Attribute("limit", g.Int, func() {
				g.Description("Maximum number of lines to return.")
				g.Example(100)
			})

			g.Required("database_id", "task_id")
		})
		g.Result(TaskLog)
		g.Error("cluster_not_initialized")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/databases/{database_id}/tasks/{task_id}/log")
			g.Param("after_line_id")
			g.Param("limit")
		})
	})

	g.Method("restore-database", func() {
		g.Description("Perform an in-place restore one or more nodes using the given restore configuration.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to restore.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("request", RestoreDatabaseRequest)

			g.Required("database_id", "request")
		})
		g.Result(RestoreDatabaseResponse)
		g.Error("cluster_not_initialized")
		g.Error("not_found")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")

		g.HTTP(func() {
			g.POST("/databases/{database_id}/restore")
			g.Body("request")
		})
	})

	// Serves the OpenAPI spec as a static file
	g.Files("/openapi.json", "./gen/http/openapi3.json")
})
