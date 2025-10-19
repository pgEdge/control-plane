# Creating a database

To create a database, submit a `POST` request to the `/v1/databases` endpoint of
any host in the cluster. For example:

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

This example request creates a three-node pgEdge database cluster with one
instance per node and an `admin` database user. The response will contain the
database specification that you submitted, along with tracking information for
the asynchronous creation task. The creation process is asynchronous, meaning
the server responds when the process starts rather than when it finishes.

> [!TIP]
> If a database creation operation fails, you can retry the operation by
> submitting the same request body in a [database update](#updating-a-database)
> request.

You can view the current status of the database by submitting a `GET` request to
the `/v1/databases/{database_id}` endpoint and inspecting the `state` field in
the response:

```sh
curl http://localhost:3000/v1/databases/example
```

You can also use the task ID from the original response to retrieve logs and
other details from the creation process. See the [Tasks and task
logs](#tasks-and-task-logs) section below for more information.

There are many other database settings that you can customize when creating or
updating a database. Settings in the `spec` object will apply to all nodes. You
can also apply or override a setting on a specific node by setting it in the
node's object in `spec.nodes[]`. This example request alters the max connections
for all nodes and overrides the port just for the `n1` node:

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

Refer to the [API specification](#openapi-specification) for details on all
available settings.