# Partial Failure Recovery Guide (Quorum Intact)

This guide explains how to recover from a partial failure scenario where one or more hosts in your cluster become unavailable. Partial recovery allows you to continue operating with reduced capacity on remaining hosts and optionally restore failed hosts later.

## Overview

Partial recovery is appropriate when:

- A host is permanently lost and cannot be recovered (hardware failure, data corruption, decommissioned infrastructure, etc.)
- The Docker Swarm cluster is still functional on remaining hosts
- The Control Plane etcd cluster maintains quorum on remaining hosts

!!! note

    This guide assumes a 3-node cluster where one node has failed. The same principles apply to larger clusters, but quorum requirements differ. For a 3-node cluster, you need at least 2 healthy nodes to maintain etcd quorum.

## Prerequisites

Before starting the recovery process, ensure you have:

- Access to the Control Plane API
- SSH access to healthy cluster hosts for Docker Swarm management
- The failed host identified by its host ID (e.g., `host-3`)
- Knowledge of which databases have nodes on the failed host (you can find this via `GET /v1/databases` — each instance in the response includes a `host_id` field)
- The Docker Swarm stack YAML file used to deploy Control Plane services

## Phase 1: Remove the Failed Host

### Step 1.1: Force Remove the Host from Control Plane

When a host is unrecoverable, use the `force` query parameter to remove it from the Control Plane cluster. This operation will:

- Remove the host from the etcd cluster membership
- Update each database to remove all instances on the failed host

```sh
curl -X DELETE http://host-1:3000/v1/hosts/host-3?force=true
```

Removing a host is an asynchronous operation. The response contains a task ID for the overall removal process, plus task IDs for each database update it performs:

```json
{
  "task": {
    "task_id": "019c243c-1eac-719e-9688-575dfb981c15",
    "type": "remove_host",
    "status": "pending"
  },
  "update_database_tasks": [
    {
      "task_id": "019c243c-1eaf-7416-ac13-489b7b40f66a",
      "database_id": "example",
      "type": "update",
      "status": "pending"
    }
  ]
}
```

You can monitor the progress of the host removal and database updates using the task endpoints:

```sh
# Monitor host removal task
curl http://host-1:3000/v1/hosts/host-3/tasks/019c243c-1eac-719e-9688-575dfb981c15

# Monitor database update task logs
curl http://host-1:3000/v1/databases/example/tasks/019c243c-1eaf-7416-ac13-489b7b40f66a/log
```

!!! warning

    The `force` query parameter bypasses health checks and should only be used when the host is confirmed unrecoverable. Using it on a healthy host can cause data inconsistencies.

### Step 1.2: Update Affected Databases (Optional)

!!! note

    Skip this step if you plan to restore the failed host later. The force remove operation in Step 1.1 is sufficient to get databases into a working state. Only perform this step if you are permanently reducing the cluster size.

If you are permanently removing the node, update each affected database to remove nodes that were running on the failed host.

First, retrieve the current database configuration:

```sh
curl http://host-1:3000/v1/databases/example
```

Then submit an update request with only the healthy nodes. For example, if your database had nodes n1, n2, n3 and n3 was on the failed host:

```sh
curl -X POST http://host-1:3000/v1/databases/example \
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
                { "name": "n2", "host_ids": ["host-2"] }
            ]
        }
    }'
```

### Step 1.3: Clean Up Docker Swarm

The failed node remains in the Docker Swarm cluster state until manually removed. On a healthy manager node, clean up the swarm:

```bash
# SSH to a healthy manager node
ssh pgedge@host-1

# List nodes to find the failed node's status
docker node ls

# If the failed node was a manager, demote it first
docker node demote <FAILED_HOSTNAME>

# Force remove the node from the swarm
docker node rm <FAILED_HOSTNAME> --force
```

Example output:
```
ID                            HOSTNAME      STATUS    AVAILABILITY   MANAGER STATUS
4aoqjp3q8jcny4kec5nadcn6x *   lima-host-1   Ready     Active         Leader
959g9937i62judknmr40kcw9r     lima-host-2   Ready     Active         Reachable
l0l51d890edg3f0ccd0xppw06     lima-host-3   Down      Active         Unreachable
```

```bash
docker node demote lima-host-3
docker node rm lima-host-3 --force
```

## Phase 2: Verify Recovery

After completing Phase 1, verify that your cluster and databases are operating correctly.

### Step 2.1: Verify Host Status

```sh
curl http://host-1:3000/v1/hosts
```

The failed host should no longer appear in the list.

### Step 2.2: Verify Database Health

Check that each affected database shows healthy status:

```sh
curl http://host-1:3000/v1/databases/example
```

Verify that:

- The database `state` is `available`
- All remaining instances show `state: available`

### Step 2.3: Verify Data Replication

Insert test data and confirm it replicates to all remaining nodes:

```bash
# Connect to the database on n1
# Insert test data
# Query on n2 to verify replication
```

At this point, your cluster is operating with reduced capacity. You can continue normal operations or proceed to Phase 3 to restore the failed host.

---

## Phase 3: Prepare the Restored Host

Once the hardware or network issue is resolved, you can add the host back to the cluster.

### Step 3.1: Clean Up Old Data

On the restored host, remove any stale Control Plane data to ensure a clean initialization:

```bash
# SSH to the restored host
ssh pgedge@host-3

# Remove old Control Plane data
sudo rm -rf /data/control-plane/*

# Verify the directory is empty
ls -la /data/control-plane/
```

