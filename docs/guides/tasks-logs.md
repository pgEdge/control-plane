# Reviewing Tasks and Logs

Every asynchronous database operation managed by the pgEdge Control Plane produces a *task* that you can use to track the progress of that operation. The pgEdge Control Plane provides tools you can use to perform refined task monitoring. The Control Plane allows you to list and inspect tasks, retrieve task logs, and cancel tasks.  You can also review Postgres log files for your databases.


## Listing Tasks

To list tasks for a specific database, submit a `GET` request to the `{node_ip_address}:3000/v1/databases/{database_id}/tasks` endpoint. 

Where: 

* `node_ip_address` is the IP address of the node.
* `database_id` is the name of the database.

For example:

=== "curl"

    ```sh
    curl http://44.196.165.149:3000/v1/databases/acctg/tasks
    ```

This request returns a comprehensive list of the tasks associated with the `acctg` database on `44.196.165.149`. This endpoint also supports pagination and sorting, which can be useful when there are a large number of tasks. To limit the number of requests returned and specify a starting task boundary, include the `limit`, `after_task_id`, and `sort_order` properties: `{node_ip_address}:3000/v1/databases/{database_id}/tasks?limit={value}&after_task_id={task_id}&sort_order={asc|desc}`

Where:

* `node_ip_address` is the IP address of the node.
* `database_id` is the name of the database.
* `limit` specifies the number of tasks to return.
* `after_task_id` specifies the early boundary for tasks retreived.
* `sort_order` specifies if you would like information returned in ascending (`asc`) or descending (`desc`) order.

For example:

=== "curl"

    ```sh
    curl 'http://44.196.165.149:3000/v1/databases/acctg/tasks?limit=5&after_task_id=404ecbe0-5cda-11f0-900b-a74a79e3bdba&sort_order=asc'
    ```

This command returns the `5` tasks that occurred after the `task_id` specified in `after_task_id` in ascending order.


**Getting a Specific Task**

If you have a task ID, such as one returned when invoking [create a database](./create-db.md), you can fetch details for that task by submitting a `GET` request to the `{node_ip_address}:3000/v1/databases/{database_id}/tasks/{task_id}` endpoint. 

Where:

* `node_ip_address` is the IP address of the node.
* `database_id` specifies the name of the database.
* `task_id` specifies the identifier for the requested task.

For example:

=== "curl"

    ```sh
    curl http://44.196.165.149:3000/v1/databases/acctg/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd
    ```

### Reviewing Task Log Files

You can fetch log file entries for a specific task by submitting a `GET` request to the `{node_ip_address}/v1/databases/{database_id}/tasks/{task_id}/log` endpoint. 

Where:

* `node_ip_address` is the IP address of the node.
* `database_id` specifies the name of the database.
* `task_id` specifies the identifier for the requested task.

For example:

=== "curl"

    ```sh
    curl http://44.196.165.149:3000/v1/databases/database_id/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log
    ```


Task logs are updated in real time, so you can fetch them while a task is still running. You can limit your request to only return new logs by including the `last_entry_id` field from a response in the `after_entry_id` property; for example:

=== "curl"

    ```sh
    curl 'http://44.196.165.149:3000/v1/databases/acctg/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?after_entry_id=0d639fbe-72bf-41ca-a81d-f7a524083cd4'
    ```

You can also limit your request to only the most recent log entries by including the `limit` parameter; for example:

=== "curl"

    ```sh
    curl 'http://44.196.165.149:3000/v1/databases/acctg/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?limit=10'
    ```

Where:

* `node_ip_address` is the IP address of the node.
* `limit` specifies the number of tasks to return.
* `after_task_id` specifies the early boundary for tasks retreived.


## Cancelling a Task

Task cancellation can be used to stop long-running operations, and is a helpful tool for emergency interruption without killing the entire service. To request cancellation of a long-running workflow task, query the Control Plane interface, specifying a `task_id` and the `cancel` keyword. This syntax provides interruption of operations such as backups, restores, updates, and database lifecycle tasks:

=== "curl"

```
 curl 'http://{host_ip_address}:3000/v1/databases/{database_id}/tasks/{task_id}/cancel
```

Where:

* `node_ip_address` is the IP address of the node.
* `database_id` specifies the name of the database.
* `task_id` specifies the identifier for the requested task.

Cancellation is cooperative: the workflow checks its context for cancellation and performs cleanup before exiting. This also means that cancellation is not instantaneous as cleanup must run. Tasks already in `completed`, `failed`, or `canceled` cannot be canceled; cancellation is only allowed if the task has a workflow instance.

!!! note

    Note that cancelling certain tasks can result in failed database states as a result of partially applied operations, and that operations are not safely rolled back. 

Create Database / Update Database

* Canceling stops the workflow immediately and the task becomes **canceled**.
* The database is marked **failed**  as the instance may be in an undefined state.

Delete Database

* The task stops and becomes **canceled**.
* The database is marked **failed**  as the instance may be in an undefined state.

Restart Instance

* Cancellation halts the restart process and marks the task as **canceled**.
* The database is set to **failed**, as the instance may be in an undefined state.

pgBackRest Backup
* The backup stops and the task becomes **canceled**.
* The database state remains unchanged.

pgBackRest Restore

* Cancellation stops the restore workflow and marks the task as **canceled**.
* The database state remains unchanged.
* Note that partially restored data may remain.


## Viewing Postgres logs

By default, each database is configured to write log files to the following directory:

```
{data_directory}/instances/{instance_id}/data/pgdata/log/
```

Where:

* `data_directory` is the location of your Postgres `data` directory.
* `instance_id` specifies the Control Plane instance identifier.

By default, Postgres is configured to rotate log files each day with a 1-week retention period.

You can use the `spec.postgresql_conf` and `spec.nodes[].postgresql_conf` fields to modify this configuration for the entire database or for a particular node. See [Creating a database](./create-db.md) for an example that demonstrates modifying this field.

You can also see the [API Reference](../api/reference.md) for more details.

If you need long-term storage of log messages, we recommend using an observability tool, like [Vector](https://vector.dev/), to transmit the contents of these files to a centralized store.
