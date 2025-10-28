# Read Replicas

A node can consist of one or more instances, where one instance serves as a primary and the others act as read replicas. The Control Plane creates one instance for each host ID in the `host_ids` array for each node. To add read replicas for a node, specify which hosts to deploy them on via the `host_ids` array.

This example request demonstrates creating a database with read replicas. Each host in this example cluster is named after the AWS region where it resides:

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

The pgEdge Control Plane relies on [Patroni](https://patroni.readthedocs.io) to automate and manage the state of PostgreSQL instances. Patroni handles tasks such as monitoring, failover, and leader election, ensuring that each node always has a primary instance and that read replicas are kept in sync.

You can identify the primary instance for each node by submitting a `GET` request to the `/v1/databases/{database_id}` endpoint and inspecting the `role` and `node` fields of each instance in the `instances` field of the response:

=== "curl"

    ```sh
    curl http://us-east-1a:3000/v1/databases/example
    ```

See [High availability client
connections](./connecting.md#high-availability-client-connections) for ways to connect to the read replicas in a high-availability use case.

---

### Switchover and Failover Operations

Switchover and Failover operations allow the Control Plane to promote a read replica to become the new primary instance for a node.

#### Switchover (Planned Role Change)

A **switchover** is a planned operation that transfers the primary role to a selected read replica while both instances are healthy.  
This allows maintenance or planned leadership change with zero downtime.

**Example request:**

```sh
curl -X POST http://host-3:3000/v1/databases/example/switchover \
  -H 'Content-Type:application/json' \
  --data '{
      "node": "n1",
      "candidate_instance_id": "example-n1-b",
      "scheduled_at": "2025-09-24T18:46:05Z"
  }'
```

If `candidate_instance_id` is omitted, the system automatically selects a healthy replica as the switchover target.  
You can optionally schedule the switchover to execute at a specific time using the `scheduled_at` field.

**Behavior:**

- If the candidate is already primary, the switchover is skipped.

- Invalid candidate IDs result in a `404 Not Found` error.

- Concurrent switchover attempts are rejected with an `already in progress` message.


#### Failover (Planned Primary Replacement)

A **failover** is a planned operation that promotes a selected read replica to become the new primary instance.

**Example request:**

```sh
curl -X POST http://host-3:3000/v1/databases/example/failover \
  -H 'Content-Type:application/json' \
  --data '{
      "node": "n1",
      "candidate_instance_id": "example-n1-c",
      "skip_validation": true
  }'
```

If `candidate_instance_id` is omitted, the Control Plane automatically selects the best replica to promote.  
The optional `skip_validation` flag allows overriding cluster health checks to force failover.

**Behavior:**

- Healthy clusters will reject failover unless `skip_validation: true` is provided.

- If the candidate is already the leader, the failover operation completes without change.

- Invalid candidate IDs result in `404 Not Found` errors.

- Concurrent failover requests are rejected with `failover already in progress` messages.

