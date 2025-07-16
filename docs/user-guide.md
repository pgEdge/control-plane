# User guide

- [User guide](#user-guide)
  - [Concepts](#concepts)
    - [The Control Plane](#the-control-plane)
    - [API terminology](#api-terminology)
      - [Hosts](#hosts)
      - [Databases](#databases)
      - [Nodes](#nodes)
      - [Instances](#instances)
  - [Installation and configuration](#installation-and-configuration)
  - [Initializing a Control Plane cluster](#initializing-a-control-plane-cluster)
  - [Creating a database](#creating-a-database)
  - [Updating a database](#updating-a-database)
  - [Connecting to a database](#connecting-to-a-database)
  - [Viewing database logs](#viewing-database-logs)
  - [Backup and restore](#backup-and-restore)
    - [Configuring pgBackRest for backups](#configuring-pgbackrest-for-backups)
    - [Initiating a backup](#initiating-a-backup)
    - [Scheduled backups](#scheduled-backups)
    - [Performing an in-place restore](#performing-an-in-place-restore)
    - [Creating a new database from a backup](#creating-a-new-database-from-a-backup)
    - [Creating a new node from a backup](#creating-a-new-node-from-a-backup)
  - [Tasks and task logs](#tasks-and-task-logs)
    - [Listing tasks](#listing-tasks)
    - [Getting a specific task](#getting-a-specific-task)
    - [Getting task logs](#getting-task-logs)
  - [Deleting a database](#deleting-a-database)
  - [OpenAPI specification](#openapi-specification)

## Concepts

### The Control Plane

The pgEdge Control Plane is a distributed application that creates and manages
PostgreSQL databases with pgEdge's multi-active replication technology. In its
default configuration, it uses an embedded Etcd server to store configuration
and coordinate database operations with other instances. You can interact with
the Control Plane via an HTTP API. Once you've initialized a Control Plane
cluster, you can submit your requests to any Control Plane instance in the
cluster.

Most Control Plane API operations, such as database modifications, are
idempotent. If an operation fails, you can safely retry the operation after
resolving the underlying issue.

Currently, the Control Plane can deploy databases to Docker Swarm. We plan to
support other orchestrators, like Kubernetes, and bare metal/VMs in the future.

### API terminology

This section defines some of the terms used in the Control Plane API and this
document.

```mermaid
graph TD;
    database::example-->node::n1;
    database::example-->node::n2;
    database::example-->node::n3;
    node::n1-->instance::n1a;
    node::n1-->instance::n1b;
    node::n2-->instance::n2a;
    node::n2-->instance::n2b;
    node::n3-->instance::n3a;
    node::n3-->instance::n3b;
```

> The above diagram demonstrates the relationship between databases, nodes, and
> instances: a database is composed of one or more nodes, and each node is
> composed of one or more instances.

#### Hosts

Hosts are the underlying compute resources used to run database instances. For
Docker Swarm, each host is a Docker Swarm node. One Control Plane instance
should be deployed to each host that will run databases. For this reason, each
Control Plane instance is identified by a host ID.

#### Databases

A database in the Control Plane API is a PostgreSQL database that is replicated
between multiple PostgreSQL instances. A database is composed of one or more
[nodes](#nodes). You create and update databases by submitting a "database spec"
to a declarative API. See [Creating a database](#creating-a-database) and
[Updating a database](#updating-a-database) for more about the database API.

#### Nodes

pgEdge uses an extension, called [Spock](https://github.com/pgEdge/spock), to
replicate data between PostgreSQL instances. In the Control Plane API, "nodes"
refer to Spock nodes. Each node is composed of one or more PostgreSQL
[instances](#instances), where one instance is a primary and the others are read
replicas. Writes can be made to the primary instance of any node in the
database.

#### Instances

Unless clarified, "instances" in the Control Plane API refers to PostgreSQL
instances. For a given node, one instance is created for each host specified in
the `host_ids` array. When a node has multiple instances, one instance will be
elected as the primary, and the others will serve as read-only replicas. In
Docker Swarm, each instance is a separate Docker container.

## Installation and configuration

See the [Docker Swarm installation document](./installation-swarm.md) for
instructions to install the Control Plane on Docker Swarm.

See the [configuration reference](./configuration.md) for descriptions of all
configuration settings.

## Initializing a Control Plane cluster

Each Control Plane server instance starts in an uninitialized state until it's
added to a cluster. In a typical configuration, you will submit a request to one
Control Plane instance to initialize a new cluster, then submit requests to all
other instances to join them to the new cluster. For example, the steps to
initialize a three-host cluster would look like:

1. Initialize the cluster on `host-1`
2. Join `host-2` to `host-1`'s cluster
3. Join `host-3` to `host-1`'s cluster

To initialize a cluster, make a `GET` request to the `/v1/cluster/init`
endpoint. The response will contain a "join token", which can be provided to
other instances via a `POST` request to the `/v1/cluster/join` endpoint. Using
the same example above, the initialization steps would be:

1. Initialize the cluster on `host-1`

```sh
curl http://host-1:3000/v1/cluster/init
```

This returns a response like:

```json
{
  "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
  "server_url":"http://198.19.249.2:3000"
}
```

We'll submit this to the other Control Plane server instances to join them to
the new cluster.

1. Join `host-2` to `host-1`'s cluster

```sh
curl -X POST http://host-2:3000/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data '{
        "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
        "server_url":"http://198.19.249.2:3000"
    }'
```

This will return a `204` response on success.

1. Join `host-3` to `host-1`'s cluster

```sh
curl -X POST http://host-3:3000/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data '{
        "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
        "server_url":"http://198.19.249.2:3000"
    }'
```

The "join token" can also be fetched from any host in the cluster with a `GET`
request the `/v1/cluster/join-token` endpoint:

```sh
curl http://host-1:3000/v1/cluster/join-token
```

After initializing the cluster, you can submit requests to any host in the
cluster.

## Creating a database

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

## Updating a database

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

## Connecting to a database

Once the database is available, you make a `GET` request to the
`/v1/databases/{database_id}` endpoint to get information about all of the
instances that the Control Plane created.

```sh
curl http://localhost:3000/v1/databases/example
```

The `instances` field in the response contains details about each instance, and
the `connection_info` field of each instance object contains connection
information for that specific instance.

> [!WARNING]
> If you're running the Control Plane with Docker Desktop on MacOS or Windows,
> the IP address in the `ip_address` field will be unreachable from your host
> machine. Use `localhost` instead when connecting to the instance.

> [!NOTE]
> If you have not exposed your database to outside connections, for example, by
> omitting the `port` field in your database specification, the
> `connection_info` field will be omitted in this API response.

## Viewing database logs

By default, each database is configured to write log files to the
`{data_directory}/instances/{instance_id}/data/pgdata/log/` directory. By
default, it will rotate log files each day with a 1-week retention period. You
can use the `spec.postgresql_conf` and `spec.nodes[].postgresql_conf` fields to
modify this configuration for the entire database or for a particular node. The
[Creating a database](#creating-a-database) section contains an example with
this field. You can also see the [OpenAPI specification](#openapi-specification)
for more details.

If you need long-term storage of log messages, we recommend using an
observability tool, like [Vector](https://vector.dev/), to transmit the contents
of these files to a centralized store.

## Backup and restore

The Control Plane uses [pgBackRest](https://pgbackrest.org/) to provide backup
and restore functionality.

### Configuring pgBackRest for backups

To use pgBackRest, you must configure one or more backup repositories for the
database or node you want to back up. You can configure backup repositories in
the `backup_config` field when you [create the database](#creating-a-database)
or when you [update an existing database](#updating-a-database). For example, to
configure all database nodes to create backups in an S3 bucket:

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
            "backup_config": {
                "repositories": [
                    {
                        "type": "s3",
                        "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                        "s3_region": "us-east-1",
                        "s3_key": "AKIAIOSFODNN7EXAMPLE",
                        "s3_key_secret": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                    }
                ]
            },
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

> [!TIP]
> If you're running your databases on AWS EC2, you can use [Instance
> Profiles](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_use_switch-role-ec2_instance-profiles.html)
> instead of providing IAM user credentials to the Control Plane API. Similarly,
> you can use [Service
> Accounts](https://cloud.google.com/compute/docs/access/service-accounts) if
> you're running your databases in Google Compute Engine.

Or to configure a single node to backup to a local NFS share:

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
                {
                    "name": "n1",
                    "host_ids": ["host-1"],
                    "orchestrator_opts": {
                        "swarm": {
                            "extra_volumes": [
                                {
                                    "host_path": "/mnt/db-backups",
                                    "destination_path": "/backups"
                                }
                            ]
                        }
                    },
                    "backup_config": {
                        "repositories": [
                            {
                                "type": "posix",
                                "base_path": "/backups"
                            }
                        ]
                    },
                },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

> [!NOTE]
> In the above example, our database is running on Docker Swarm, so we also need
> to tell the Control Plane to mount our local NFS share in the container as an
> extra volume. Note that the `base_path` we configured for the repository is
> the `destination_path` for our extra volume.

### Initiating a backup

Once we've [configured pgBackRest for our
database](#configuring-pgbackrest-for-backups), we can initiate a backup by
submitting a `POST` request to the
`/v1/databases/{database_id}/nodes/{node_name}/backups` endpoint. For example,
to initiate a full backup from the `n1` node of our example database:

```sh
curl -X POST http://host-3:3000/v1/databases/example/nodes/n1/backups \
    -H 'Content-Type:application/json' \
    --data '{ "type": "full" }'
```

Creating a backup is an asynchronous process. The response from this request
contains a task identifier that you can use to fetch logs and status information
for the backup process. See [Tasks and task logs](#tasks-and-task-logs) for more
information about tasks.

### Scheduled backups

You can include schedules in your `backup_config` to perform backups on a
schedule. Schedules are expressed as "cron expressions" and evaluated in UTC.
For example, the expression `0 0 * * *` would result in a backup every night at
midnight UTC. This example request uses that expression to configure a full
backup for every night at midnight as well as an incremental backup every hour:

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
            "backup_config": {
                "repositories": [
                    {
                        "type": "s3",
                        "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                        "s3_region": "us-east-1",
                    }
                ],
                "schedules": [
                    {
                        "id": "nightly-full-backup",
                        "type": "full",
                        "cron_expression": "0 0 * * *"
                    },
                    {
                        "id": "hourly-incremental",
                        "type": "incr",
                        "cron_expression": "0 * * * *"
                    }
                ]
            },
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

### Performing an in-place restore

You can perform an in-place restore on one or more nodes at a time. For each
node to be restored, the in-place restore process will:

1. Remove Spock subscriptions to or from the node
2. Tear down any read replicas for the node
3. Remove backup configurations for the node
4. Stop the node's primary instance
5. Run `pgbackrest restore` with the `--delta` option
6. Start the node's primary instance
7. Recreate any read replicas for the node
8. Recreate Spock subscriptions for the node

> [!IMPORTANT]
> The Control Plane removes the backup configuration for each node that's being
> restored. This is necessary because the instance's system identifier can
> change with the restore, and pgBackRest will prevent you from reusing a
> repository when that system identifier changes. Once the restore is complete,
> you must submit an [update request](#updating-a-database) to reenable backups
> for the node you've restored. When you do, we recommend that you either modify
> the repository's `base_path` or include the optional `id` property to store
> the backups in a new location. This will prevent the issue of reusing the same
> backup repository.

To perform an in-place restore, submit a `POST` request to the
`/v1/databases/{database_id}/restore` endpoint. This example demonstrates an
in-place restore on the `n1` node from the latest backup of `n3`, using an EC2
Instance Profile to provide the AWS SDK credentials:

```sh
curl -X POST http://host-3:3000/v1/databases/example/restore \
    -H 'Content-Type:application/json' \
    --data '{
        "target_nodes": ["n1"],
        "restore_config": {
            "source_database_id": "example",
            "source_node_name": "n3",
            "source_database_name": "example",
            "repository": {
                "type": "s3",
                "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                "s3_region": "us-east-1"
            }
        }
    }'
```

You can omit the `target_nodes` field to perform the restore on all nodes. This
example builds on the above example but instead restores all nodes to a specific
point in time:

```sh
curl -X POST http://host-3:3000/v1/databases/example/restore \
    -H 'Content-Type:application/json' \
    --data '{
        "restore_config": {
            "source_database_id": "example",
            "source_node_name": "n3",
            "source_database_name": "example",
            "repository": {
                "type": "s3",
                "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                "s3_region": "us-east-1"
            },
            "restore_options": {
                "set": "20250505-133723F",
                "type": "time",
                "target": "2025-05-05 09:38:52-04"
            }
        }
    }'
```

### Creating a new database from a backup

You can use the `spec.restore_config` field in your [create database
request](#creating-a-database) to create a database from an existing backup. For
example:

```sh
curl -X POST http://host-3:3000/v1/databases \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example-copy",
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
            ],
            "restore_config": {
                "source_database_id": "example",
                "source_node_name": "n1",
                "source_database_name": "example",
                "repository": {
                    "type": "s3",
                    "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                    "s3_region": "us-east-1"
                }
            }
        }
    }'
```

> [!NOTE]
> These `restore_config` fields will not affect existing instances when they're
> included in an update request. They are only used to create new instances,
> such as when you add a new node or when you add an instance to an existing
> node. You can safely remove the `restore_config` fields in an update request
> if you don't want that behavior.

### Creating a new node from a backup

Similar to [creating a new database from a
backup](#creating-a-new-database-from-a-backup), you can include a
`restore_config` on a specific node to create that node from a backup. This
example demonstrates adding a new node, `n4`, to an existing database using a
backup of `n1` as the source:

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
                {
                    "name": "n4",
                    "host_ids": ["host-4"],
                    "restore_config": {
                        "source_database_id": "example",
                        "source_node_name": "n1",
                        "source_database_name": "example",
                        "repository": {
                            "type": "s3",
                            "s3_bucket": "backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
                            "s3_region": "us-east-1"
                        }
                    }
                }
            ],
        }
    }'
```

The `restore_config` is not used after creating the node, so it's safe to remove
via an [update request](#updating-a-database) afterward.

## Tasks and task logs

Every asynchronous operation produces a "task" that you can use to track the
progress of that operation.

### Listing tasks

To list tasks for a database, submit a `GET` request to the
`/v1/databases/{database_id}/tasks` endpoint. For example:

```sh
curl http://host-3:3000/v1/databases/example/tasks
```

This returns all tasks associated with the database across time. This endpoint
also supports pagination and sorting, which can be useful when there are a large
number of tasks:

```sh
curl 'http://host-3:3000/v1/databases/example/tasks?limit=5&after_task_id=404ecbe0-5cda-11f0-900b-a74a79e3bdba&sort_order=asc'
```

### Getting a specific task

If you have a task ID, such as one returned by the [create database
endpoint](#creating-a-database), you can fetch details for that task by
submitting a `GET` request to the `/v1/databases/{database_id}/tasks/{task_id}`
endpoint. For example:

```sh
curl http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd
```

### Getting task logs

You can fetch log messages for a task by submitting a `GET` request to the
`/v1/databases/{database_id}/tasks/{task_id}/log` endpoint.

```sh
curl http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log
```

Task logs are updated in real time, so you can fetch them while the task is
still running. You can limit your request to only return new logs by taking the
`last_entry_id` field from the response and using it in the `after_entry_id`
parameter:

```sh
curl 'http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?after_entry_id=0d639fbe-72bf-41ca-a81d-f7a524083cd4'
```

You can also limit your request to only the most recent log entries with the
`limit` parameter:

```sh
curl 'http://host-3:3000/v1/databases/example/tasks/d3cd2fab-4b1f-4eb9-b614-181c10b07acd/log?limit=10'
```

## Deleting a database

When you no longer need a database, you can delete it by submitting a `DELETE`
request to the `/v1/databases/{database_id}` endpoint:

```sh
curl -X DELETE http://host-3:3000/v1/databases/example
```

Deletes are asynchronous, so the response will contain a task that you can use
to track the progress of the delete.

## OpenAPI specification

The Control Plane server serves a JSON OpenAPI v3 specification from the
`/v1/openapi.json` endpoint. It also implements [RFC
8631](https://datatracker.ietf.org/doc/html/rfc8631) for use with tools like
[Restish](https://rest.sh).

You can also access offline copies of the OpenAPI specification in this
repository. We generate a few versions of the specification to accommodate
different tools and use cases:

- [OpenAPI v3 YAML](../api/apiv1/gen/http/openapi3.yaml)
- [OpenAPI v3 JSON](../api/apiv1/gen/http/openapi3.json)
- [OpenAPI v2 YAML](../api/apiv1/gen/http/openapi.yaml)
- [OpenAPI v2 JSON](../api/apiv1/gen/http/openapi.json)

If you've cloned this repository and have Docker installed, you can run this
command to start a local API documentation server on http://localhost:8999:

```sh
make api-docs
```
