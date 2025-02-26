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
	g.HTTP(func() {
		g.Response("cluster_already_initialized", http.StatusConflict)
		g.Response("cluster_not_initialized", http.StatusConflict)
		g.Response("invalid_join_token", http.StatusUnauthorized)
		g.Response("invalid_input", http.StatusBadRequest)
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

	// g.Method("init-cluster", func() {
	// 	g.Meta()
	// 	g.Description("Initializes a new cluster.")
	// 	g.Result(ClusterJoinToken)

	// 	g.HTTP(func() {
	// 		g.GET("/cluster/init")
	// 	})
	// })

	g.Method("inspect-cluster", func() {
		g.Description("Returns information about the cluster.")
		g.Result(Cluster)
		g.Error("cluster_not_initialized")

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
			g.Attribute("request", UpdateDatabaseRequest)
		})
		g.Result(Database, func() {
			g.View("default")
		})
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.POST("/databases/{database_id}")
			g.Body("request")
		})
	})

	g.Method("delete-database", func() {
		g.Description("Deletes a database from the cluster.")
		g.Payload(func() {
			g.Attribute("database_id", g.String, func() {
				g.Description("ID of the database to delete.")
				g.Example("02f1a7db-fca8-4521-b57a-2a375c1ced51")
			})
		})
		g.Error("cluster_not_initialized")

		g.HTTP(func() {
			g.DELETE("/databases/{database_id}")
		})
	})

	// Serves the OpenAPI spec as a static file
	g.Files("/openapi.json", "./gen/http/openapi3.json")
})
