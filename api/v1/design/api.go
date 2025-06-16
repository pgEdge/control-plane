package design

import (
	_ "embed"
	"net/http"

	g "goa.design/goa/v3/dsl"

	"github.com/pgEdge/control-plane/api"
)

var _ = g.API("control-plane", func() {
	g.Title("pgEdge Control Plane v1 API")
	g.Version(api.Version)
	g.Description("Service for creating, modifying, and operating pgEdge databases.")
	g.Server("control-plane", func() {
		g.Host("localhost", func() {
			g.URI("http://localhost:3000")
		})
	})
	g.Meta("openapi:operationId", "{method}")

	// Common errors
	g.Error("cluster_already_initialized", APIError)
	g.Error("cluster_not_initialized", APIError)
	g.Error("database_not_modifiable", APIError)
	g.Error("invalid_input", APIError)
	g.Error("invalid_join_token", APIError)
	g.Error("not_found", APIError)
	g.Error("operation_already_in_progress", APIError)
	g.Error("server_error", APIError)
	g.HTTP(func() {
		g.Response("cluster_already_initialized", http.StatusConflict)
		g.Response("cluster_not_initialized", http.StatusConflict)
		g.Response("database_not_modifiable", http.StatusConflict)
		g.Response("invalid_input", http.StatusBadRequest)
		g.Response("invalid_join_token", http.StatusUnauthorized)
		g.Response("not_found", http.StatusNotFound)
		g.Response("operation_already_in_progress", http.StatusConflict)
		g.Response("server_error", http.StatusInternalServerError)
	})
})

