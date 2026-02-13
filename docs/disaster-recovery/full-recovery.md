# Complete Failure Recovery Guide (No Quorum)

This guide explains how to recover from a failure scenario where one or more hosts in your cluster are completely lost (hardware destroyed, VM deleted, etc.) and need to recreate the failed machine from scratch. Unlike partial recovery where hosts can be repaired, full recovery involves provisioning new infrastructure.

## Overview

Full recovery is required when:

- A host's infrastructure is completely destroyed and must be recreated
- The VM or physical host needs to be reprovisioned from scratch
- The host cannot be repaired and must be replaced with new hardware

## Prerequisites

Before starting the recovery process, ensure you have:

- SSH access to healthy cluster hosts
- The Docker Swarm stack YAML file used to deploy Control Plane services
- Ability to provision new infrastructure (VM, physical server, etc.)
- Network connectivity between all hosts

## Phase 1: Remove the Failed Host

### Step 1.1: Force Remove the Host from Control Plane

Remove the host which is completly lost, use the `force` query parameter to remove it from the Control Plane cluster. This operation will:

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

    The `force` query parameter bypasses health checks and should only be used when the host is confirmed lost. Using it on a healthy host can cause data inconsistencies.

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

The lost node remains in the Docker Swarm cluster state until manually removed. On a healthy manager node, clean up the swarm:

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

The lost host should no longer appear in the list.

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

## Phase 3: Provision New Host

### Step 3.1: Create New Host

Provision the replacement host using your infrastructure tooling. 
For example Lima-based environments:

```bash
# Using the setup_new_host.yaml playbook
cd e2e/fixtures
ansible-playbook \
    --extra-vars='@vars/lima.yaml' \
    --extra-vars='@vars/small.yaml' \
    --extra-vars='target_host=host-3' \
    setup_new_host.yaml
```

For different environments, provision the new server according to your infrastructure standards and install the required prerequisites (Docker, etc.).

### Step 3.2: Configure Host Resolution

Verify connectivity:

```bash
ssh pgedge@host-1 'ping -c 1 lima-host-3'
```
----

### Step 3.3: Rejoin Docker Swarm

On a healthy manager node, generate a join token. Use the appropriate token type based on whether the lost host was a manager or worker:

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

On the new recreated host, execute the join command:

```bash
# On host-3
docker swarm join --token SWMTKN-1-xxx...xxx lima-host-1:2377
```

### Step 3.4: Verify Swarm Membership

Confirm the new host appears in the swarm:

```bash
# On any manager node
docker node ls
```

Expected output:

```
ID                            HOSTNAME      STATUS    AVAILABILITY   MANAGER STATUS
4aoqjp3q8jcny4kec5nadcn6x *   lima-host-1   Ready     Active         Leader
959g9937i62judknmr40kcw9r     lima-host-2   Ready     Active         Reachable
l0l51d890edg3f0ccd0xppw06     lima-host-3   Ready     Active         Reachable
```

## Phase 4: Deploy Control Plane Service

### Step 4.1: Prepare Data Directory

Ensure the data directory exists on the newly created host:

```bash
# On host-3
sudo mkdir -p /data/control-plane
```

### Step 4.2: Deploy Control Plane Stack

After the host rejoins the swarm, redeploy the Control Plane stack to start services on the newly created host:

```bash
# On any manager node
docker stack deploy -c /tmp/stack.yaml control-plane
```

### Step 4.3: Verify Service Startup

Wait for the Control Plane service to start on the restored host:

```bash
# Check service status
docker service ps control-plane_host-3

# View service logs
docker service logs control-plane_host-3 --follow
```

The service should reach `Running` state. 

## Phase 5: Join Control Plane Cluster

### Step 5.1: Get Join Token

Generate a join token from an existing cluster member:

```sh
JOIN_TOKEN="$(curl http://host-1:3000/v1/cluster/join-token)"
```

### Step 5.2: Join the Cluster

Call the join-cluster API on the new host:

```sh
curl -X POST http://host-3:3000/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data "${JOIN_TOKEN}"
```

