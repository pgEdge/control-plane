# Read Replicas

A node can consist of one or more instances, where one instance serves as a primary and the others act as read replicas. The Control Plane creates one instance for each host ID in the `host_ids` array for each node. To add read replicas for a node, specify the hosts on which to deploy them on via the `host_ids` array.

This example request demonstrates creating a database with read replicas. Each host in this sample cluster is named after the AWS region where it resides:

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
                    { "name": "n1", "host_ids": ["us-east-1a", "us-east-1c"] },
                    { "name": "n2", "host_ids": ["eu-central-1a", "eu-central-1b"] },
                    { "name": "n3", "host_ids": ["ap-south-2a", "ap-south-2c"] }
                ]
            }
        }'
    ```

On database creation, the primary instance for each node will be set to the first host in the `host_ids` array. After a database is created, the primary instance may change due to a failover or switchover operation.

The pgEdge Control Plane relies on [Patroni](https://patroni.readthedocs.io) to automate and manage the state of Postgres instances. Patroni handles tasks such as monitoring, failover, and leader election, ensuring that each node always has a primary instance and that read replicas are kept in sync.

You can identify the primary instance for each node by submitting a `GET` request to the `/v1/databases/{database_id}` endpoint and inspecting the `role` and `node` fields of each instance in the `instances` field of the response:

=== "curl"

    ```sh
    curl http://us-east-1a:3000/v1/databases/example
    ```

See [High Availability Client
Connections](./connecting.md#high-availability-client-connections) for ways to connect to the read replicas in a high-availability use case.


## Switchover and Failover Operations

Switchover and failover operations allow the Control Plane to promote a read replica to become the new primary instance for a node. These operations rely on [Patroni](https://patroni.readthedocs.io/en/latest/) to manage leader election, failover, and cluster health.

For more information, see [Patroni's REST API: Switchover and Failover](https://patroni.readthedocs.io/en/latest/rest_api.html#switchover-and-failover-endpoints).

In addition to manual control, the system also supports **automatic failover** when Patroni detects a primary outage.

### Switchover (Planned Role Change)

A switchover is a planned operation that transfers the primary role to a selected read replica while both instances are healthy. It can be executed immediately or scheduled for a later time.

#### Checking Instance Health

Before performing a switchover, ensure that the instance is healthy. The `patroni_state` field in the `postgres` section indicates the current status:

=== "curl"

    ```sh
    curl -X GET http://host-3:3000/v1/databases/example \
    -H "Content-Type: application/json" 
    ```
```
    {
    "id": "example-n1-689qacsi",
    "node_name": "n1",
    "postgres": {
        "patroni_state": "running",
        "role": "primary",
        "version": "17.6"
    },
    "spock": {
        "read_only": "off",
        "subscriptions": [
        { "name": "sub_n2_n1", "provider_node": "n2", "status": "replicating" },
        { "name": "sub_n3_n1", "provider_node": "n3", "status": "replicating" }
        ],
        "version": "5.0.4"
    },
    "state": "available"
    }

```
In this example, `"patroni_state": "running"` confirms that the instance is healthy.

#### Executing a Switchover

When calling the switchover endpoint, you may specify a `candidate_instance_id`. If omitted, Patroni automatically selects a healthy replica as the new primary after the current leader steps down.

This sample request demonstrates a switchover operation on `example-n1-b`:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/switchover \
    -H 'Content-Type:application/json' \
    --data '{
        "node": "n1",
        "candidate_instance_id": "example-n1-b",
        "scheduled_at": "2025-09-24T18:46:05Z"
    }'
    ```

If `candidate_instance_id` is omitted, the system automatically selects a suitable replica for promotion. 

The `scheduled_at` field allows the switchover to be executed at a specific scheduled time.

**Behavior:**

- If the specified `candidate_instance_id` is already primary, the operation is skipped.

- An invalid `candidate_instance_id` will result in a `404 Not Found` error.

- Concurrent switchover attempts are rejected with an `already in progress` message.


### Failover (Manual Primary Replacement)

A failover is used to manually promote a replica to primary. This is typically performed when no healthy synchronous replicas are available (e.g., promoting an asynchronous standby). However, failover is not restricted to unhealthy clusters — it can also be triggered on a healthy cluster if required.

#### Executing a Failover

When calling the failover endpoint, you may specify a `candidate_instance_id`. If omitted, Control Plane automatically selects a healthy replica to promote as the new primary.

This sample request demonstrates a failover operation on `example-n1-c`:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases/example/failover \
    -H 'Content-Type:application/json' \
    --data '{
        "node": "n1",
        "candidate_instance_id": "example-n1-c",
        "skip_validation": true
    }'
    ```

If `candidate_instance_id` is omitted, the Control Plane automatically selects the best available replica for promotion.

The optional `skip_validation` flag bypasses cluster health checks, allowing a forced failover.

**Behavior:**

- On healthy clusters, failover requests are rejected unless `skip_validation: true` is provided(to prevent accidental failovers).

- If the `candidate_instance_id` is already the primary, the failover operation completes without changes.

- An invalid `candidate_instance_id` will result in a `404 Not Found` error.

- Concurrent failover requests are rejected with `failover already in progress` message.

