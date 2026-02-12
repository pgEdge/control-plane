# Control Plane Quorum Scenarios

## Purpose

This document lists quorum-loss outcomes for pgEdge Control Plane with embedded etcd.

**Focus areas:**
- Server-mode hosts and quorum
- Client-mode hosts and dependence on etcd
- Recovery actions for common failures

## Terms

- **Server-mode host.** Runs embedded etcd. Counts toward quorum.
- **Client-mode host.** Connects to etcd. Does not count toward quorum.
- **Quorum.** Majority of server-mode hosts. Quorum equals floor(N/2) plus 1.
- **N.** Count of server-mode hosts.

## Key rules

- Only server-mode hosts run embedded etcd.
- Client-mode hosts depend on etcd for cluster actions.
- Quorum depends on server-mode count only.
- Work one host at a time during recovery.

## Quorum examples

- **N=3, quorum=2.** Example: three server-mode hosts require two online.
- **N=5, quorum=3.** Example: five server-mode hosts require three online.

## Failure scenarios summary

| Scenario | Server-mode lost | Client-mode lost | Quorum | Impact | Complexity | Primary recovery |
|----------|------------------|------------------|--------|--------|------------|------------------|
| **Single client-mode host down** | 0% | 1 host | ✅ Intact | One host unreachable. Database nodes on host offline. | ⚠️ Simple | Remove host record. Rejoin client-mode host. |
| **Multiple client-mode hosts down** | 0% | Multiple hosts | ✅ Intact | Multiple hosts unreachable. Database nodes on lost hosts offline. | ⚠️ Simple | Remove each host record. Rejoin each client-mode host. |
| **All client-mode hosts down** | 0% | All hosts | ✅ Intact | All client-mode hosts unreachable. Server-mode side runs. | ⚠️ Simple | Rejoin client-mode hosts after service restore. |
| **Single server-mode host down** | <50% | 0% | ✅ Intact | One server-mode host unreachable. Database nodes on host offline. | ⚠️ Simple | Remove host record. Rejoin server-mode host. Restore database instances. |
| **Majority server-mode hosts lost** | >50% | 0% | ❌ Lost | API down. etcd leader election fails. No writes. | 🔴 Complex | Restore from snapshot on one server-mode host. Force new cluster. Rejoin server-mode hosts. |
| **Majority server-mode plus client-mode lost** | >50% | Multiple or all | ❌ Lost | Full outage across hosts. | 🔴 Complex | Restore quorum first, then rejoin client-mode hosts. |
| **All server-mode hosts lost** | 100% | 0% | ❌ Lost | No recovery host online. | 🔴 Critical | Restore snapshot on recovered or new server-mode host with matching credentials. |
| **All hosts lost** | 100% | 100% | ❌ Lost | No hosts online. | 🔴 Critical | Restore snapshot on recovered or new server-mode host with matching credentials. |

## Recovery actions

### Client-mode host recovery

Use this flow for each lost client-mode host.

**Steps:**
1. Stop the Control Plane service on the host.
2. Remove `generated.config.json` from the data directory.
3. Start the Control Plane service on the host.
4. Request a join token from a server-mode host.
5. Join the host to the cluster.
6. Repeat for each lost client-mode host.

**Shell reference:**
```bash
PGEDGE_DATA_DIR=<path-to-data-dir>
API_PORT=<api-port>
SERVER_HOST=http://<server-mode-host-ip>:${API_PORT}
CLIENT_HOST=http://<client-mode-host-ip>:${API_PORT}
```

**Commands:**

