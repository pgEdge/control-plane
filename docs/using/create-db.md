# Creating a Database

To create a database, submit a `POST` request to the `/v1/databases` endpoint of
any host in the cluster.

For example, this request creates a three node distributed database with one instance per node and an `admin` database user.

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] },
                    { "name": "n3", "host_ids": ["host-3"] }
                ]
            }
        }'
    ```

Alternatively, to deploy a single-region database with three instances (one primary and two replicas), use the following example.

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1", "host-2", "host-3"] },
                ]
            }
        }'
    ```

The API response will contain the database specification that you submitted, along with tracking information for the asynchronous creation task. The creation process is asynchronous, meaning the server responds when the process starts rather than when it finishes.

!!! tip

    If a database creation operation fails, you can retry the operation by
    submitting the same request body in a [database update](update-db.md) request.

You can view the current status of the database by submitting a `GET` request to
the `/v1/databases/{database_id}` endpoint and inspecting the `state` field in
the response:

=== "curl"

    ```sh
    curl http://localhost:3000/v1/databases/example
    ```

You can also use the task ID from the original response to retrieve logs and
other details from the creation process. See the [Tasks and Logs](tasks-logs.md) for more information.

## Customizing Database Configuration

There are many other database settings that you can customize when creating or
updating a database. Settings in the `spec` object will apply to all distributed nodes. You can also apply or override a setting on a specific node by setting it in the node's object in `spec.nodes[]`.

This example request alters the `max_connections` value for all nodes and overrides the port just for the `n1` node:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "postgresql_conf": {
                    "max_connections": 5000
                },
                "nodes": [
                    {
                        "name": "n1",
                        "host_ids": ["host-1"],
                        "port": 6432
                    },
                    { "name": "n2", "host_ids": ["host-2"] },
                    { "name": "n3", "host_ids": ["host-3"] }
                ]
            }
        }'
    ```

!!! warning

    The pgEdge Control Plane is designed to ensure that configuration is applied during database operations. It is not recommended to apply configuration directly to underlying components, including Postgres, since those changes may be overwritten or reverted. You should instead use the Control Plane to manage Postgres configuration for consistency reasons. 

Refer to the [API Reference](../api/reference.md) for details on all
available settings.

## Extension Support

The Control Plane supports all extensions included in the standard flavor of the [pgEdge Enterprise Postgres Image](https://github.com/pgedge/postgres-images?tab=readme-ov-file#standard-images). You can configure extension-related settings using the `postgresql_conf` object in your database specification.

To support extension configuration, the Control Plane allows setting `shared_preload_libraries` in the `postgresql_conf` field on the database spec.  If your extension requires additional configuration parameters, you can also include them in the `postgresql_conf` parameter. 

By default, `shared_preload_libraries` contains `pg_stat_statements`, `snowflake`, `spock`, and `postgis`.

In this example, the `shared_preload_libraries` parameter is set to load both `spock` and `pg_cron` extensions when the database starts, and `pg_cron` 
is further configured using the `cron.database_name` parameter:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "postgresql_conf": {
                    "shared_preload_libraries": "spock,pg_cron",
                    "cron.database_name": "example"
                },
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ]
            }
        }'
    ```

After creating the database, you can enable extensions in your database using `CREATE EXTENSION`.

!!! note

    Always include `spock` in `shared_preload_libraries`, as it is required for core functionality provided by the Control Plane. The Control Plane will call `CREATE EXTENSION` for spock when initializing each instance.