var _ = g.Service("control-plane", func() {
	g.HTTP(func() {
		g.Meta("openapi:tag:Cluster:name", "Cluster")
		g.Meta("openapi:tag:Cluster:x-displayName", "Cluster")
		g.Meta("openapi:tag:Cluster:description", "Cluster operations")

		g.Meta("openapi:tag:Host:name", "Host")
		g.Meta("openapi:tag:Host:x-displayName", "Hosts")
		g.Meta("openapi:tag:Host:description", "Host operations")

		g.Meta("openapi:tag:Database:name", "Database")
		g.Meta("openapi:tag:Database:x-displayName", "Databases")
		g.Meta("openapi:tag:Database:description", "Database operations")

		g.Meta("openapi:tag:System:name", "System")
		g.Meta("openapi:tag:System:x-displayName", "System")
		g.Meta("openapi:tag:System:description", "System operations")
	})

	g.Error("server_error")

	g.Method("init-cluster", func() {
		g.Description("Initializes a new cluster.")
		g.Meta("openapi:summary", "Initialize cluster")
		g.Result(ClusterJoinToken)
		g.Error("cluster_already_initialized")

		g.HTTP(func() {
			g.GET("/v1/cluster/init")

			g.Meta("openapi:tag:Cluster")
		})
	})

	g.Method("join-cluster", func() {
		g.Description("Joins this host to an existing cluster.")
		g.Meta("openapi:summary", "Join cluster")
		g.Payload(ClusterJoinToken)
		g.Error("cluster_already_initialized")
		g.Error("invalid_join_token")

		g.HTTP(func() {
			g.POST("/v1/cluster/join")

			g.Meta("openapi:tag:Cluster")
		})
	})

	g.Method("get-join-token", func() {
		g.Description("Gets the join token for this cluster.")
		g.Meta("openapi:summary", "Get cluster join token")
		g.Result(ClusterJoinToken)
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/v1/cluster/join-token")

			g.Meta("openapi:tag:Cluster")
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
			g.POST("/v1/internal/cluster/join-options")
		})
	})

	g.Method("get-cluster", func() {
		g.Description("Returns information about the cluster.")
		g.Meta("openapi:summary", "Get cluster")
		g.Result(Cluster)
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/v1/cluster")

			g.Meta("openapi:tag:Cluster")
		})
	})

	g.Method("list-hosts", func() {
		g.Description("Lists all hosts within the cluster.")
		g.Meta("openapi:summary", "List hosts")
		g.Result(g.ArrayOf(Host))
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/v1/hosts")

			g.Meta("openapi:tag:Host")
		})
	})

	g.Method("get-host", func() {
		g.Description("Returns information about a particular host in the cluster.")
		g.Meta("openapi:summary", "Get host")
		g.Payload(func() {
			g.Attribute("host_id", g.String, func() {
				g.Description("ID of the host to get.")
				g.Format(g.FormatUUID)
				g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
			})

			g.Required("host_id")
		})
		g.Result(Host)
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/v1/hosts/{host_id}")

			g.Meta("openapi:tag:Host")
		})
	})

	g.Method("remove-host", func() {
		g.Description("Removes a host from the cluster.")
		g.Meta("openapi:summary", "Remove host")
		g.Payload(func() {
			g.Attribute("host_id", g.String, func() {
				g.Description("ID of the host to remove.")
				g.Format(g.FormatUUID)
				g.Example("de3b1388-1f0c-42f1-a86c-59ab72f255ec")
			})

			g.Required("host_id")
		})
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.DELETE("/v1/hosts/{host_id}")

			g.Meta("openapi:tag:Host")
		})
	})

	g.Method("list-databases", func() {
		g.Description("Lists all databases in the cluster.")
		g.Meta("openapi:summary", "List databases")
		g.Result(g.CollectionOf(Database), func() {
			g.View("abbreviated")
		})
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.GET("/v1/databases")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("create-database", func() {
		g.Description("Creates a new database in the cluster.")
		g.Meta("openapi:summary", "Create database")
		g.Payload(CreateDatabaseRequest)
		g.Result(CreateDatabaseResponse)
		g.Error("cluster_not_initialized")
		g.Error("database_already_exists", APIError)
		g.Error("invalid_input")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases")
			g.Response("database_already_exists", http.StatusConflict)

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("get-database", func() {
		g.Description("Returns information about a particular database in the cluster.")
		g.Meta("openapi:summary", "Get database")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Description("ID of the database to get.")
				g.Format(g.FormatUUID)
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})

			g.Required("database_id")
		})
		g.Result(Database, func() {
			g.View("default")
		})
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/v1/databases/{database_id}")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("update-database", func() {
		g.Description("Updates a database with the given specification.")
		g.Meta("openapi:summary", "Update database")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Description("ID of the database to update.")
				g.Format(g.FormatUUID)
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("force_update", g.Boolean, func() {
				g.Description("Force update the database even if the spec is the same.")
				g.Default(false)
				g.Example(true)
			})
			g.Attribute("request", UpdateDatabaseRequest)

			g.Required("database_id", "request")
		})
		g.Result(UpdateDatabaseResponse)
		g.Error("cluster_not_initialized")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}")
			g.Param("force_update")
			g.Body("request")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("delete-database", func() {
		g.Description("Deletes a database from the cluster.")
		g.Meta("openapi:summary", "Delete database")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to delete.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})

			g.Required("database_id")
		})
		g.Result(DeleteDatabaseResponse)
		g.Error("cluster_not_initialized")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.DELETE("/v1/databases/{database_id}")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("backup-database-node", func() {
		g.Description("Initiates a backup for a database node.")
		g.Meta("openapi:summary", "Backup database node")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database to back up.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("node_name", g.String, func() {
				g.Description("Name of the node to back up.")
				g.Pattern(nodeNamePattern)
				g.Example("n1")
			})
			g.Attribute("options", BackupOptions)

			g.Required("database_id", "node_name", "options")
		})
		g.Result(BackupDatabaseNodeResponse)
		g.Error("cluster_not_initialized")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/nodes/{node_name}/backups")
			g.Body("options")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("list-database-tasks", func() {
		g.Description("Lists all tasks for a database.")
		g.Meta("openapi:summary", "List database tasks")
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
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/v1/databases/{database_id}/tasks")
			g.Param("after_task_id")
			g.Param("limit")
			g.Param("sort_order")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("get-database-task", func() {
		g.Description("Returns information about a particular task.")
		g.Meta("openapi:summary", "Get database task")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the database the task belongs to.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
			g.Attribute("task_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the task to get.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})

			g.Required("database_id", "task_id")
		})
		g.Result(Task)
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/v1/databases/{database_id}/tasks/{task_id}")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("get-database-task-log", func() {
		g.Description("Returns the log of a particular task for a database.")
		g.Meta("openapi:summary", "Get database task log")
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
			g.Attribute("after_entry_id", g.String, func() {
				g.Format(g.FormatUUID)
				g.Description("ID of the entry to start from.")
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})
			g.Attribute("limit", g.Int, func() {
				g.Description("Maximum number of entries to return.")
				g.Example(100)
			})

			g.Required("database_id", "task_id")
		})
		g.Result(TaskLog)
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.GET("/v1/databases/{database_id}/tasks/{task_id}/log")
			g.Param("after_entry_id")
			g.Param("limit")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("restore-database", func() {
		g.Description("Perform an in-place restore one or more nodes using the given restore configuration.")
		g.Meta("openapi:summary", "Restore database")
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
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/restore")
			g.Body("request")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("get-version", func() {
		g.Description("Returns version information for this Control Plane server.")
		g.Meta("openapi:summary", "Get version")
		g.Result(VersionInfo)

		g.HTTP(func() {
			g.GET("/v1/version")

			g.Meta("openapi:tag:System")
		})
	})

	// Serves the OpenAPI spec as a static file
	g.Files("/v1/openapi.json", "./gen/http/openapi3.json", func() {
		g.Meta("openapi:generate", "false")
	})
})

var APIError = g.Type("APIError", func() {
	g.Description("A Control Plane API error.")
	g.ErrorName("name", g.String, "The name of the error.")
	g.Attribute("message", g.String, "The error message.")

	g.Required("name", "message")
})
