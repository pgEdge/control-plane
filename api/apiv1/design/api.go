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
	g.Description("A distributed application that provides a declarative API to deploy and manage Postgres databases.")
	g.Server("control-plane", func() {
		g.Host("localhost", func() {
			g.URI("http://localhost:3000")
		})
	})
	g.Meta("openapi:operationId", "{method}")
	g.Meta("openapi:json:indent", "  ")

	// Common errors
	g.Error("cluster_already_initialized", APIError)
	g.Error("cluster_not_initialized", APIError)
	g.Error("database_not_modifiable", APIError)
	g.Error("invalid_input", APIError)
	g.Error("invalid_join_token", APIError)
	g.Error("not_found", APIError)
	g.Error("operation_already_in_progress", APIError)
	g.Error("server_error", APIError)
	g.Error("operation_not_supported", APIError)
	g.HTTP(func() {
		g.Response("cluster_already_initialized", http.StatusConflict)
		g.Response("cluster_not_initialized", http.StatusConflict)
		g.Response("database_not_modifiable", http.StatusConflict)
		g.Response("invalid_input", http.StatusBadRequest)
		g.Response("invalid_join_token", http.StatusUnauthorized)
		g.Response("not_found", http.StatusNotFound)
		g.Response("operation_already_in_progress", http.StatusConflict)
		g.Response("server_error", http.StatusInternalServerError)
		g.Response("operation_not_supported", http.StatusBadRequest)
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

		g.Payload(InitClusterRequest)
		g.Result(ClusterJoinToken)
		g.Error("cluster_already_initialized")
		g.Error("operation_not_supported")

		g.HTTP(func() {
			g.GET("/v1/cluster/init")

			g.Param("cluster_id")

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
		g.Result(ListHostsResponse, func() {
			g.Example(ListHostsResponseExample)
		})
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
			g.Attribute("host_id", Identifier, func() {
				g.Description("ID of the host to get.")
				g.Example("host-1")
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
			g.Attribute("host_id", Identifier, func() {
				g.Description("ID of the host to remove.")
				g.Example("host-1")
			})
			g.Attribute("force", g.Boolean, func() {
				g.Description("Force removal even if instances exist or quorum would be violated. " +
					"Use only for disaster recovery when a host is permanently lost. ")
				g.Default(false)
				g.Example(true)
			})

			g.Required("host_id")
		})
		g.Error("cluster_not_initialized")
		g.Error("invalid_input")
		g.Error("not_found")

		g.HTTP(func() {
			g.DELETE("/v1/hosts/{host_id}")
			g.Param("force")

			g.Meta("openapi:tag:Host")
		})
	})

	g.Method("list-databases", func() {
		g.Description("Lists all databases in the cluster.")
		g.Meta("openapi:summary", "List databases")
		g.Result(ListDatabasesResponse)
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
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to get.")
				g.Example("my-app")
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
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to update.")
				g.Example("my-app")
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
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to delete.")
				g.Example("my-app")
			})
			g.Attribute("force", g.Boolean, func() {
				g.Description("Force deletion of a database even in an unmodifiable state")
				g.Default(false)
				g.Example(true)
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
			g.Param("force")

			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("backup-database-node", func() {
		g.Description("Initiates a backup for a database node.")
		g.Meta("openapi:summary", "Backup database node")
		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to back up.")
				g.Example("my-app")
			})
			g.Attribute("node_name", g.String, func() {
				g.Description("Name of the node to back up.")
				g.Pattern(nodeNamePattern)
				g.Example("n1")
			})
			g.Attribute("options", BackupOptions)

			g.Attribute("force", g.Boolean, func() {
				g.Description("Forcibly attempt backup even in unmodifiable state")
				g.Default(false)
				g.Example(true)
			})

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
			g.Param("force")

			g.Meta("openapi:tag:Database")
		})
	})
	g.Method("switchover-database-node", func() {
		g.Description("Performs a planned switchover for a node's primary to a replica candidate.")
		g.Meta("openapi:summary", "Switchover database node")
		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to operate on.")
				g.Example("my-app")
			})
			g.Attribute("node_name", g.String, func() {
				g.Description("Name of the node to operate on.")
				g.Pattern(nodeNamePattern)
				g.Example("n1")
			})
			g.Attribute("candidate_instance_id", g.String, func() {
				g.Description("Optional instance_id for the replica candidate.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi")
			})
			g.Attribute("scheduled_at", g.String, func() {
				g.Description("Optional scheduled time (ISO8601) for the switchover. If absent switchover happens immediately.")
				g.Format(g.FormatDateTime)
				g.Example("2025-09-20T22:00:00+05:30")
			})

			g.Required("database_id", "node_name")
		})
		g.Result(SwitchoverDatabaseNodeResponse)
		g.Error("cluster_not_initialized")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/nodes/{node_name}/switchover")
			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("failover-database-node", func() {
		g.Description("Performs a failover for a node to a replica candidate.")
		g.Meta("openapi:summary", "Failover database node")
		g.Payload(FailoverDatabaseNodeRequest)
		g.Result(FailoverDatabaseNodeResponse)

		g.Error("cluster_not_initialized")
		g.Error("database_not_modifiable")
		g.Error("invalid_input")
		g.Error("not_found")
		g.Error("operation_already_in_progress")

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/nodes/{node_name}/failover")
			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("list-database-tasks", func() {
		g.Description("Lists all tasks for a database.")
		g.Meta("openapi:summary", "List database tasks")
		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to list tasks for.")
				g.Example("my-app")
			})
			g.Attribute("after_task_id", g.String, func() {
				g.Description("ID of the task to start from.")
				g.Format(g.FormatUUID)
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
		g.Result(ListDatabaseTasksResponse)
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
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database the task belongs to.")
				g.Example("my-app")
			})
			g.Attribute("task_id", g.String, func() {
				g.Description("ID of the task to get.")
				g.Format(g.FormatUUID)
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
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to get the task log for.")
				g.Example("my-app")
			})
			g.Attribute("task_id", g.String, func() {
				g.Description("ID of the task to get the log for.")
				g.Format(g.FormatUUID)
				g.Example("3c875a27-f6a6-4c1c-ba5f-6972fb1fc348")
			})
			g.Attribute("after_entry_id", g.String, func() {
				g.Description("ID of the entry to start from.")
				g.Format(g.FormatUUID)
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
		g.Description("Perform an in-place restore of one or more nodes using the given restore configuration.")
		g.Meta("openapi:summary", "Restore database")
		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database to restore.")
				g.Example("my-app")
			})
			g.Attribute("request", RestoreDatabaseRequest)

			g.Attribute("force", g.Boolean, func() {
				g.Description("Force restoration of a database even in an unmodifiable state")
				g.Default(false)
				g.Example(true)
			})

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
			g.Param("force")

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
	g.Method("restart-instance", func() {
		g.Description("Restarts a specific instance within a database. Supports immediate or scheduled restarts.")
		g.Meta("openapi:summary", "Restart a database instance")

		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("The ID of the database that owns the instance.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d")
			})
			g.Attribute("instance_id", Identifier, func() {
				g.Description("The ID of the instance to restart.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi")
			})
			g.Attribute("scheduled_at", g.String, func() {
				g.Format(g.FormatDateTime)
				g.Description("The time at which the restart is scheduled.")
				g.Example("2025-06-18T16:52:05Z")
			})

			g.Required("database_id", "instance_id")
		})

		g.Result(RestartInstanceResponse, func() {
			g.Description("Returns a task representing the restart operation.")
			g.Example(map[string]any{
				"task": map[string]any{
					"created_at":  "2025-06-18T16:52:05Z",
					"database_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493",
					"instance_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493-n1-689qacsi",
					"status":      "pending",
					"task_id":     "019783f4-75f4-71e7-85a3-c9b96b345d77",
					"type":        "restart_instance",
				},
			})
		})
		g.Error("cluster_not_initialized", func() {
			g.Description("The cluster must be initialized before instance operations.")
		})
		g.Error("invalid_input", func() {
			g.Description("The input values are malformed or missing.")
		})
		g.Error("not_found", func() {
			g.Description("The specified database or instance could not be found.")
		})
		g.Error("restart_failed", func() {
			g.Description("Restart operation could not be completed.")
		})

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/instances/{instance_id}/restart")
			g.Param("database_id")
			g.Param("instance_id")
			g.Body(func() {
				g.Attribute("scheduled_at")
			})
			g.Meta("openapi:tag:Database")
		})
	})
	g.Method("stop-instance", func() {
		g.Description("Stops a specific instance within a database. Supports immediate stops.")
		g.Meta("openapi:summary", "Stops a database instance")

		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("The ID of the database that owns the instance.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d")
			})
			g.Attribute("instance_id", Identifier, func() {
				g.Description("The ID of the instance to stop.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi")
			})
			g.Attribute("force", g.Boolean, func() {
				g.Description("Force stopping an instance even if database in an unmodifiable state")
				g.Default(false)
				g.Example(true)
			})

			g.Required("database_id", "instance_id")
		})

		g.Result(StopInstanceResponse, func() {
			g.Description("Returns a task representing the stop operation.")
			g.Example(map[string]any{
				"task": map[string]any{
					"created_at":  "2025-06-18T16:52:05Z",
					"database_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493",
					"instance_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493-n1-689qacsi",
					"status":      "pending",
					"task_id":     "019783f4-75f4-71e7-85a3-c9b96b345d77",
					"type":        "stop_instance",
				},
			})
		})
		g.Error("cluster_not_initialized", func() {
			g.Description("The cluster must be initialized before instance operations.")
		})
		g.Error("invalid_input", func() {
			g.Description("The input values are malformed or missing.")
		})
		g.Error("not_found", func() {
			g.Description("The specified database or instance could not be found.")
		})
		g.Error("stop_failed", func() {
			g.Description("Stop operation could not be completed.")
		})

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/instances/{instance_id}/stop-instance")
			g.Param("force")

			g.Meta("openapi:tag:Database")
		})
	})
	g.Method("start-instance", func() {
		g.Description("Starts a specific instance within a database. Supports immediate starts")
		g.Meta("openapi:summary", "Starts a database instance")

		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("The ID of the database that owns the instance.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d")
			})
			g.Attribute("instance_id", Identifier, func() {
				g.Description("The ID of the instance to start.")
				g.Example("68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi")
			})
			g.Attribute("force", g.Boolean, func() {
				g.Description("Force starting an instance even if database in an unmodifiable state")
				g.Default(false)
				g.Example(true)
			})

			g.Required("database_id", "instance_id")
		})

		g.Result(StartInstanceResponse, func() {
			g.Description("Returns a task representing the start operation.")
			g.Example(map[string]any{
				"task": map[string]any{
					"created_at":  "2025-06-18T16:52:05Z",
					"database_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493",
					"instance_id": "f2f1cae9-6f37-4609-aa98-d0991bff3493-n1-689qacsi",
					"status":      "pending",
					"task_id":     "019783f4-75f4-71e7-85a3-c9b96b345d77",
					"type":        "start_instance",
				},
			})
		})
		g.Error("cluster_not_initialized", func() {
			g.Description("The cluster must be initialized before instance operations.")
		})
		g.Error("invalid_input", func() {
			g.Description("The input values are malformed or missing.")
		})
		g.Error("not_found", func() {
			g.Description("The specified database or instance could not be found.")
		})
		g.Error("start_failed", func() {
			g.Description("Start operation could not be completed.")
		})

		g.HTTP(func() {
			g.POST("/v1/databases/{database_id}/instances/{instance_id}/start-instance")
			g.Param("force")
			g.Meta("openapi:tag:Database")
		})
	})

	g.Method("cancel-database-task", func() {
		g.Description("Cancels a running or pending task for a database.")
		g.Meta("openapi:summary", "Cancel a database task")

		g.Payload(func() {
			g.Attribute("database_id", Identifier, func() {
				g.Description("ID of the database that owns the task.")
				g.Example("abc123")
			})
			g.Attribute("task_id", Identifier, func() {
				g.Description("ID of the task to cancel.")
				g.Example("def456")
			})

			g.Required("database_id", "task_id")
		})

		g.Result(Task, func() {
			g.Description("Returns the canceled task.")
		})

		g.Error("not_found", func() {
			g.Description("The specified database or task could not be found.")
		})
		g.Error("invalid_input", func() {
			g.Description("The input values are malformed or missing.")
		})
		g.Error("cancel_failed", func() {
			g.Description("The task could not be canceled.")
		})

		g.HTTP(func() {
			g.GET("/v1/databases/{database_id}/tasks/{task_id}/cancel")
			g.Meta("openapi:tag:Database")

		})
	})

	// Serves the OpenAPI spec as a static file
	g.Files("/v1/openapi.json", "./gen/http/openapi3.json", func() {
		g.Meta("openapi:generate", "false")
	})
})

var APIError = g.Type("APIError", func() {
	g.Description("A Control Plane API error.")
	g.ErrorName("name", g.String, func() {
		g.Description("The name of the error.")
		g.Example("error_name")
	})
	g.Attribute("message", g.String, func() {
		g.Description("The error message.")
		g.Example("A longer description of the error.")
	})

	g.Required("name", "message")
})
