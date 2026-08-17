# Upgrading a Database

## Minor Version Upgrades

The Control Plane supports minor Postgres version upgrades, such as upgrading
from Postgres 18.0 to 18.1, via the API. The Postgres version is a field in the
`spec` that you submit in the [create](./create-db.md) and
[update](./update-db.md) requests.

!!! note "systemd clusters"

    API-driven minor version upgrades are not supported on systemd clusters.
    See [Performing Postgres Minor Version Upgrades](../installation/systemd-upgrading.md#performing-postgres-minor-version-upgrades)
    for the manual upgrade procedure.

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




## Image Upgrades

The Control Plane tracks available image upgrades - Postgres minor version
bumps within the same major and Spock major version bucket - using the
[version manifest](./image-management.md). You can check for available upgrades
and apply them without changing the Postgres major version.

### Checking for Available Image Upgrades

To see whether a newer stable image is available for your database in the same
Postgres major and Spock major version bucket, include `available_upgrades` in
the response:

=== "curl"

    ```sh
    curl 'http://host-3:3000/v1/databases/example?include=available_upgrades'
    ```

The response includes an `available_upgrades` array:

```json
{
  "available_upgrades": [
    {
      "image": "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1",
      "postgres_version": "17.10",
      "spock_version": "5"
    }
  ]
}
```

When no upgrades are available, the `available_upgrades` field is omitted from
the response. This means your database is already running the latest stable
image in its version bucket.

### Applying an Image Upgrade

Once you have identified the target image from `available_upgrades`, apply it
with a `POST` to the upgrade endpoint:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/upgrade \
        -H 'Content-Type:application/json' \
        --data '{
            "image": "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1"
        }'
    ```

The response contains a `task` object and the updated `database` object. Use
`task.task_id` to track progress as described in
[Tasks and Logs](./tasks-logs.md). Container pull and restart happen
asynchronously.

!!! note

    The target image must be a stable manifest entry in the same Postgres major
    and Spock major bucket as the database’s current version, and must be
    strictly newer than the currently running image. Same-version or downgrade
    requests are rejected. To upgrade to a different Postgres major version,
    see [Major Version Upgrades](#major-version-upgrades). To upgrade to a
    different Spock major version, see
    [Spock Major-Version Upgrades](#spock-major-version-upgrades).

## Spock Major-Version Upgrades

The Control Plane supports upgrading a database's Spock extension across a
major version boundary (for example, Spock 5.x to 6.x) through a dedicated
action, separate from both the minor/patch upgrade path above and the
Postgres major-version approach. A Spock major-version change affects the
replication protocol itself, so it's rolled out one node at a time rather
than applied uniformly the way a build-number bump is.

!!! note "systemd clusters"

    API-driven Spock major-version upgrades are not supported on systemd
    clusters.

To start the upgrade, submit a `POST` request to the
`/v1/databases/{database_id}/upgrade-major` endpoint with the target Spock
version and the image to upgrade to:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/upgrade-major \
        -H 'Content-Type:application/json' \
        --data '{
            "target_spock_version": "6",
            "image": "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1"
        }'
    ```

The response contains a `task` object (type `major_upgrade`) and the updated
`database` object. Use `task.task_id` to track progress as described in
[Tasks and Logs](./tasks-logs.md).

By default, nodes are upgraded in the order they appear in the database
spec. To control the order explicitly, include `node_order`, naming every
node in the database exactly once:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/upgrade-major \
        -H 'Content-Type:application/json' \
        --data '{
            "target_spock_version": "6",
            "image": "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1",
            "node_order": ["n3", "n1", "n2"]
        }'
    ```

### How the Upgrade Works

The Control Plane upgrades one node at a time: it swaps that node's image,
waits for Spock's own extension-update mechanism to bring it onto the new
major version, and confirms the node has actually resumed replicating with
its peers before moving on to the next node. Every node except the one
currently being upgraded remains fully readable and writable throughout —
Spock supports replication between mixed major versions during this
transition window.

Once every node is on the target version, the database's spec is normalized
back to a single `spock_version` field at the database level.

!!! note

    The target image must be present in the version manifest at the
    requested Spock major version and the same Postgres version as the
    database's current version — a Spock major-version upgrade changes
    Spock only, not Postgres. Requests to move to the same major version or
    to an older one are rejected: Spock cannot be downgraded in place. To
    apply a Postgres version change instead, see
    [Minor Version Upgrades](#minor-version-upgrades) or
    [Major Version Upgrades](#major-version-upgrades) above.

!!! warning

    As with any major-version upgrade, test this against a non-production
    database first, and take a fresh backup before upgrading your production
    database. See [Backup and Restore](./backup-restore.md).

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
Postgres. If you don’t see your desired version in this list, check the
[releases page](https://github.com/pgEdge/control-plane/releases/latest) to see
if there is a newer version of the Control Plane. If there is, you can follow
the [Control Plane cluster upgrade instructions](../installation/swarm-upgrading.md) to upgrade
your Control Plane cluster to the latest version.
