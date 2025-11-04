# Backup and Restore

The Control Plane uses [pgBackRest](https://pgbackrest.org/) to provide backup
and restore capabilities for databases.

## Configuring pgBackRest for Backups

To use pgBackRest, you must configure one or more backup repositories for the
database or node you want to back up. You can configure backup repositories in
the `backup_config` field when you [create a database](./create-db.md)
or when you [update an existing database](./update-db.md).

For example, to configure all database nodes to create backups in an S3 bucket:

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

!!! tip

    If you're running your databases on AWS EC2, you can use an [AWS Instance Profile](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_use_switch-role-ec2_instance-profiles.html) instead of providing IAM user credentials to the Control Plane API. Similarly, you can use a [Google Service Account](https://cloud.google.com/compute/docs/access/service-accounts) if you're running your databases in Google Compute Engine.

Alternatively, you could also configure a single node to backup to a local NFS share:

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

!!! note

    In the above example, our database is running on Docker Swarm, so we also need to tell the Control Plane to mount our local NFS share in the container as an extra volume. Note that the `base_path` we configured for the repository is the `destination_path` for our extra volume.

## Initiating a Backup

Once we've [configured pgBackRest for our
database](#configuring-pgbackrest-for-backups), we can initiate a backup by
submitting a `POST` request to the
`/v1/databases/{database_id}/nodes/{node_name}/backups` endpoint.

For example, to initiate a full backup from the `n1` node of our example database:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/nodes/n1/backups \
        -H 'Content-Type:application/json' \
        --data '{ "type": "full" }'
    ```

Creating a backup is an asynchronous process. The response from this request
contains a task identifier that you can use to fetch logs and status information
for the backup process. See [Tasks and Logs](./tasks-logs.md) for more
information about tasks.

## Scheduled Backups

You can include schedules in your `backup_config` to perform backups on a
schedule. Schedules are expressed as "cron expressions" and evaluated in UTC.
For example, the expression `0 0 * * *` would result in a backup every night at
midnight UTC.

This example request uses that expression to configure a full
backup for every night at midnight as well as an incremental backup every hour:

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

## Performing an in-place restore

You can perform an in-place restore on one or more nodes at a time. For each
node to be restored, the in-place restore process will:

    1. Remove Spock subscriptions to or from the node.
    2. Tear down any read replicas for the node.
    3. Remove backup configurations for the node.
    4. Stop the node's primary instance.
    5. Run `pgbackrest restore` with the `--delta` option.
    6. Start the node's primary instance.
    7. Recreate any read replicas for the node.
    8. Recreate Spock subscriptions for the node.

!!! important

    The Control Plane removes the backup configuration for each node that's being restored. This is necessary because the instance's system identifier can change with the restore, and pgBackRest will prevent you from reusing a repository when that system identifier changes. Once the restore is complete, you must submit an [update request](./update-db.md) to reenable backups for the node you've restored. When you do, we recommend that you either modify the repository's `base_path` or include the optional `id` property to store the backups in a new location. This will prevent the issue of reusing the same backup repository.

To perform an in-place restore, submit a `POST` request to the
`/v1/databases/{database_id}/restore` endpoint. 

This example demonstrates an in-place restore on the `n1` node from the latest backup of `n3`, using an EC2 Instance Profile to provide the AWS SDK credentials:

=== "curl"

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

=== "curl"

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

## Creating a New Database from a Backup

You can use the `spec.restore_config` field in your [create database
request](./create-db.md) to create a database from an existing backup. 

=== "curl"

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

## Creating a New Node from a Backup

Similar to [creating a new database from a backup](#creating-a-new-database-from-a-backup), you can include a
`restore_config` on a specific node to create that node from a backup. 

This example demonstrates adding a new node, `n4`, to an existing database using a backup of `n1` as the source:

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
via a [database update](./update-db.md) afterward.
