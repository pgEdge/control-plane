# Partial Recovery Guide

This guide explains how to recover from a partial failure scenario where one or more hosts in your cluster become unavailable. Partial recovery allows you to continue operating with reduced capacity on remaining hosts and optionally restore failed hosts later.

## Overview

Partial recovery is appropriate when:

- A host becomes unreachable (hardware failure, network isolation, VM crash, etc.)
- The Docker Swarm cluster is still functional on remaining hosts
- The Control Plane etcd cluster maintains quorum on remaining hosts
- You need to remove the failed host and continue operations

!!! note

    This guide assumes a 3-node cluster where one node has failed. The same principles apply to larger clusters, but quorum requirements differ. For a 3-node cluster, you need at least 2 healthy nodes to maintain etcd quorum.

## Prerequisites

Before starting the recovery process, ensure you have:

- Access to the Control Plane API (via `cp-req` CLI or direct API calls)
- SSH access to healthy cluster hosts for Docker Swarm management
- The failed host identified by its host ID (e.g., `host-3`)
- Knowledge of which databases have nodes on the failed host

## Phase 1: Remove the Failed Host

### Step 1.1: Force Remove the Host from Control Plane

When a host is unreachable, use the `--force` flag to remove it from the Control Plane cluster. This operation will:

- Remove the host from the etcd cluster membership
- Mark all database instances on the failed host for cleanup
- Clean up orphaned instance records from the Control Plane state

=== "curl"

    ```sh
    curl -X DELETE http://host-1:3000/v1/hosts/host-3?force=true
    ```

=== "cp-req"

    ```sh
    cp1-req remove-host host-3 --force
    ```

!!! warning

    The `--force` flag bypasses health checks and should only be used when the host is confirmed unreachable. Using it on a healthy host can cause data inconsistencies.

### Step 1.2: Update Affected Databases

After removing the host, update each affected database to remove nodes that were running on the failed host. This ensures the database operates correctly with the remaining nodes.

First, retrieve the current database configuration:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/databases/example
    ```

=== "cp-req"

    ```sh
    cp1-req get-database example
    ```

Then submit an update request with only the healthy nodes. For example, if your database had nodes n1, n2, n3 and n3 was on the failed host:

=== "curl"

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

=== "cp-req"

    ```sh
    cp1-req update-database example < reduced-spec.json
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

=== "curl"

    ```sh
    curl http://host-1:3000/v1/hosts
    ```

=== "cp-req"

    ```sh
    cp1-req list-hosts
    ```

The failed host should no longer appear in the list.

### Step 2.2: Verify Database Health

Check that each affected database shows healthy status:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/databases/example
    ```

=== "cp-req"

    ```sh
    cp1-req get-database example
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

On a healthy manager node, generate a join token:

```bash
# On host-1 (manager)
docker swarm join-token manager
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

!!! note

    Always join as a manager first using the manager token. The Control Plane requires all nodes to be swarm managers for proper service orchestration.

### Step 3.3: Deploy Control Plane Stack

After the host rejoins the swarm, redeploy the Control Plane stack to start services on the restored host:

```bash
# On any manager node
docker stack deploy -c /path/to/stack.yaml control-plane
```

### Step 3.4: Verify Service Startup

Wait for the Control Plane service to start on the restored host:

```bash
# Check service status
docker service ps control-plane_host-3

# View service logs
docker service logs control-plane_host-3 --follow
```

The service should reach `Running` state. If it shows errors, see the Troubleshooting section.

### Step 3.5: Prepare Data Directory

Ensure the data directory exists and has correct permissions on the restored host:

```bash
# On host-3
sudo mkdir -p /data/control-plane
```

## Phase 4: Join Control Plane Cluster

### Step 4.1: Get Join Token

Generate a join token from an existing cluster member:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/join-token
    ```

=== "cp-req"

    ```sh
    JOIN_TOKEN=$(cp1-req get-join-token)
    echo $JOIN_TOKEN
    ```

### Step 4.2: Join the Cluster

Call the join-cluster API on the restored host. This adds the host to the etcd cluster and registers it with the Control Plane:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/join-cluster \
        -H 'Content-Type:application/json' \
        --data "{\"token\": \"$JOIN_TOKEN\"}"
    ```

=== "cp-req"

    ```sh
    cp3-req join-cluster "$JOIN_TOKEN"
    ```

!!! important

    The join-cluster API must be called on the host being added (host-3 in this example), not on an existing cluster member.

### Step 4.3: Verify Host Rejoined

Confirm the host appears in the cluster:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/hosts
    ```

=== "cp-req"

    ```sh
    cp1-req list-hosts
    ```

The restored host should now appear in the list with `state: available`.

