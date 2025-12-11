# Upgrading a Database

## Minor Version Upgrades

The Control Plane supports minor Postgres version upgrades, such as upgrading
from Postgres 18.0 to 18.1, via the API. The Postgres version is a field in the
`spec` that you submit in the [create](./create-db.md) and
[update](./update-db.md) requests.

!!! tip

    Before you upgrade, make sure that your desired version is supported by
    following the [instructions below](#which-versions-are-available).

To upgrade a database, submit a `POST` request to the
`/v1/databases/{database_id}` endpoint of any host in the cluster with the new
version in the `postgres_version` field. For example:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
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
                "postgres_version": "17.6",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] },
                    { "name": "n3", "host_ids": ["host-3"] }
                ]
            }
        }'
    ```

The API response will contain a task ID that you can use to track the upgrade
process. See the [Tasks and Logs](./tasks-logs.md) guide for more information.

Like with all other updates, the Control Plane applies upgrades in a rolling
fashion, meaning that it upgrades one node at a time. If a node has both primary
and replica instances, the replica instances are upgraded first.

You can also set the `postgres_version` field on a per node basis if you want to gradually roll out minor version updates to different nodes. For example:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
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
                "postgres_version": "18.0",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { 
                        "name": "n2", 
                        "host_ids": ["host-2"], 
                        "postgres_version": "18.1" 
                    },
                ]
            }
        }'
    ```

## Major Version Upgrades


The Control Plane supports major Postgres version upgrades by leveraging the Spock extension's zero downtime add node capability. With this approach, you can add a new node to your existing database with the `postgres_version` set to the new version.


For example, to gradually move a single node database running Postgres 17.6 to 18.1:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
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
                "postgres_version": "17.6",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { 
                        "name": "n2", 
                        "host_ids": ["host-2"], 
                        "postgres_version": "18.1", 
                        "source_node": "n1" 
                    },
                ]
            }
        }'
    ```

Once the update operation completes, the newly added node will be running Postgres 18.1, and the existing node remains operational, running Postgres 17.6. Both nodes remain writeable during this operation. This allows for easier migration of your applications to the new node.


Once you are satisfied with the upgraded node, you can perform an additional update to remove the old node from your database spec, and update the `postgres_version` field across your database to 18.1. For example:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
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
                "postgres_version": "18.1",
                "nodes": [
                    { "name": "n2", "host_ids": ["host-2"] },
                ]
            }
        }'
    ```

!!! tip

    This approach is supported for both single region and distributed deployments. You can also choose to use the same host for the upgraded node, assuming you have the available space and compute power to run both instances side by side.




Outside of this approach, the Control Plane doesn't currently support other mechanisms for performing in-place major version upgrades of Postgres. 

As an alternative to using the zero downtime add node approach, you can also [create a new database](./create-db.md) with your desired version, and then use [`pg_dump`](https://www.postgresql.org/docs/current/backup-dump.html) to migrate your data to the new database.

!!! warning

You should thoroughly test major version upgrade scenarios in a non-production environment before upgrading your production database. Additionally, we recommend you review the [Backup and Restore](./backup-restore.md) documentation, and take a fresh backup of your database before upgrading to ensure that you are prepared to restore in the event you run into problems.




## Which Versions Are Available

You can see the list of supported Postgres versions for each host by submitting
a `GET` request to the `/v1/hosts` endpoint of any host in the cluster and
inspecting the `supported_pgedge_versions` fields in the output:

=== "curl"

    ```
    curl http://host-3:3000/v1/hosts
    ```

### If a Version Isn’t Listed

Newer versions of the Control Plane server will support newer versions of
Postgres. If you don't see your desired version in this list, check the
[releases page](https://github.com/pgEdge/control-plane/releases/latest) to see
if there is a newer version of the Control Plane. If there is, you can follow
the [Control Plane cluster upgrade instructions](../installation/upgrading.md) to upgrade
your Control Plane cluster to the latest version.
