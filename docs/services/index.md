# Services

The pgEdge Control Plane supports running auxiliary services alongside your databases. Services are containerized applications that attach to a database, run on the same hosts, and connect via automatically-managed database credentials.

## What Are Services?

A service is a containerized application that runs alongside a database. Each service instance runs on a single host and receives its own set of database credentials scoped to that instance.

Supported service types:

- **[MCP (Model Context Protocol)](mcp.md)** — Connects AI agents and LLM-powered applications to your database, enabling natural language queries and AI-powered data access.
- **[PostgREST](postgrest.md)** — *(Coming soon)* Automatically generates a REST API from your PostgreSQL schema, making your data accessible over HTTP without writing backend code.
- **[RAG](rag.md)** — *(Coming soon)* Enables retrieval-augmented generation workflows using your database as a knowledge store.

## Service Instances

When you add a service to a database, the Control Plane creates one service instance per host listed in the service's `host_ids`. Each instance runs as a separate container and receives its own database credentials.

Service instances go through the following lifecycle states:

| State | Description |
|-------|-------------|
| `creating` | The Control Plane is provisioning the service instance. |
| `running` | The service instance is healthy and operational. |
| `failed` | The service instance exited or failed its health check. |
| `deleting` | The Control Plane is removing the service instance. |

## Database Credentials

Each service instance is automatically provisioned with two dedicated database users:

- **Read-only user** (`svc_{service_id}_ro`) — Has read access to the database. This is the default for most service types.
- **Read-write user** (`svc_{service_id}_rw`) — Has read and write access to the database. Used when the service requires read/write access.

These credentials are managed entirely by the Control Plane. You do not need to create or rotate them manually.

## Next Steps

To add a service to a database, see [Managing Services](managing.md). Then refer to the page for your specific service type for configuration details.
