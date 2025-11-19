# Tasks and Logs

The pgEdge Control Plane provides tools to monitor asynchronous operations and access database logs. You can list and inspect tasks, retrieve task logs, and view Postgres log files for your databases.

## Tasks

Every asynchronous database operation managed by the pgEdge Control Plane produces a *task* that you can use to track the progress of that operation.

### Listing Tasks

To list tasks for a database, submit a `GET` request to the
`/v1/databases/{database_id}/tasks` endpoint. For example:

=== "curl"

    ```sh
    curl http://host-3:3000/v1/databases/example/tasks
    ```

This returns all tasks associated with the database across time. This endpoint
also supports pagination and sorting, which can be useful when there are a large
number of tasks:

=== "curl"

    ```sh
    curl 'http://host-3:3000/v1/databases/example/tasks?limit=5&after_task_id=404ecbe0-5cda-11f0-900b-a74a79e3bdba&sort_order=asc'
    ```

### Getting a Specific Task

If you have a task ID, such as one returned when [create a database](./create-db.md), you can fetch details for that task by
submitting a `GET` request to the `/v1/databases/{database_id}/tasks/{task_id}`
endpoint. For example:

=== "curl"

    ```sh
    curl http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd
    ```

### Getting Task Logs

You can fetch log messages for a task by submitting a `GET` request to the
`/v1/databases/{database_id}/tasks/{task_id}/log` endpoint.

=== "curl"

    ```sh
    curl http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log
    ```

Task logs are updated in real time, so you can fetch them while the task is
still running. You can limit your request to only return new logs by taking the
`last_entry_id` field from the response and using it in the `after_entry_id`
parameter:

=== "curl"

    ```sh
    curl 'http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?after_entry_id=0d639fbe-72bf-41ca-a81d-f7a524083cd4'
    ```

You can also limit your request to only the most recent log entries with the
`limit` parameter:

=== "curl"

    ```sh
    curl 'http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?limit=10'
    ```

## Viewing Postgres Logs

By default, each database is configured to write log files to the following directory:

```
{data_directory}/instances/{instance_id}/data/pgdata/log/
```

By default, Postgres will be configured to rotate log files each day with a 1-week retention period.

You can use the `spec.postgresql_conf` and `spec.nodes[].postgresql_conf` fields to modify this configuration for the entire database or for a particular node. See [Creating a database](./create-db.md) for an example which demonstrates modifying this field.

You can also see the [API Reference](../api/reference.md) for more details.

If you need long-term storage of log messages, we recommend using an
observability tool, like [Vector](https://vector.dev/), to transmit the contents
of these files to a centralized store.
