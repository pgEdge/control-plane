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

## Understanding Quorum

### Quorum Calculation

- **Quorum** = floor(N/2) + 1, where N = total number of server-mode hosts
- **Server-mode hosts** run embedded etcd and count toward quorum
- **Client-mode hosts** connect to etcd but do NOT count toward quorum

### Examples

| Total Server-Mode Hosts | Quorum Requirement | Can Lose |
|------------------------|-------------------|----------|
| 3 | 2 | 1 host (quorum intact) |
| 5 | 3 | 2 hosts (quorum intact) |
| 7 | 4 | 3 hosts (quorum intact) |

**Key Point:** As long as you have at least the quorum threshold of server-mode hosts online, quorum is intact and you can use this guide.

## Scenario 1: Client-Mode Host Recovery

### When This Applies

- One or more client-mode hosts are lost
- Quorum remains intact (server-mode hosts are still online)
- Control Plane API is accessible

### Impact

- Lost hosts are unreachable
- Database instances on lost hosts are offline
- Cluster continues operating with remaining hosts
- No impact on etcd quorum

### Recovery Steps

Use this flow for each lost client-mode host.

#### Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-data-dir>"
API_PORT=<api-port>
SERVER_HOST="http://<server-mode-host-ip>:${API_PORT}"
CLIENT_HOST="http://<client-mode-host-ip>:${API_PORT}"
```

#### Step 1: Stop the Control Plane Service

```bash
# On Swarm manager node
docker service scale control-plane_<lost-client-host-id>=0
```

#### Step 2: Remove generated.config.json

```bash
# On lost client-mode host node
rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### Step 3: Start the Control Plane Service

```bash
# On Swarm manager node
docker service scale control-plane_<lost-client-host-id>=1
sleep 10
```

#### Step 4: Request a Join Token

```bash
JOIN_TOKEN=$(curl -sS ${SERVER_HOST}/v1/cluster/join-token | jq -r ".token")
```

#### Step 5: Join the Host to the Cluster

```bash
curl -sS -X POST ${CLIENT_HOST}/v1/cluster/join \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_HOST}\"}"
```

#### Repeat for Each Lost Client-Mode Host

Update `CLIENT_HOST` for the next host, then repeat steps 1 to 5.

## Scenario 2: Server-Mode Host Recovery (Quorum Intact)

### When This Applies

- One server-mode host is lost
- Quorum remains intact (enough other server-mode hosts are online)
- Control Plane API is accessible

### Impact

- Lost server-mode host is unreachable
- Database instances on lost host are offline
- Quorum remains intact (other server-mode hosts maintain quorum)
- Cluster continues operating

### Assumptions

- You lost the host and its Control Plane data volume
- You will remove the host record before you attempt join
- You will restore any database instances that lived on the lost host

### Recovery Steps

#### Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-data-dir>"
API_PORT=<api-port>
SERVER_HOST="http://<healthy-server-mode-host-ip>:${API_PORT}"
LOST_SERVER_HOST="http://<lost-server-mode-host-ip>:${API_PORT}"
LOST_HOST_ID="<lost-server-host-id>"
LOST_SERVICE="control-plane_<lost-server-host-id>"
```

#### Step 1: Remove the Lost Host Record

**Run from a node with API access:**

```bash
RESP=$(curl -sS -X DELETE "${SERVER_HOST}/v1/hosts/${LOST_HOST_ID}?force=true")
TASK_ID=$(echo "${RESP}" | jq -r '.task.task_id // .task.id // .id // empty')
echo "${RESP}" | jq '.'
```

#### Step 2: Stop the Host Service

**Run on a Swarm manager node. Skip this step if the service name does not exist.**

```bash
docker service scale "${LOST_SERVICE}=0"
```

#### Step 3: Clear Server-Mode State

**Run on the lost host node. These paths exist only when some data survived.**

```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

#### Step 4A: Start the Host Service