!!! important

    The join-cluster API must be called on the host being added (host-3), not on an existing cluster member.

### Step 5.3: Verify Host Joined

Confirm the host appears in the cluster:

```sh
curl http://host-1:3000/v1/hosts
```

The new host should appear with `state: available`.

### Step 5.4: Update Database with New Node

Update your database spec to include the new node. The Control Plane will automatically:

- Create new database instances on the new host
- Set up Patroni for high availability
- Configure Spock replication subscriptions
- Synchronize data from existing nodes

```sh
curl -X POST http://host-1:3000/v1/databases/storefront \
    -H 'Content-Type:application/json' \
    --data '{
        "spec": {
            "database_name": "storefront",
            "database_users": [
                {
                    "username": "admin",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

### Step 5.5: Monitor Database Update

The database update is an asynchronous operation. Monitor the task progress:

```sh
# Get task ID from update response, then:
curl http://host-1:3000/v1/databases/storefront/tasks/<task-id>

# View detailed logs
curl http://host-1:3000/v1/databases/storefront/tasks/<task-id>/log
```

### Step 5.6: Verify Full Recovery

Once the task completes, verify the database is fully operational:

```sh
curl http://host-1:3000/v1/databases/storefront
```

Confirm:

- Database `state` is `available`
- All three instances show `state: available`
- All subscriptions show `status: replicating`

## Phase 6: Post-Recovery Verification

### Step 6.1: Verify Data Replication

Test that data replicates correctly to the new node:

```bash
# Insert on n1
# Verify on n3
```

### Step 6.2: Verify Cluster Health

Check overall cluster health:

```sh
# List all hosts
curl http://host-1:3000/v1/hosts

# Check database status
curl http://host-1:3000/v1/databases/storefront
```
---


## Summary

| Phase | Step | Action | Command |
|-------|------|--------|---------|
| 1 | 1.1 | Force remove host from Control Plane | `curl -X DELETE http://<HOST>:3000/v1/hosts/<HOST_ID>?force=true` |
| 1 | 1.2 | Update affected databases (optional) | `curl -X POST http://<HOST>:3000/v1/databases/<DB>` |
| 1 | 1.3 | Clean up Docker Swarm | `docker node rm <HOST> --force` |
| 2 | 2.1 | Verify host removed | `curl http://<HOST>:3000/v1/hosts` |
| 2 | 2.2 | Verify database health | `curl http://<HOST>:3000/v1/databases/<DB>` |
| 2 | 2.3 | Verify data replication | Query remaining nodes |
| 3 | 3.1 | Create new host | Infrastructure-specific |
| 3 | 3.2 | Configure host resolution | Verify connectivity with `ping` |
| 3 | 3.3 | Rejoin Docker Swarm | `docker swarm join --token <TOKEN> <MANAGER>:2377` |
| 3 | 3.4 | Verify swarm membership | `docker node ls` |
| 4 | 4.1 | Prepare data directory | `sudo mkdir -p /data/control-plane` |
| 4 | 4.2 | Deploy Control Plane stack | `docker stack deploy -c stack.yaml control-plane` |
| 4 | 4.3 | Verify service startup | `docker service ps control-plane_<HOST>` |
| 5 | 5.1 | Get join token | `curl http://<HOST>:3000/v1/cluster/join-token` |
| 5 | 5.2 | Join Control Plane cluster | `curl -X POST http://<NEW_HOST>:3000/v1/cluster/join` |
| 5 | 5.3 | Verify host joined | `curl http://<HOST>:3000/v1/hosts` |
| 5 | 5.4 | Update database with new node | `curl -X POST http://<HOST>:3000/v1/databases/<DB>` |
| 5 | 5.5 | Monitor task progress | `curl http://<HOST>:3000/v1/databases/<DB>/tasks/<ID>/log` |
| 5 | 5.6 | Verify full recovery | `curl http://<HOST>:3000/v1/databases/<DB>` |
| 6 | 6.1 | Verify data replication | Query all nodes |
| 6 | 6.2 | Verify cluster health | `curl http://<HOST>:3000/v1/hosts` |
