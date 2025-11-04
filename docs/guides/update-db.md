# Updating a Database

To update a database, submit a `POST` request to the
`/v1/databases/{database_id}` endpoint of any host in the cluster with the updated spec for the database.

For example, this update request adds a new node on `host-4` for the existing `example` database:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] },
                    { "name": "n3", "host_ids": ["host-3"] },
                    { "name": "n4", "host_ids": ["host-4"] }
                ]
            }
        }'
    ```

By default, the Control Plane performs a zero downtime add node operation when adding a distributed node, loading existing database data and structure from the first node in the database (`n1` in the example above). 

You can control which node is used to load data by specifying the `source_node` property on the node you are adding:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] },
                    { "name": "n3", "host_ids": ["host-3"] },
                    { "name": "n4", "host_ids": ["host-4"], "source_node": "n2" }
                ]
            }
        }'
    ```

Alternatively, you can use pgBackRest to bootstrap the new node via a restore. See [Creating a New Node from a Backup](./backup-restore.md#creating-a-new-node-from-a-backup).

!!! tip

    Secret values, such as database user passwords or cloud credentials, are only needed at creation time. You can omit these values from update requests unless you need to change them. After removing the secret values, you can safely save the request body to a file and even add it to version control alongside other infrastructure-as-code files.

Similar to the creation process, updating a database is also an asynchronous process. 

You can view the current database status by submitting a `GET` request
to the `/v1/databases/{database_id}` endpoint. 

=== "curl"

    ```sh
    curl http://localhost:3000/v1/databases/example
    ```


You can also use the task ID from the original response to retrieve logs and other details from the update process. See the [Tasks and Logs](./tasks-logs.md) for more information.
