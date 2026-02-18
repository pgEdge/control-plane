# Partial Failure Recovery (Quorum Intact)

This guide covers recovery of both the **etcd cluster** and **Docker Swarm** when quorum remains intact but one or more hosts are lost. Because the etcd cluster can still accept writes and Docker Swarm has enough managers, the Control Plane API and service orchestration remain operational throughout recovery.

## When to Use This Guide

Use this guide when:

- Majority of server-mode hosts are still online (etcd quorum intact)
- Docker Swarm has enough managers to accept commands
- Control Plane API is accessible
- One or more hosts (server-mode or client-mode) are unavailable

**Examples:**

| Scenario | Quorum? | This Guide? |
|----------|---------|-------------|
| 1 of 3 server-mode hosts lost | Yes (2/3) | Yes |
| 1 or more client-mode hosts lost | Yes | Yes |
| All client-mode hosts lost | Yes | Yes |
| 2 of 3 server-mode hosts lost | No | No — see [Quorum Loss Recovery](full-recovery.md) |

## Prerequisites

Before starting, ensure you have:

- Access to the Control Plane API on a healthy host
- SSH access to healthy cluster hosts (for Docker Swarm management)
- The failed host identified by its host ID (e.g., `host-3`)
- The Docker Swarm stack YAML file used to deploy Control Plane services

Determine which recovery path applies to your situation:

