# Updating a database

To update a database, submit a `POST` request to the
`/v1/databases/{database_id}` endpoint of any host in the cluster. Using the
same example as above, a request to add a new node on `host-4` would look like:

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

> [!TIP]
> Secret values, such as database user passwords or cloud credentials, are only
> needed at creation time. You can omit these values from update requests unless
> you need to change them. After removing the secret values, you can safely save
> the request body to a file and even add it to version control alongside other
> infrastructure-as-code files.

Similar to the creation process, updating a database is also an asynchronous
process. You can view the current database status by submitting a `GET` request
to the `/v1/databases/{database_id}` endpoint. You can also use the task ID from
the original response to retrieve logs and other details from the update
process. See the [Tasks and task logs](#tasks-and-task-logs) section below for
more information.