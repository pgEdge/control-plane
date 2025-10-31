# Upgrading a Database

## Minor Version Upgrades

The Control Plane supports minor Postgres version upgrades, such as upgrading
from Postgres 17.5 to 17.6, via the API. The Postgres version is a field in the
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
    curl -X POST http://host-3:3000/v1/databases \
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

## Major Version Upgrades

The Control Plane doesn't currently support major Postgres version upgrades. We
recommend that you [create a new Database](./create-db.md) with your desired
version, and then use
[`pg_dump`](https://www.postgresql.org/docs/current/backup-dump.html) to migrate
your data to the new database.

## Which Versions Are Available

You can see the list of supported Postgres versions for each host by submitting
a `GET` request to the `/v1/hosts` endpoint of any host in the cluster and
inspecting the `supported_pgedge_versions` fields in the output:

=== "curl"

    ```
    curl http://host-3:3000/v1/hosts
    ```

### What if a Version Isn’t Listed

Newer versions of the Control Plane server will support newer versions of
Postgres. If you don't see your desired version in this list, check the
[releases page](https://github.com/pgEdge/control-plane/releases/latest) to see
if there is a newer version of the Control Plane. If there is, you can follow
the [Control Plane cluster upgrade
instructions](../installation.md#upgrading-the-control-plane) to upgrade
your Control Plane cluster to the latest version.