## Phase 5: Restore Database Capacity

### Step 5.1: Update Database with All Nodes

Update your database spec to include the restored node. The Control Plane will automatically:

- Create new database instances on the restored host
- Set up Patroni for high availability
- Configure Spock replication subscriptions
- Synchronize data from existing nodes

=== "curl"

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

=== "cp-req"

    ```sh
    cp1-req update-database example < full-spec.json
    ```

By default, the new node syncs data from n1. You can specify a different source using the `source_node` property:

```json
{
    "name": "n3",
    "host_ids": ["host-3"],
    "source_node": "n2"
}
```

### Step 5.2: Monitor Database Task

The database update is an asynchronous operation. Monitor the task progress:

=== "curl"

    ```sh
    # Get task ID from update response, then:
    curl http://host-1:3000/v1/databases/example/tasks/<task-id>
    ```

=== "cp-req"

    ```sh
    cp1-req get-task example <task-id>
    ```

### Step 5.3: Verify Full Recovery

Once the task completes, verify the database is fully operational:

=== "curl"

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

## Troubleshooting

### "Peer URLs already exists" Error During Join

This error occurs when joining the Control Plane cluster if there's a stale etcd member entry from the previous instance of the host.

```
etcdserver: Peer URLs already exists
```

**Solution**: Remove the stale etcd member from an existing cluster node.

```bash
# On host-1, find the etcd credentials
cat /data/control-plane/generated.config.json | jq '.etcd'

# List etcd members
sudo etcdctl --endpoints=https://127.0.0.1:2379 \
  --cacert=/data/control-plane/certificates/ca.crt \
  --cert=/data/control-plane/certificates/etcd-user.crt \
  --key=/data/control-plane/certificates/etcd-user.key \
  --user='root:<PASSWORD_FROM_CONFIG>' \
  member list

# Remove the stale member (use the member ID from the list)
sudo etcdctl --endpoints=https://127.0.0.1:2379 \
  --cacert=/data/control-plane/certificates/ca.crt \
  --cert=/data/control-plane/certificates/etcd-user.crt \
  --key=/data/control-plane/certificates/etcd-user.key \
  --user='root:<PASSWORD_FROM_CONFIG>' \
  member remove <MEMBER_ID>
```

Then retry the join-cluster command.

### "etcd already initialized" Error

This error occurs when the restored host has stale etcd data in its data directory.

**Solution**: Clear the data directory and restart the service.

```bash
# On the restored host
sudo rm -rf /data/control-plane/*

# Force restart the Control Plane service
docker service update --force control-plane_host-3
```

Then retry the join-cluster command.

### Instance Shows "unknown" State

If an instance shows `state: unknown` after recovery, the Patroni configuration may have stale etcd endpoints.

**Solution**: Update the database to regenerate Patroni configurations:

```bash
cp1-req update-database example < current-spec.json
```

This triggers a refresh of the Patroni configuration with correct etcd endpoints.

---

## Summary

| Phase | Step | Action | Command |
|-------|------|--------|---------|
| 1 | 1.1 | Remove failed host | `cp1-req remove-host <HOST_ID> --force` |
| 1 | 1.2 | Update database to remove failed node | `cp1-req update-database <DB_ID> < reduced-spec.json` |
| 1 | 1.3 | Clean up Docker Swarm | `docker node rm <HOST> --force` |
| 2 | 2.1 | Verify host removed | `cp1-req list-hosts` |
| 2 | 2.2 | Verify database health | `cp1-req get-database <DB_ID>` |
| 2 | 2.3 | Verify data replication | Query all nodes |
| 3 | 3.1 | Clean restored host data | `sudo rm -rf /data/control-plane/*` |
| 3 | 3.2 | Rejoin Docker Swarm | `docker swarm join --token <TOKEN> <MANAGER>:2377` |
| 3 | 3.3 | Deploy Control Plane stack | `docker stack deploy -c stack.yaml control-plane` |
| 3 | 3.4 | Verify service startup | `docker service ps control-plane_<HOST_ID>` |
| 3 | 3.5 | Prepare data directory | `sudo mkdir -p /data/control-plane` |
| 4 | 4.1 | Get join token | `cp1-req get-join-token` |
| 4 | 4.2 | Join Control Plane cluster | `cp<N>-req join-cluster "$TOKEN"` |
| 4 | 4.3 | Verify host rejoined | `cp1-req list-hosts` |
| 5 | 5.1 | Update database with all nodes | `cp1-req update-database <DB_ID> < full-spec.json` |
| 5 | 5.2 | Monitor task progress | `cp1-req get-task <DB_ID> <TASK_ID>` |
| 5 | 5.3 | Verify full recovery | Query all nodes |