!!! warning

    This step is critical. Stale etcd data will cause "etcd already initialized" errors when rejoining the cluster.

### Step 3.2: Rejoin Docker Swarm

On a healthy manager node, generate a join token. Use the appropriate token type based on whether the failed host was a manager or worker:

**For manager nodes:**
```bash
# On host-1 (existing manager)
docker swarm join-token manager
```

**For worker nodes:**
```bash
# On host-1 (existing manager)
docker swarm join-token worker
```

This outputs a command like:
```
docker swarm join --token SWMTKN-1-xxx...xxx 10.0.0.1:2377
```

On the restored host, execute the join command:

```bash
# On host-3
docker swarm join --token SWMTKN-1-xxx...xxx lima-host-1:2377
```

### Step 3.3: Prepare Data Directory

Ensure the data directory exists on the restored host:

```bash
# On host-3
sudo mkdir -p /data/control-plane
```

### Step 3.4: Deploy Control Plane Stack

After the host rejoins the swarm, redeploy the Control Plane stack to start services on the restored host:

```bash
# On any manager node
docker stack deploy -c /tmp/stack.yaml control-plane
```

### Step 3.5: Verify Service Startup

Wait for the Control Plane service to start on the restored host:

```bash
# Check service status
docker service ps control-plane_host-3

# View service logs
docker service logs control-plane_host-3 --follow
```

The service should reach `Running` state. If it shows errors, see the Troubleshooting section.

## Phase 4: Join Control Plane Cluster

### Step 4.1: Get Join Token

Generate a join token from an existing cluster member. The response contains both the token and the leader's server URL, which together form the request body for the join-cluster API:

```sh
JOIN_TOKEN="$(curl http://host-1:3000//v1/cluster/join-token)"
```

### Step 4.2: Join the Cluster

Call the join-cluster API on the restored host. This adds the host to the etcd cluster and registers it with the Control Plane:

```sh
curl -X POST http://host-3:3000/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data "${JOIN_TOKEN}"
```

!!! important

    The join-cluster API must be called on the host being added (host-3 in this example), not on an existing cluster member.

### Step 4.3: Verify Host Rejoined

Confirm the host appears in the cluster:

```sh
curl http://host-1:3000/v1/hosts
```

The restored host should now appear in the list with `state: available`.

## Phase 5: Restore Database Capacity

### Step 5.1: Update Database with All Nodes

Update your database spec to include the restored node. The Control Plane will automatically:

- Create new database instances on the restored host
- Set up Patroni for high availability
- Configure Spock replication subscriptions
- Synchronize data from existing nodes

```sh
curl -X POST http://host-1:3000/v1/databases/example \
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
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

The Control Plane will automatically choose a source node. If you want to use a specific source node, you can specify it using the `source_node` property:

```json
{
    "name": "n3",
    "host_ids": ["host-3"],
    "source_node": "n2"
}
```

### Step 5.2: Monitor Database Task

The database update is an asynchronous operation. Monitor the task progress:

```sh
# Get task ID from update response, then:
curl http://host-1:3000/v1/databases/example/tasks/<task-id>
```

### Step 5.3: Verify Full Recovery

Once the task completes, verify the database is fully operational:

```sh
curl http://host-1:3000/v1/databases/example
```

Confirm:

- All three instances show `state: available`

Test replication to the restored node:

```bash
# Insert on n1
# Verify on n3
```

---

## Summary

| Phase | Step | Action | Command |
|-------|------|--------|---------|
| 1 | 1.1 | Remove failed host | `curl -X DELETE http://<HOST>:3000/v1/hosts/<HOST_ID>?force=true` |
| 1 | 1.2 | Update database to remove failed node | `curl -X POST http://<HOST>:3000/v1/databases/<DB_ID>` |
| 1 | 1.3 | Clean up Docker Swarm | `docker node rm <HOST> --force` |
| 2 | 2.1 | Verify host removed | `curl http://<HOST>:3000/v1/hosts` |
| 2 | 2.2 | Verify database health | `curl http://<HOST>:3000/v1/databases/<DB_ID>` |
| 2 | 2.3 | Verify data replication | Query all nodes |
| 3 | 3.1 | Clean restored host data | `sudo rm -rf /data/control-plane/*` |
| 3 | 3.2 | Rejoin Docker Swarm | `docker swarm join --token <TOKEN> <MANAGER>:2377` |
| 3 | 3.3 | Prepare data directory | `sudo mkdir -p /data/control-plane` |
| 3 | 3.4 | Deploy Control Plane stack | `docker stack deploy -c stack.yaml control-plane` |
| 3 | 3.5 | Verify service startup | `docker service ps control-plane_<HOST_ID>` |
| 4 | 4.1 | Get join token | `curl http://<HOST>:3000/v1/join-token` |
| 4 | 4.2 | Join Control Plane cluster | `curl -X POST http://<NEW_HOST>:3000/v1/join-cluster` |
| 4 | 4.3 | Verify host rejoined | `curl http://<HOST>:3000/v1/hosts` |
| 5 | 5.1 | Update database with all nodes | `curl -X POST http://<HOST>:3000/v1/databases/<DB_ID>` |
| 5 | 5.2 | Monitor task progress | `curl http://<HOST>:3000/v1/databases/<DB_ID>/tasks/<TASK_ID>` |
| 5 | 5.3 | Verify full recovery | Query all nodes |