**Run on a Swarm manager node. Use this when the service already exists.**

```bash
docker service scale "${LOST_SERVICE}=1"
sleep 10
```

#### Step 4B: Redeploy the Stack

**Run on a Swarm manager node. Use this when Swarm no longer has the service definition.**

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
sleep 10
```

#### Step 5: Request a Join Token

**Run from a node with API access:**

```bash
JOIN_TOKEN=$(curl -sS "${SERVER_HOST}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="${SERVER_HOST}"
```

#### Step 6: Join the Lost Host

**Run from a node with API access:**

```bash
curl -sS -X POST "${LOST_SERVER_HOST}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

#### Step 7: Verify Host Health

**Run from a node with API access:**

```bash
curl -sS "${SERVER_HOST}/v1/hosts" | jq \
    'if type=="array" then .[] else to_entries[].value end | select(.id=="'"${LOST_HOST_ID}"'") | {id, status, etcd_mode}'
```

**Expected fields:**
- `status` equals `"reachable"`
- `etcd_mode` equals `"server"`

#### Step 8: Restore Database Instances

**List databases and node placement:**

```bash
curl -sS "${SERVER_HOST}/v1/databases" | jq '.[] | {id, nodes: .spec.nodes}'
```

**Fetch one database spec before you change it:**

```bash
curl -sS "${SERVER_HOST}/v1/databases/<database-id>" | jq '.spec'
```

**Update the database spec to place a node back on the recovered host:**

```bash
curl -sS -X PUT "${SERVER_HOST}/v1/databases/<database-id>" \
    -H "Content-Type: application/json" \
    -d '{
        "spec": {
            "nodes": [
                {
                    "name": "<node-name>",
                    "host_ids": ["<existing-host-id>", "'"${LOST_HOST_ID}"'"]
                }
            ]
        }
    }'
```

**Check database state after the update:**

```bash
curl -sS "${SERVER_HOST}/v1/databases/<database-id>" | jq '.'
```

**Note:** Control Plane automatically uses **Zero Downtime Add Node (ZODAN)** when you update the database spec to include the recovered host. This means:
- ✅ **No downtime** - Database remains available during node addition
- ✅ **Automatic** - Control Plane handles all the complexity
- ✅ **Data synchronization** - Automatically copies all data and structure from source node
- ✅ **Replication setup** - Automatically configures Spock subscriptions

By default, Control Plane uses the first node (n1) as the source. You can specify `source_node` to use a different source:

```bash
curl -sS -X PUT "${SERVER_HOST}/v1/databases/<database-id>" \
    -H "Content-Type: application/json" \
    -d '{
        "spec": {
            "nodes": [
                {
                    "name": "<node-name>",
                    "host_ids": ["<existing-host-id>", "'"${LOST_HOST_ID}"'"],
                    "source_node": "<source-node-name>"
                }
            ]
        }
    }'
```

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

**Solution:** Remove the host record first (Step 1 for server-mode, or use API for client-mode) and wait for the task to complete before rejoining.

### Issue: Host joins but shows as unreachable

**Cause:** Network connectivity issue or service not fully started.

**Solution:**
- Check service logs: `docker service logs control-plane_<host-id>`
- Verify network connectivity between hosts
- Wait a bit longer and check again

### Issue: Database instances don't restore

**Cause:** Database spec doesn't include the recovered host.

**Solution:** Update the database spec (Step 8) to include the recovered host in the node's `host_ids` array. Control Plane will automatically use zero downtime add node.

## Summary

Partial quorum loss recovery is straightforward when quorum remains intact:

1. **Remove** the lost host from Control Plane
2. **Clean** old data on the lost host
3. **Rejoin** the host to the cluster
4. **Restore** database instances if needed (automatic with ZODAN)

The key advantage is that the Control Plane API remains accessible, allowing you to manage the recovery through API calls rather than manual etcd operations.