| Condition | Recovery Path |
|-----------|---------------|
| Lost host is still accessible (SSH works, Docker running) | [Path A: Host Accessible](#phase-3a-restore-accessible-host) |
| Lost host is destroyed or unreachable (hardware failure, VM deleted) | [Path B: Host Destroyed](#phase-3b-provision-new-host) |

---

## Phase 1: Remove the Failed Host

Update each affected database to remove the lost host, then remove the host from the Control Plane. The host removal will fail if any database still has instances on that host, so complete Step 1.1 before Step 1.2.

### Step 1.1: Update Affected Databases to Remove the Lost Host

For each database that has instances on the failed host, submit an update with the `remove_host` query parameter and a spec that excludes the failed host. This removes those instances from the database and allows the host to be removed in Step 1.2.

```sh
# Get the current database spec, then submit an update with remove_host and the desired spec (only healthy nodes)
curl http://<HEALTHY_HOST>:3000/v1/databases/<DB>
```

Example: remove `host-3` from a database that had nodes on host-1, host-2, and host-3:

```sh
curl -X POST "http://<HEALTHY_HOST>:3000/v1/databases/<DB>?remove_host=<LOST_HOST_ID>" \
    -H 'Content-Type: application/json' \
    -d '{
        "spec": {
            "database_name": "<DB_NAME>",
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

Wait for each database update task to complete. Monitor task status using the [Tasks and Logs](../using/tasks-logs.md) documentation.

### Step 1.2: Remove the Host from Control Plane

After all affected databases have been updated, remove the host from the Control Plane:

```sh
curl -X DELETE "http://<HEALTHY_HOST>:3000/v1/hosts/<LOST_HOST_ID>"
```

Monitor the removal task:

```sh
curl http://<HEALTHY_HOST>:3000/v1/hosts/<LOST_HOST_ID>/tasks/<TASK_ID>
```

### Step 1.3: Clean Up Docker Swarm

The failed node stays in the Swarm until manually removed. On a healthy manager node:

```bash
# List nodes to identify the failed one
docker node ls
```

Example output:

```
ID                            HOSTNAME      STATUS    AVAILABILITY   MANAGER STATUS
4aoqjp3q8jcny4kec5nadcn6x *   lima-host-1   Ready     Active         Leader
959g9937i62judknmr40kcw9r     lima-host-2   Ready     Active         Reachable
l0l51d890edg3f0ccd0xppw06     lima-host-3   Down      Active         Unreachable
```

```bash
# If the failed node was a manager, demote it first
docker node demote <FAILED_HOSTNAME>

# Force remove the node
docker node rm <FAILED_HOSTNAME> --force
```

---

## Phase 2: Verify Recovery

### Step 2.1: Verify Host Status

```sh
curl http://<HEALTHY_HOST>:3000/v1/hosts
```

The failed host should no longer appear in the list.

### Step 2.2: Verify Database Health

```sh
curl http://<HEALTHY_HOST>:3000/v1/databases/<DB>
```

Verify that:

- Database `state` is `available`
- All remaining instances show `state: available`

### Step 2.3: Verify Data Replication

Insert test data and confirm it replicates to all remaining nodes.

At this point, your cluster is operating with reduced capacity. Continue to Phase 3 to restore the lost host.

---

## Phase 3A: Restore Accessible Host

**Use this path when the lost host is still reachable via SSH and Docker is running.**

### Step 3A.1: Stop the Host Service

On a Swarm manager node (skip if the service no longer exists):

```bash
docker service scale control-plane_<LOST_HOST_ID>=0
```

### Step 3A.2: Clear Host State

SSH to the lost host and clear stale data.

**For server-mode hosts:**

```bash
PGEDGE_DATA_DIR="<path-to-data-dir>"
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

**For client-mode hosts:**

```bash
PGEDGE_DATA_DIR="<path-to-data-dir>"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

### Step 3A.3: Start the Host Service

**If the Swarm service still exists:**

```bash
docker service scale control-plane_<LOST_HOST_ID>=1
```

**If Swarm no longer has the service definition:**

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
```

Now proceed to [Phase 4: Join Control Plane Cluster](#phase-4-join-control-plane-cluster).

---

## Phase 3B: Provision New Host

**Use this path when the host is destroyed and must be recreated from scratch.**

### Step 3B.1: Create New Host

Provision the replacement host using your infrastructure tooling. For Lima-based environments:

```bash
cd e2e/fixtures
ansible-playbook \
    --extra-vars='@vars/lima.yaml' \
    --extra-vars='@vars/small.yaml' \
    --extra-vars='target_host=host-3' \
    setup_new_host.yaml
```

For other environments, provision according to your infrastructure standards and install prerequisites (Docker, etc.).

### Step 3B.2: Verify Connectivity

```bash
ssh pgedge@<HEALTHY_HOST> 'ping -c 1 <NEW_HOSTNAME>'
```

### Step 3B.3: Rejoin Docker Swarm

On a healthy manager node, generate a join token:

**For manager nodes:**

```bash
docker swarm join-token manager
```

**For worker nodes:**

```bash
docker swarm join-token worker
```

On the new host, execute the join command:

```bash
docker swarm join --token SWMTKN-1-xxx...xxx <MANAGER_HOST>:2377
```

### Step 3B.4: Verify Swarm Membership

```bash
docker node ls
```

The new host should appear with `STATUS: Ready`.

### Step 3B.5: Prepare Data Directory

On the new host:

```bash
sudo mkdir -p /data/control-plane
```

### Step 3B.6: Deploy Control Plane Stack

On any manager node:

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
```

### Step 3B.7: Verify Service Startup

```bash
docker service ps control-plane_<HOST_ID>
docker service logs control-plane_<HOST_ID> --follow
```

The service should reach `Running` state.

Now proceed to [Phase 4: Join Control Plane Cluster](#phase-4-join-control-plane-cluster).

---

## Phase 4: Join Control Plane Cluster

### Step 4.1: Get Join Token

```sh
JOIN_TOKEN="$(curl http://<HEALTHY_HOST>:3000/v1/cluster/join-token)"
```

### Step 4.2: Join the Cluster

Call the join API **on the host being added** (not on an existing member):

```sh
curl -X POST http://<NEW_HOST>:3000/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data "${JOIN_TOKEN}"
```

!!! important

    The join-cluster API must be called on the host being added, not on an existing cluster member.

### Step 4.3: Verify Host Joined

```sh
curl http://<HEALTHY_HOST>:3000/v1/hosts
```

The restored host should appear with `status: reachable` and the correct `etcd_mode` (`server` or `client`).

---

## Phase 5: Restore Database Capacity

### Step 5.1: Update Database with All Nodes

Update your database spec to include the restored node. Control Plane will automatically create instances, configure replication, and synchronize data:

```sh
curl -X POST http://<HEALTHY_HOST>:3000/v1/databases/<DB> \
    -H 'Content-Type:application/json' \
    --data '{
        "spec": {
            "database_name": "<DB_NAME>",
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

To use a specific source node for data synchronization:

```json
{ "name": "n3", "host_ids": ["host-3"], "source_node": "n2" }
```

### Step 5.2: Monitor Database Update

```sh
curl http://<HEALTHY_HOST>:3000/v1/databases/<DB>/tasks/<TASK_ID>
curl http://<HEALTHY_HOST>:3000/v1/databases/<DB>/tasks/<TASK_ID>/log
```

### Step 5.3: Verify Full Recovery

```sh
curl http://<HEALTHY_HOST>:3000/v1/databases/<DB>
```

Confirm:

- Database `state` is `available`
- All instances show `state: available`
- All subscriptions show `status: replicating`

---

## Recovery Order for Multiple Hosts

If multiple hosts were lost, recover them in this order:

1. **Server-mode hosts first** — restore etcd membership
2. **Client-mode hosts after** — can rejoin once etcd is stable

Repeat Phases 1 through 5 for each host, updating variables accordingly.

---

## Verification Checklist

After recovery, verify:

- [ ] All hosts show `status: reachable` in `/v1/hosts`
- [ ] Server-mode hosts show `etcd_mode: server`
- [ ] Client-mode hosts show `etcd_mode: client`
- [ ] Databases show instances on all hosts
- [ ] All database instances are in `available` state
- [ ] Data replicates correctly across all nodes

---

## Troubleshooting

### "duplicate host ID" error when rejoining

**Cause:** Host record still exists in etcd.

**Solution:** Complete Step 1.1 and Step 1.2 (update databases, then remove host) and wait for the removal task to finish before rejoining.

### Host joins but shows as unreachable

**Cause:** Network connectivity issue or service not fully started.

**Solution:**

- Check service logs: `docker service logs control-plane_<HOST_ID>`
- Verify network connectivity between hosts
- Wait for the service to fully initialize and check again

### Database instances don't restore

**Cause:** Database spec doesn't include the recovered host.

**Solution:** Update the database spec (Phase 5) to include the recovered host. Control Plane will automatically provision new instances.

### "etcd already initialized" error on rejoin

**Cause:** Stale etcd data on the host.

**Solution:** Clear the data directory before joining (see Step 3A.2 for the rejoin path; on a newly provisioned host, remove any existing etcd data in the data directory).

---

## Summary

| Phase | Step | Action | Path |
|-------|------|--------|------|
| 1 | 1.1 | Update databases to remove lost host | Both |
| 1 | 1.2 | Remove host from Control Plane | Both |
| 1 | 1.3 | Clean up Docker Swarm | Both |
| 2 | 2.1 | Verify host removed | Both |
| 2 | 2.2 | Verify database health | Both |
| 2 | 2.3 | Verify data replication | Both |
| 3A | 3A.1–3A.3 | Stop service, clear state, restart | Host Accessible |
| 3B | 3B.1–3B.7 | Provision new host, rejoin Swarm, deploy | Host Destroyed |
| 4 | 4.1–4.3 | Join Control Plane cluster | Both |
| 5 | 5.1–5.3 | Restore database capacity | Both |
