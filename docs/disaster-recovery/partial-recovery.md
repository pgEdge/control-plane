# Partial Quorum Loss Recovery Guide

## Overview

Recovery guide for when **quorum remains intact** but one or more hosts are lost. In these scenarios, the etcd cluster can still accept writes and elect leaders, making recovery simpler than total quorum loss.

## When to Use This Guide

Use this guide when:
- ✅ **Quorum is intact** (majority of server-mode hosts are still online)
- ✅ **Control Plane API is accessible** (you can make API calls)
- ✅ **etcd cluster is operational** (can accept writes)

**Examples:**
- Single client-mode host down
- Multiple client-mode hosts down
- Single server-mode host down (when you have 3+ server-mode hosts)
- All client-mode hosts down

## Host Recovery (Client-Mode or Server-Mode)

### When This Applies

- One or more hosts are lost (client-mode or server-mode)
- Quorum remains intact (enough server-mode hosts are still online)
- Control Plane API is accessible
- **The lost host is accessible** (you can SSH to it and run Docker commands)

**Important:** This recovery process requires SSH access to the lost host to clear state and restart services. If the host is truly unreachable (no SSH access), this process does not apply. You would need to restore the host first (e.g., from a backup or recreate it), then follow these steps.

### Impact

- Lost hosts are unreachable
- Database instances on lost hosts are offline
- Cluster continues operating with remaining hosts
- Quorum remains intact (if server-mode hosts are lost, enough others remain)

### Recovery Steps

Use this flow for each lost host. The steps are identical for both client-mode and server-mode hosts.

#### Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-data-dir>"
API_PORT=<api-port>
SERVER_HOST="http://<healthy-server-mode-host-ip>:${API_PORT}"
LOST_HOST="http://<lost-host-ip>:${API_PORT}"
LOST_HOST_ID="<lost-host-id>"
LOST_SERVICE="control-plane_<lost-host-id>"
```

#### Step 1: Remove the Lost Host Record

**Important:** You must remove the host record before rejoining, as host IDs must be unique.

**Run from a node with API access:**

```bash
RESP=$(curl -sS -X DELETE "${SERVER_HOST}/v1/hosts/${LOST_HOST_ID}?force=true")
echo "${RESP}"
```

**Important:** The delete operation is asynchronous and returns a task. Monitor the task status using the [Tasks and Logs](../../using/tasks-logs.md) documentation. Wait for the deletion task to complete before proceeding to Step 2.

#### Step 2: Stop the Host Service

**Run on a Swarm manager node. Skip this step if the service name does not exist.**

**Note:** This step stops the Control Plane service that may still be running on the lost host. We stop it, clear state, and restart to rejoin cleanly. If the host is truly unreachable, you can't SSH to run Step 3, so this recovery process doesn't apply.

```bash
docker service scale "${LOST_SERVICE}=0"
```

#### Step 3: Clear Host State

**Run on the lost host node.**

**For server-mode hosts:**
```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

**For client-mode hosts:**
```bash
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

#### Step 4A: Start the Host Service

**Run on a Swarm manager node. Use this when the service already exists.**

```bash
docker service scale "${LOST_SERVICE}=1"
```

#### Step 4B: Redeploy the Stack

**Run on a Swarm manager node. Use this when Swarm no longer has the service definition.**

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
```

#### Step 5: Join the Lost Host to the Cluster

Join the lost host to the cluster. See [Initializing the Control Plane](../../installation/installation.md#initializing-the-control-plane).

#### Step 6: Verify Host Health

**Run from a node with API access:**

```bash
curl -sS "${SERVER_HOST}/v1/hosts"
# Look for the host with id matching ${LOST_HOST_ID} in the response
# Verify it shows status: "reachable" and the correct etcd_mode
```

**Expected fields:**
- `status` equals `"reachable"`
- `etcd_mode` equals `"server"` for server-mode hosts or `"client"` for client-mode hosts

#### Step 7: Restore Database Instances (If Needed)

Update the database spec to include the recovered host in the node's `host_ids` array. Control Plane automatically uses [zero downtime add node](../../using/update-db.md) when you update the database. See [Updating a Database](../../using/update-db.md) for details.

#### Repeat for Each Lost Host

Update the variables for the next host, then repeat steps 1 to 6. If you need to restore database instances on the recovered host, also complete Step 7.

## Recovery Order for Multiple Hosts

If multiple hosts were lost, recover them in this order:

1. **Server-mode hosts first** - Restore quorum if needed
2. **Client-mode hosts after** - Can rejoin once quorum is stable

**Example:** If you lost host-3 (server-mode) and host-2 (client-mode):
1. Recover host-3 first (server-mode)
2. Then recover host-2 (client-mode)

## Verification Checklist

After recovery, verify:

- [ ] All hosts show `status: "reachable"` in `/v1/hosts`
- [ ] Server-mode hosts show `etcd_mode: "server"`
- [ ] Client-mode hosts show `etcd_mode: "client"`
- [ ] Databases show instances on recovered hosts
- [ ] Database instances are in `available` state
- [ ] Can query databases on recovered hosts

## Troubleshooting

### Issue: "duplicate host ID" error when rejoining

**Cause:** Host record still exists in etcd.

**Solution:** Remove the host record first (Step 1) and wait for the task to complete before rejoining.

### Issue: Host joins but shows as unreachable

**Cause:** Network connectivity issue or service not fully started.

**Solution:**
- Check service logs: `docker service logs control-plane_<host-id>`
- Verify network connectivity between hosts
- Wait a bit longer and check again

### Issue: Database instances don't restore

**Cause:** Database spec doesn't include the recovered host.

**Solution:** Update the database spec (Step 7) to include the recovered host in the node's `host_ids` array. Control Plane will automatically use [zero downtime add node](../../using/update-db.md).
