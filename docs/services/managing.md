# Managing Services

Services are declared as part of your database spec. You add, update,
and remove services by modifying the `services` array in a
`create_database` or `update_database` request. See the
[Services Overview](index.md) for a conceptual introduction.

## Service Spec Fields

Each service in the `services` array is declared using a service spec.
The following table describes the fields in a service spec:

| Field | Type | Required | Description                                                                                                                                                     |
|-------|------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `service_id` | string | Yes | A unique identifier for this service within the database.                                                                                                       |
| `service_type` | string | Yes | The type of service to run. One of: `mcp`, `rag`, `postgrest`.                                                                                                   |
| `version` | string | Yes | The service version in semver format (e.g., `1.0.0`) or the literal `latest`.                                                                                   |
| `host_ids` | array | Yes | The IDs of the hosts to run this service on. One instance is created per host.                                                                                  |
| `config` | object | No | Service-type-specific configuration. See the page for your service type for valid fields. When omitted, the service uses sensible defaults.                     |
| `port` | integer | No | Host port to publish the service on. Set to `0` to let Docker assign a random port. When omitted, the service is not reachable from outside the Docker network. |
| `cpus` | string | No | CPU limit for the service container. Accepts a decimal (e.g., `"0.5"`) or millicpu suffix (e.g., `"500m"`). Defaults to container defaults if unspecified.      |
| `memory` | string | No | Memory limit for the service container in SI or IEC notation (e.g., `"512M"`, `"1GiB"`). Defaults to container defaults if unspecified.                        |
| `connect_as` | string | Yes | Username of the `database_users` entry the service connects to Postgres as. Must exist in `database_users`.                                                    |
| `database_connection` | object | No | Optional routing configuration for how the service connects to the database. See [Database Connection Routing](#database-connection-routing).                   |

## Adding a Service

Include a `services` array in your database spec when creating or
updating a database. The following examples show how to add an MCP
service and a PostgREST service.

### Adding an MCP Service

In the following example, a `curl` command creates a single-node
database with one MCP service instance:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "config": {
                             "llm_enabled": true,
                            "llm_provider": "anthropic",
                            "llm_model": "claude-sonnet-4-5",
                            "anthropic_api_key": "sk-ant-..."
                        }
                    }
                ]
            }
        }'
    ```

### Adding a PostgREST Service

In the following example, a `curl` command creates a single-node
database with a PostgREST service instance. The service exposes the
`public` schema and enables JWT authentication:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "storefront",
            "spec": {
                "database_name": "storefront",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "app",
                        "password": "changeme",
                        "db_owner": true,
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "api",
                        "service_type": "postgrest",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 3100,
                        "connect_as": "app",
                        "config": {
                            "jwt_secret": "a-secret-key-of-at-least-32-characters"
                        }
                    }
                ]
            }
        }'
    ```

The response includes a task ID you can use to track progress. See
[Tasks & Logs](../using/tasks-logs.md) for details.

## Updating a Service

To update a service's configuration, submit a `POST` request to
`/v1/databases/{database_id}` with the modified service spec in the
`services` array.

!!! important

    The `services` array in an update request is declarative; it
    replaces the complete list of services for the database. To keep an
    existing service running unchanged, include its current spec
    alongside any new or modified entries.

In the following example, a `curl` command updates the MCP service to
use a different model:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases/example \
        -H 'Content-Type: application/json' \
        --data '{
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "config": {
                            "llm_enabled": true,
                            "llm_provider": "anthropic",
                            "llm_model": "claude-opus-4-5",
                            "anthropic_api_key": "sk-ant-..."
                        }
                    }
                ]
            }
        }'
    ```

## Removing a Service

To remove a service, submit an update request that omits the service
from the `services` array. The Control Plane stops and deletes all
service instances for that service and revokes its database credentials.

!!! warning

    Removing a service is irreversible. The Control Plane deletes all
    service instances, their configuration, and their data directories.
    Any clients connected to the service lose access immediately.
    The `connect_as` database user is **not** dropped — it is
    customer-managed and may be shared with other services or applications.

## Checking Service Status

To check the current state of your service instances, retrieve the
database and inspect the `service_instances` field in the response. In
the following example, a `curl` command retrieves the database:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/databases/example
    ```

Each service instance in the response includes a `state` field. See the
[Services Overview](index.md#service-instances) for a description of
each state.

## Database Connection Routing

By default, the Control Plane builds a connection string that includes
all database nodes, with the local node listed first. You can override
this behavior using the `database_connection` field in the service spec.
The following table describes the `database_connection` fields:

| Field | Type | Description |
|-------|------|-------------|
| `target_nodes` | array of strings | An ordered list of node names to include in the connection string. Nodes are tried in the order listed. |
| `target_session_attrs` | string | Overrides the libpq `target_session_attrs` parameter. Valid values: `primary`, `prefer-standby`, `standby`, `read-write`, `any`. |

In the following example, the `database_connection` field routes the
service to the `n1` node only:

=== "curl"

    ```sh
    "database_connection": {
        "target_nodes": ["n1"],
        "target_session_attrs": "primary"
    }
    ```

!!! tip

    Use `database_connection` when your service needs to read from a
    specific node or enforce write routing to the primary.