#### Step 1: Stop the Control Plane service on the host

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-client-host-id>=0
```

#### Step 2: Remove generated.config.json from the data directory

Run on the lost client-mode host:

```bash
rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### Step 3: Start the Control Plane service on the host

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-client-host-id>=1
sleep 10
```

#### Step 4: Request a join token from a server-mode host

```bash
JOIN_TOKEN=$(curl -sS "${SERVER_HOST}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="${SERVER_HOST}"
```

#### Step 5: Join the host to the cluster

```bash
curl -sS -X POST "${CLIENT_HOST}/v1/cluster/join" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

**Repeat for each lost client-mode host:** Update `CLIENT_HOST` for the next host, then repeat steps 1 to 5.

### Server-mode host recovery with quorum intact

**Purpose**

You lost one server-mode host. Quorum still holds. You want the host back in the cluster.

**Assumptions**

- You lost the host and its Control Plane data volume.
- You will remove the host record before you attempt join.
- You will restore any database instances that lived on the lost host.

**Steps:**
1. Remove the lost host from Control Plane records.
2. Stop the host service in Swarm, when the service exists.
3. Clear local state on the lost host node, when files exist.
4. Start the host service, or redeploy the stack.
5. Get a join token from a healthy server-mode host.
6. Join the lost host.
7. Verify host health and mode.
8. Restore database instances that lived on the lost host.

**Shell reference:**
```bash
PGEDGE_DATA_DIR=<path-to-data-dir>
API_PORT=<api-port>
SERVER_HOST=http://<healthy-server-mode-host-ip>:${API_PORT}
LOST_SERVER_HOST=http://<lost-server-mode-host-ip>:${API_PORT}
LOST_HOST_ID=<lost-server-host-id>
LOST_SERVICE=control-plane_<lost-server-host-id>
```

**Commands:**

#### Step 1: Remove the lost host record

Run from a node with API access:

```bash
RESP=$(curl -sS -X DELETE "${SERVER_HOST}/v1/hosts/${LOST_HOST_ID}?force=true")
TASK_ID=$(echo "${RESP}" | jq -r '.task.task_id // .task.id // .id // empty')
echo "${RESP}" | jq
```

#### Step 2: Stop the host service

Skip this step if the service name does not exist.

Run on a Swarm manager node:

```bash
docker service scale "${LOST_SERVICE}=0"
```

#### Step 3: Clear server-mode state on the lost host node

Run on the lost host node. These paths exist only when some data survived.

```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

#### Step 4A: Start the host service

Use this when the service already exists.

Run on a Swarm manager node:

```bash
docker service scale "${LOST_SERVICE}=1"
sleep 10
```

#### Step 4B: Redeploy the stack

Use this when Swarm no longer has the service definition.

Run on a Swarm manager node:

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
sleep 10
```

#### Step 5: Request a join token

Run from a node with API access:

```bash
JOIN_TOKEN=$(curl -sS "${SERVER_HOST}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="${SERVER_HOST}"
```

#### Step 6: Join the lost host

Run from a node with API access:

```bash
curl -sS -X POST "${LOST_SERVER_HOST}/v1/cluster/join" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

#### Step 7: Verify host health

Run from a node with API access:

```bash
curl -sS "${SERVER_HOST}/v1/hosts" | jq \
  'if type=="array" then .[] else to_entries[].value end
   | select(.id=="'"${LOST_HOST_ID}"'")
   | {id, status, etcd_mode}'
```

**Expected fields:**
- `status` equals `reachable`
- `etcd_mode` equals `server`

#### Step 8: Restore database instances from the lost host

List databases and node placement:

```bash
curl -sS "${SERVER_HOST}/v1/databases" | jq '.[] | {id, nodes: .spec.nodes}'
```

Fetch one database spec before you change it:

```bash
curl -sS "${SERVER_HOST}/v1/databases/<database-id>" | jq '.spec'
```

Update the database spec to place a node back on the recovered host:

Use your real node layout. Keep host_ids consistent with your cluster design.

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

Check database state after the update:

```bash
curl -sS "${SERVER_HOST}/v1/databases/<database-id>" | jq
```

### Quorum loss recovery

Use this flow after majority server-mode loss.

#### Prerequisites

**Calculate quorum requirement:**
- Quorum = floor(N/2) + 1, where N = total number of server-mode hosts
- Example: 5 server-mode hosts → quorum = floor(5/2) + 1 = 3 hosts
- Example: 7 server-mode hosts → quorum = floor(7/2) + 1 = 4 hosts

**After recovery host is up:**
- You have 1 server-mode host online
- Quorum is NOT YET RESTORED (need at least the quorum threshold)
- You must rejoin additional server-mode hosts until quorum threshold is reached

#### Service name format

`control-plane_<host-id>`

#### Shell reference

```bash
PGEDGE_DATA_DIR=<path-to-data-dir>
RECOVERY_HOST_IP=<recovery-host-ip>
RECOVERY_HOST_ID=<recovery-host-id>
SNAPSHOT_PATH=<path-to-etcd-snapshot.db>
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
API_PORT=<api-port>
CONTROL_PLANE_USER=<control-plane-user>
CONTROL_PLANE_GROUP=<control-plane-group>
```

#### Steps

1. Stop Control Plane on all hosts.
2. Select one server-mode host for recovery.
3. Move aside the etcd data directory on the recovery host.
4. Restore snapshot into a fresh etcd data directory.
5. Run etcd with `--force-new-cluster` on the recovery host, then stop etcd.
6. Fix ownership of the etcd data directory.
7. Start Control Plane on the recovery host.
8. Verify the recovery host health.
9. Remove lost host records one at a time, wait for each task.
10. Rejoin server-mode hosts one at a time until quorum returns.
11. Rejoin remaining server-mode hosts for redundancy.
12. Rejoin client-mode hosts after quorum returns.
13. Restart all hosts.
14. Final verification.

#### Commands

##### Step 1: Stop Control Plane on all hosts

Run on a Swarm manager node:

```bash
docker service scale control-plane_host-<host-number>=0
# Repeat for each host
```

##### Step 2: Confirm services stopped

Run on a Swarm manager node:

```bash
docker service ls --filter name=control-plane
docker service ps control-plane_<host-id>
```

##### Step 3: Move aside etcd data on the recovery host

Run on the recovery host:

```bash
mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
```

##### Step 4: Restore snapshot on the recovery host

Run on the recovery host:

```bash
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
  --name "${RECOVERY_HOST_ID}" \
  --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
  --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
  --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

##### Step 5: Fix ownership after restore

Run on the recovery host:

```bash
chown -R "${CONTROL_PLANE_USER}:${CONTROL_PLANE_GROUP}" "${PGEDGE_DATA_DIR}/etcd"
```

##### Step 6: Run etcd with --force-new-cluster

Run on the recovery host:

```bash
pkill -9 etcd 2>/dev/null || true

etcd \
  --name "${RECOVERY_HOST_ID}" \
  --data-dir "${PGEDGE_DATA_DIR}/etcd" \
  --force-new-cluster \
  --listen-client-urls "https://0.0.0.0:${ETCD_CLIENT_PORT}" \
  --advertise-client-urls "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
  --listen-peer-urls "https://0.0.0.0:${ETCD_PEER_PORT}" \
  --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
  --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
  --initial-cluster-state "new" \
  --client-cert-auth \
  --trusted-ca-file "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
  --cert-file "${PGEDGE_DATA_DIR}/certificates/etcd-server.crt" \
  --key-file "${PGEDGE_DATA_DIR}/certificates/etcd-server.key" \
  --peer-client-cert-auth \
  --peer-trusted-ca-file "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
  --peer-cert-file "${PGEDGE_DATA_DIR}/certificates/etcd-server.crt" \
  --peer-key-file "${PGEDGE_DATA_DIR}/certificates/etcd-server.key" \
  >/tmp/etcd-force.log 2>&1 &

ETCD_PID=$!
sleep 30
kill "${ETCD_PID}" 2>/dev/null || true
```

##### Step 7: Fix ownership after membership rewrite

Run on the recovery host:

```bash
chown -R "${CONTROL_PLANE_USER}:${CONTROL_PLANE_GROUP}" "${PGEDGE_DATA_DIR}/etcd"
```

##### Step 8: Start Control Plane on the recovery host

Run on a Swarm manager node:

```bash
docker service scale control-plane_<recovery-host-id>=1
```

##### Step 9: Verify recovery host health

Run from a node with API access:

```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster" | jq
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq
```

**Important:** At this point, you have 1 server-mode host online. Quorum is NOT YET RESTORED. You must rejoin additional server-mode hosts to reach the quorum threshold.

##### Step 10: Remove lost host records, one at a time

Run from a node with API access. **Wait for each task to complete before removing the next host:**

```bash
# For each lost host, repeat this process:
RESP=$(curl -sS -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<lost-host-id>?force=true")
TASK_ID=$(echo "${RESP}" | jq -r '.task.task_id // .task.id // .id // empty')
echo "${RESP}" | jq

# Wait for task to complete
if [ -n "${TASK_ID}" ] && [ "${TASK_ID}" != "null" ]; then
  while true; do
    TASK_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/tasks/${TASK_ID}")
    STATUS=$(echo "${TASK_RESP}" | jq -r '.task.status // .status // empty')
    if [ "${STATUS}" = "completed" ] || [ "${STATUS}" = "failed" ]; then
      break
    fi
    sleep 5
  done
fi
```

##### Step 11: Rejoin server-mode hosts until quorum is restored

**Critical:** Rejoin server-mode hosts one at a time. After each rejoin, verify quorum status. Continue until you have reached the quorum threshold.

**For each lost server-mode host, repeat Steps 11a-11f:**

###### Step 11a: Stop the lost server-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-server-host-id>=0
```

###### Step 11b: Clear server-mode host state

Run on the lost server-mode host node:

```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

###### Step 11c: Start the lost server-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-server-host-id>=1
sleep 10
```

###### Step 11d: Get join token from the recovery host

Run from a node with API access:

```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

###### Step 11e: Join the lost server-mode host

Run from a node with API access:

```bash
curl -sS -X POST "http://<lost-server-host-ip>:${API_PORT}/v1/cluster/join" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

###### Step 11f: Verify quorum status

Run on the recovery host:

```bash
ETCD_USER=$(jq -r ".etcd_username" "${PGEDGE_DATA_DIR}/generated.config.json")
ETCD_PASS=$(jq -r ".etcd_password" "${PGEDGE_DATA_DIR}/generated.config.json")

ETCDCTL_API=3 etcdctl endpoint status \
  --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
  --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
  --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
  --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
  --user "${ETCD_USER}" \
  --password "${ETCD_PASS}" \
  -w table
```

**Check the output:**
- Count the number of server-mode hosts shown in the status table
- Compare to your quorum threshold (floor(N/2) + 1)
- **If count < quorum threshold:** Continue to Step 11a for the next lost server-mode host
- **If count >= quorum threshold:** Quorum is RESTORED! Proceed to Step 12

**Example verification:**
```bash
# Count server-mode hosts
SERVER_COUNT=$(ETCDCTL_API=3 etcdctl endpoint status \
  --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
  --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
  --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
  --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
  --user "${ETCD_USER}" \
  --password "${ETCD_PASS}" \
  -w json | jq 'length')

QUORUM_THRESHOLD=<calculate-based-on-total-server-hosts>
echo "Server-mode hosts online: ${SERVER_COUNT}"
echo "Quorum threshold: ${QUORUM_THRESHOLD}"

if [ "${SERVER_COUNT}" -ge "${QUORUM_THRESHOLD}" ]; then
  echo "✅ Quorum RESTORED!"
else
  echo "⚠️  Quorum NOT YET RESTORED. Rejoin more server-mode hosts."
fi
```

##### Step 12: Rejoin remaining server-mode hosts (after quorum is restored)

**After quorum is restored, continue rejoining any remaining lost server-mode hosts for redundancy.**

**For each remaining lost server-mode host, repeat Steps 12a-12e:**

###### Step 12a: Stop the lost server-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-server-host-id>=0
```

###### Step 12b: Clear server-mode host state

Run on the lost server-mode host node:

```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

###### Step 12c: Start the lost server-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-server-host-id>=1
sleep 10
```

###### Step 12d: Get join token from the recovery host

Run from a node with API access:

```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

###### Step 12e: Join the lost server-mode host

Run from a node with API access:

```bash
curl -sS -X POST "http://<lost-server-host-ip>:${API_PORT}/v1/cluster/join" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

**Note:** After quorum is restored, you can rejoin remaining server-mode hosts in parallel or sequentially. Quorum is already maintained, so order doesn't matter.

##### Step 13: Rejoin client-mode hosts (after quorum returns)

**Only proceed after quorum is restored (Step 11f confirms quorum).**

**For each lost client-mode host, repeat Steps 13a-13e:**

###### Step 13a: Stop the lost client-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-client-host-id>=0
```

###### Step 13b: Clear client-mode credentials

Run on the lost client-mode host node:

```bash
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

###### Step 13c: Start the lost client-mode host service

Run on a Swarm manager node:

```bash
docker service scale control-plane_<lost-client-host-id>=1
sleep 10
```

###### Step 11d: Get join token from the recovery host

Run from a node with API access:

```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

###### Step 13e: Join the lost client-mode host

Run from a node with API access:

```bash
curl -sS -X POST "http://<lost-client-host-ip>:${API_PORT}/v1/cluster/join" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

##### Step 14: Restart all hosts

Run on a Swarm manager node:

**Scale all services to zero:**
```bash
docker service scale control-plane_host-<host-number>=0
# Repeat for each host
```

**Scale all services to one:**
```bash
docker service scale control-plane_host-<host-number>=1
# Repeat for each host
```

**Wait for all services to start:**
```bash
sleep 30
```

##### Step 15: Final verification

Run from a node with API access:

**Verify all hosts are healthy:**
```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq
```

**Verify databases:**
```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases" | jq
```

**Verify etcd status:**
```bash
ETCD_USER=$(jq -r ".etcd_username" "${PGEDGE_DATA_DIR}/generated.config.json")
ETCD_PASS=$(jq -r ".etcd_password" "${PGEDGE_DATA_DIR}/generated.config.json")

ETCDCTL_API=3 etcdctl endpoint status \
  --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
  --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
  --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
  --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
  --user "${ETCD_USER}" \
  --password "${ETCD_PASS}" \
  -w table
```

## Total server-mode loss

**Outcome:** No server-mode host online.

**Inputs required:**
- One etcd snapshot from before the outage.
- `generated.config.json` and certificates directory from one server-mode host identity.
- One recovered server-mode host, or one new host built with the same identity.

**Recovery outline:**
1. Bring up one server-mode host with matching identity files.
2. Restore snapshot into the etcd data directory.
3. Run etcd `--force-new-cluster`, then stop etcd.
4. Start Control Plane on the recovery host.
5. Remove stale host records one at a time.
6. Rejoin remaining server-mode hosts, then rejoin client-mode hosts.

Follow the [Quorum Loss Recovery](#quorum-loss-recovery) procedure above.

## Expected behavior examples

### Six-host split example

- **Server-mode hosts:** host-1, host-2, host-3.
- **Client-mode hosts:** host-4, host-5, host-6.
- **Quorum target:** 2 of 3 server-mode hosts.

**Outcomes:**
- **Three of three server-mode online.** Leader election works. Writes work.
- **Two of three server-mode online.** Quorum holds. Cluster actions work.
- **One of three server-mode online.** Quorum lost. Leader election fails. Writes fail.
- **Client-mode host loss.** Quorum holds. Work loss limited to affected host.

## Out of scope

- **Network split:** The majority side elects the leader. The minority side rejects writes.
- **Slow network:** Leader changes repeat. API timeouts rise.
- **Wrong recovery advertise address:** Recovery host advertises `127.0.0.1`. Other hosts fail to reach etcd on 2379 and 2380.
- **Credential mismatch:** `generated.config.json` username or password mismatch versus snapshot data. etcd auth fails.
- **Stale host records after restore:** Host records remain in etcd. Remove host records before rejoin.

## Operator checklist

- Count server-mode hosts.
- Compute quorum, floor(N/2) plus 1.
- Confirm quorum status.
- Pick recovery flow from the scenario table.
- Run one host action at a time.
- Log each change with time and host ID.
