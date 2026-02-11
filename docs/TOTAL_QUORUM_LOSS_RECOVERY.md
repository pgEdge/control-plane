# Total Quorum Loss Recovery Guide

## Overview

This guide covers recovery from **total quorum loss** scenarios where a majority of server-mode hosts are lost, causing the etcd cluster to lose quorum. In these scenarios, the Control Plane API is down, etcd cannot elect a leader, and writes are not possible.

## When to Use This Guide

Use this guide when:
- ❌ **Quorum is LOST** (more than 50% of server-mode hosts are offline)
- ❌ **Control Plane API is down** (cannot make API calls)
- ❌ **etcd cannot accept writes** (no leader election possible)

**Examples:**
- Majority server-mode hosts lost (>50%)
- All server-mode hosts lost (100%)
- Majority server-mode plus client-mode hosts lost

## Understanding Quorum Loss

### Quorum Calculation

- **Quorum** = floor(N/2) + 1, where N = total number of server-mode hosts
- **Quorum loss** occurs when more than 50% of server-mode hosts are permanently lost

### Examples

| Total Server-Mode Hosts | Quorum Requirement | Quorum Lost When |
|------------------------|-------------------|------------------|
| 3 | 2 | 2 or more hosts lost |
| 5 | 3 | 3 or more hosts lost |
| 7 | 4 | 4 or more hosts lost |

### Impact of Quorum Loss

When quorum is lost:
- ❌ etcd cannot elect a leader
- ❌ etcd cannot accept writes
- ❌ Control Plane API cannot process requests
- ❌ Database operations cannot proceed
- ✅ etcd can still serve reads (if any members are online)

## Prerequisites

Before starting recovery, ensure you have:

1. **An etcd snapshot** taken before the outage
   - Taken with `etcdctl snapshot save`
   - Or copied from `member/snap/db` file (may lose WAL data)

2. **Certificate files** from a server-mode host:
   - `certificates/ca.crt`
   - `certificates/etcd-server.crt`
   - `certificates/etcd-server.key`
   - `certificates/etcd-user.crt`
   - `certificates/etcd-user.key`

3. **Configuration file:**
   - `generated.config.json` (contains etcd username/password)

4. **A recovery host:**
   - One server-mode host that can be brought online
   - Must have matching certificate files and configuration
   - Can be the same host that provided the snapshot, or a new host with matching identity

## Recovery Process Overview

The recovery process follows these phases:

1. **Stop all services** - Prevent conflicts during recovery
2. **Restore snapshot** - Recover etcd data from snapshot
3. **Rewrite cluster membership** - Use `--force-new-cluster` to create new cluster
4. **Start recovery host** - Bring up one server-mode host
5. **Remove lost hosts** - Clean up stale host records
6. **Rejoin server-mode hosts** - Restore quorum one host at a time
7. **Rejoin client-mode hosts** - Add back client-mode hosts after quorum is restored
8. **Verify recovery** - Confirm cluster is fully operational

## Phase 1: Prepare for Recovery

### Step 1.1: Take Snapshot (If Not Already Done)

**Purpose:** If you still have access to a running etcd member, take a snapshot immediately.

**Command:**
```bash
# On a server-mode host that's still running
PGEDGE_DATA_DIR="/data/control-plane"
ETCD_USER=$(jq -r ".etcd_username" "${PGEDGE_DATA_DIR}/generated.config.json")
ETCD_PASS=$(jq -r ".etcd_password" "${PGEDGE_DATA_DIR}/generated.config.json")

ETCDCTL_API=3 etcdctl snapshot save "${PGEDGE_DATA_DIR}/snapshot.db" \
    --endpoints https://localhost:2379 \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}"
```

**Why this matters:**
- Snapshots contain all etcd data up to the point they were taken
- Without a snapshot, you cannot recover the cluster state
- Always prefer `etcdctl snapshot save` over copying `member/snap/db` (may lose WAL data)

### Step 1.2: Verify Snapshot Status

**Purpose:** Confirm the snapshot is valid and contains the expected data.

**Command:**
```bash
etcdutl snapshot status snapshot.db -w table
```

**Expected output:**
```
+---------+----------+------------+------------+
|  HASH   | REVISION | TOTAL KEYS | TOTAL SIZE |
+---------+----------+------------+------------+
| 7ef846e |   485261 |      11642 |      94 MB |
+---------+----------+------------+------------+
```

**What to check:**
- **HASH**: Integrity hash (should match if snapshot was taken with `etcdctl`)
- **REVISION**: The etcd revision at snapshot time
- **TOTAL KEYS**: Number of keys in the snapshot
- **TOTAL SIZE**: Size of the snapshot file

### Step 1.3: Stop All Control Plane Services

**Purpose:** Prevent conflicts and ensure clean recovery state.

**Command:**
```bash
# On a Swarm manager node (if accessible)
docker service scale control-plane_host-1=0
docker service scale control-plane_host-2=0
docker service scale control-plane_host-3=0
# ... repeat for all hosts

# Verify all services are stopped
docker service ls --filter name=control-plane
```

**Why this is critical:**
- Prevents etcd processes from interfering with recovery
- Ensures clean state for snapshot restore
- Required before running `--force-new-cluster`

## Phase 2: Restore Snapshot

### Step 2.1: Select Recovery Host

**Purpose:** Choose which server-mode host will be the recovery host.

**Criteria:**
- Must be a server-mode host
- Must have access to the snapshot file
- Must have matching certificate files
- Must have matching `generated.config.json`

**Set variables:**
```bash
PGEDGE_DATA_DIR="/data/control-plane"
RECOVERY_HOST_IP="192.168.104.1"  # Replace with recovery host IP
RECOVERY_HOST_ID="host-1"  # Replace with recovery host ID
SNAPSHOT_PATH="${PGEDGE_DATA_DIR}/snapshot.db"
ETCD_CLIENT_PORT=2379
ETCD_PEER_PORT=2380
API_PORT=3000
CONTROL_PLANE_USER=1001  # Check your Docker user/group
CONTROL_PLANE_GROUP=1001
```

### Step 2.2: Backup Existing etcd Data

**Purpose:** Preserve existing data in case recovery fails.

**Command:**
```bash
# On the recovery host
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
    echo "Backed up existing etcd data"
fi
```

**Why backup:**
- Allows rollback if recovery fails
- Preserves any data that might be useful for troubleshooting
- Timestamp in backup name helps identify when backup was made

### Step 2.3: Restore Snapshot

**Purpose:** Restore etcd data from the snapshot into a fresh etcd data directory.

**Command:**
```bash
# On the recovery host
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

**What this does:**
- Creates a new etcd data directory from the snapshot
- Sets up single-member cluster configuration
- Prepares etcd to start as a new cluster

**Parameters explained:**
- `--name`: The etcd member name (must match host ID)
- `--initial-cluster`: Single-member cluster for recovery
- `--initial-advertise-peer-urls`: Peer URL for this member
- `--data-dir`: Directory where etcd data will be restored

**Optional: Use revision bump (recommended for production):**

```bash
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --bump-revision 1000000000 \
    --mark-compacted \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

**Why use `--bump-revision` and `--mark-compacted`:**
- **`--bump-revision`**: Ensures revisions never decrease after restore. Critical for clients using watches (like Kubernetes controllers) that cache revision numbers.
- **`--mark-compacted`**: Marks all revisions, including the bump, as compacted. Ensures watches are terminated and etcd doesn't respond to requests about revisions after the snapshot.

**Example:** If your snapshot is 1 week old and etcd processes ~1500 writes/second, you might bump by 1,000,000,000 revisions to cover the gap.

### Step 2.4: Fix Ownership

**Purpose:** Ensure the Control Plane process can access the restored etcd data.

**Command:**
```bash
# On the recovery host
chown -R "${CONTROL_PLANE_USER}:${CONTROL_PLANE_GROUP}" "${PGEDGE_DATA_DIR}/etcd"
```

**Why this matters:**
- The restore process may create files with wrong ownership
- Control Plane runs as a specific user/group (typically 1001:1001)
- Wrong ownership prevents etcd from starting

## Phase 3: Rewrite Cluster Membership

### Step 3.1: Kill Any Running etcd Processes

**Purpose:** Ensure no etcd processes are running before rewriting membership.

**Command:**
```bash
# On the recovery host
sudo pkill -9 etcd 2>/dev/null || true

# Verify no etcd processes are running
ps aux | grep etcd | grep -v grep
```

**Expected:** Should show no etcd processes.

### Step 3.2: Run etcd with --force-new-cluster

**Purpose:** Rewrite cluster membership to create a new logical cluster.

**Why this is necessary:**
- The restore process creates a new logical cluster
- Old cluster members might still be alive and try to connect
- `--force-new-cluster` ensures the recovered member only connects to the new cluster
- Rewrites membership metadata in the etcd data directory

**Command:**
```bash
# On the recovery host
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
wait "${ETCD_PID}" 2>/dev/null || true
```

**What this does:**
- Starts etcd with `--force-new-cluster` flag
- Rewrites cluster membership metadata
- Runs for 30 seconds to complete the rewrite
- Stops etcd after membership is rewritten

**Warning:** According to etcd documentation, `--force-new-cluster` will panic if other members from the previous cluster are still alive. Make sure all old etcd processes are stopped before running this.

**Check logs for errors:**
```bash
# Review etcd logs
cat /tmp/etcd-force.log
```

**Expected:** Should show etcd starting and rewriting membership without errors.

### Step 3.3: Fix Ownership Again

**Purpose:** Ensure ownership is correct after etcd rewrites membership.

**Command:**
```bash
# On the recovery host
chown -R "${CONTROL_PLANE_USER}:${CONTROL_PLANE_GROUP}" "${PGEDGE_DATA_DIR}/etcd"
```

## Phase 4: Start Recovery Host

### Step 4.1: Start Control Plane Service

**Purpose:** Start the Control Plane service on the recovery host.

**Command:**
```bash
# On a Swarm manager node
docker service scale control-plane_${RECOVERY_HOST_ID}=1

# Wait for service to start
sleep 10

# Verify service is running
docker service ps control-plane_${RECOVERY_HOST_ID}
```

**Expected:** Service should show `Replicas: 1/1` with status `Running`.

### Step 4.2: Verify API is Accessible

**Purpose:** Confirm the Control Plane API is responding.

**Command:**
```bash
# Wait for API to be ready
for i in {1..30}; do
    if curl -sS --max-time 5 "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/version" >/dev/null 2>&1; then
        echo "API is ready!"
        break
    fi
    echo "Waiting for API... (attempt $i)"
    sleep 2
done

# Check version
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/version" | jq '.'
```

**Expected:** Should return version information.

### Step 4.3: Verify etcd Health

**Purpose:** Confirm etcd is running and healthy.

**Command:**
```bash
# On the recovery host
PGEDGE_DATA_DIR="/data/control-plane"
ETCD_USER=$(jq -r ".etcd_username" "${PGEDGE_DATA_DIR}/generated.config.json")
ETCD_PASS=$(jq -r ".etcd_password" "${PGEDGE_DATA_DIR}/generated.config.json")

ETCDCTL_API=3 etcdctl endpoint health \
    --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}"
```

**Expected output:**
```
https://192.168.104.1:2379 is healthy: successfully committed proposal: took = 1.234567ms
```

### Step 4.4: Check etcd Member Status

**Purpose:** Verify etcd cluster shows only the recovery host.

**Command:**
```bash
ETCDCTL_API=3 etcdctl endpoint status \
    --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}" \
    -w table
```

**Expected:** Should show only the recovery host (1 member).

**Important:** At this point, you have **1 server-mode host online**. Quorum is **NOT YET RESTORED**. You must rejoin additional server-mode hosts to reach the quorum threshold.

## Phase 5: Remove Lost Host Records

### Step 5.1: Calculate Quorum Threshold

**Purpose:** Determine how many server-mode hosts you need to restore quorum.

**Command:**
```bash
# Example: 3 server-mode hosts total
TOTAL_SERVER_HOSTS=3
QUORUM_THRESHOLD=$(( (TOTAL_SERVER_HOSTS / 2) + 1 ))
echo "Total server-mode hosts: ${TOTAL_SERVER_HOSTS}"
echo "Quorum threshold: ${QUORUM_THRESHOLD}"
```

**Expected:** For 3 server-mode hosts, quorum threshold = 2.

### Step 5.2: Remove Lost Hosts One at a Time

**Purpose:** Remove stale host records from etcd so hosts can rejoin cleanly.

**Why this is necessary:**
- After restoring from snapshot, old host records remain in etcd
- These must be removed before rejoining hosts
- Otherwise, you'll get "duplicate host ID" errors

**Command (for each lost host):**
```bash
# Remove a lost host
LOST_HOST_ID="host-3"  # Replace with lost host ID

RESP=$(curl -sS -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/${LOST_HOST_ID}?force=true")
TASK_ID=$(echo "${RESP}" | jq -r '.task.task_id // .task.id // .id // empty')
echo "${RESP}" | jq '.'

# Wait for task to complete
if [ -n "${TASK_ID}" ] && [ "${TASK_ID}" != "null" ]; then
    while true; do
        TASK_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/tasks/${TASK_ID}")
        STATUS=$(echo "${TASK_RESP}" | jq -r '.task.status // .status // empty')
        echo "Task ${TASK_ID} status: ${STATUS}"
        if [ "${STATUS}" = "completed" ] || [ "${STATUS}" = "failed" ]; then
            break
        fi
        sleep 5
    done
fi
```

**Critical:** Remove hosts **one at a time** and **wait for each task to complete** before removing the next host.

**Order:**
1. Remove server-mode hosts first
2. Then remove client-mode hosts

## Phase 6: Restore Quorum

### Step 6.1: Rejoin Server-Mode Hosts Until Quorum is Restored

**Purpose:** Add server-mode hosts back to restore etcd quorum.

**Critical:** Rejoin server-mode hosts **one at a time**. After each rejoin, verify quorum status. Continue until you have reached the quorum threshold.

**For each lost server-mode host, repeat these steps:**

#### Step 6.1a: Stop the Lost Server-Mode Host Service

**Purpose:** Ensure service is stopped before cleaning data.

**Command:**
```bash
# On a Swarm manager node
LOST_SERVER_HOST_ID="host-3"  # Replace with lost host ID
docker service scale control-plane_${LOST_SERVER_HOST_ID}=0
```

#### Step 6.1b: Clear Server-Mode Host State

**Purpose:** Remove all old etcd data and credentials.

**Command:**
```bash
# On the lost server-mode host node
PGEDGE_DATA_DIR="/data/control-plane"
sudo rm -rf ${PGEDGE_DATA_DIR}/etcd
sudo rm -rf ${PGEDGE_DATA_DIR}/certificates
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

**Why remove all three:**
- `etcd/` - Contains old cluster membership
- `certificates/` - Tied to old cluster identity
- `generated.config.json` - Contains old credentials

#### Step 6.1c: Start the Lost Server-Mode Host Service

**Purpose:** Start service so host can accept join requests.

**Command:**
```bash
# On a Swarm manager node
docker service scale control-plane_${LOST_SERVER_HOST_ID}=1
sleep 10
```

#### Step 6.1d: Get Join Token from Recovery Host

**Purpose:** Obtain token for joining the cluster.

**Command:**
```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### Step 6.1e: Join the Lost Server-Mode Host

**Purpose:** Add the host back to the etcd cluster.

**Command:**
```bash
# Get the lost host's IP
LOST_SERVER_HOST_IP="<lost-server-host-ip>"  # Replace with actual IP

curl -sS -X POST "http://${LOST_SERVER_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}" | jq '.'
```

**What happens:**
- Control Plane validates the join token
- Generates new certificates for the host
- Adds host to etcd cluster as a new member
- Starts embedded etcd on the host
- Host becomes part of the etcd quorum

#### Step 6.1f: Verify Quorum Status

**Purpose:** Check if quorum has been restored.

**Command:**
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

echo "Server-mode hosts online: ${SERVER_COUNT}"
echo "Quorum threshold: ${QUORUM_THRESHOLD}"

if [ "${SERVER_COUNT}" -ge "${QUORUM_THRESHOLD}" ]; then
    echo "✅ Quorum RESTORED!"
else
    echo "⚠️  Quorum NOT YET RESTORED. Rejoin more server-mode hosts."
fi
```

**Decision:**
- **If count < quorum threshold:** Continue to Step 6.1a for the next lost server-mode host
- **If count >= quorum threshold:** Quorum is RESTORED! Proceed to Step 6.2

### Step 6.2: Rejoin Remaining Server-Mode Hosts

**Purpose:** Add remaining server-mode hosts for redundancy after quorum is restored.

**After quorum is restored**, continue rejoining any remaining lost server-mode hosts. Use the same steps as 6.1a-6.1e (you can skip 6.1f since quorum is already restored).

**Note:** After quorum is restored, you can rejoin remaining server-mode hosts in parallel or sequentially. Quorum is already maintained, so order doesn't matter.

## Phase 7: Rejoin Client-Mode Hosts

### Step 7.1: Rejoin Client-Mode Hosts

**Purpose:** Add client-mode hosts back to the cluster.

**Important:** Only proceed after quorum is restored (Step 6.1f confirms quorum).

**For each lost client-mode host, repeat these steps:**

#### Step 7.1a: Stop the Lost Client-Mode Host Service

**Command:**
```bash
# On a Swarm manager node
LOST_CLIENT_HOST_ID="host-2"  # Replace with lost host ID
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=0
```

#### Step 7.1b: Clear Client-Mode Credentials

**Purpose:** Remove old credentials so host can rejoin.

**Command:**
```bash
# On the lost client-mode host node
PGEDGE_DATA_DIR="/data/control-plane"
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

**Note:** Client-mode hosts don't have etcd data or certificates to remove - they only need to clear their credentials.

#### Step 7.1c: Start the Lost Client-Mode Host Service

**Command:**
```bash
# On a Swarm manager node
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=1
sleep 10
```

#### Step 7.1d: Get Join Token

**Command:**
```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### Step 7.1e: Join the Lost Client-Mode Host

**Command:**
```bash
# Get the lost host's IP
LOST_CLIENT_HOST_IP="<lost-client-host-ip>"  # Replace with actual IP

curl -sS -X POST "http://${LOST_CLIENT_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}" | jq '.'
```

## Phase 8: Final Verification

### Step 8.1: Restart All Hosts

**Purpose:** Ensure all hosts are running with the latest cluster state.

**Command:**
```bash
# On a Swarm manager node
# Scale all services to zero
docker service scale control-plane_host-1=0
docker service scale control-plane_host-2=0
# ... repeat for all hosts

# Scale all services to one
docker service scale control-plane_host-1=1
docker service scale control-plane_host-2=1
# ... repeat for all hosts

# Wait for all services to start
sleep 30
```

### Step 8.2: Verify All Hosts Are Healthy

**Command:**
```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq '.[] | {id, status, etcd_mode}'
```

**Expected:** All hosts should show `status: "reachable"` and correct `etcd_mode`.

### Step 8.3: Verify Databases

**Command:**
```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases" | jq '.[] | {id, state}'
```

**Expected:** All databases should show `state: "available"`.

### Step 8.4: Verify etcd Cluster Status

**Command:**
```bash
ETCDCTL_API=3 etcdctl endpoint status \
    --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}" \
    -w table
```

**Expected:** Should show all server-mode hosts with their status.

## Recovery Order Summary

**Critical recovery order:**

1. ✅ **Recovery host** - Restore snapshot and start
2. ✅ **Remove lost host records** - One at a time, wait for each
3. ✅ **Rejoin server-mode hosts** - Until quorum threshold is reached
4. ✅ **Rejoin remaining server-mode hosts** - For redundancy
5. ✅ **Rejoin client-mode hosts** - After quorum is restored
6. ✅ **Restart all hosts** - Final verification

**Why this order matters:**

- **Server-mode hosts first**: They restore quorum, enabling the cluster to accept writes
- **Client-mode hosts after**: They don't affect quorum, but need quorum to be restored before they can rejoin
- **One at a time**: Prevents race conditions and makes troubleshooting easier

## Common Issues and Solutions

### Issue: "duplicate host ID" error when rejoining

**Cause:** Host record still exists in etcd from before the snapshot.

**Solution:** Remove the host record via API (Phase 5) and wait for the task to complete before retrying.

### Issue: etcd fails to start with certificate errors

**Cause:** Certificate files don't match the snapshot data.

**Solution:** Ensure you're using the same certificate files that were used when the snapshot was taken. The certificates must match the etcd authentication in the snapshot.

### Issue: Quorum not restored after rejoining hosts

**Cause:** Not enough server-mode hosts have rejoined.

**Solution:** 
- Verify you've rejoined enough server-mode hosts to meet the quorum threshold
- Check etcd member status to confirm all server-mode hosts are online
- Ensure you're counting only server-mode hosts, not client-mode hosts

### Issue: `--force-new-cluster` panics

**Cause:** Other etcd members from the previous cluster are still running.

**Solution:** 
- Stop all Control Plane services before running `--force-new-cluster`
- Kill any remaining etcd processes manually
- Verify no etcd processes are running before starting recovery

### Issue: Revision going backwards after restore

**Cause:** Restored snapshot is older than current state, and clients cache revision numbers.

**Solution:** Use `--bump-revision` and `--mark-compacted` flags during restore (Step 2.3).

## Best Practices

1. **Take regular snapshots**: Schedule automated etcd snapshots to minimize data loss
2. **Test recovery procedures**: Regularly test quorum loss recovery in non-production environments
3. **Document your cluster**: Keep records of:
   - Total number of server-mode hosts
   - Quorum threshold
   - Host IDs and their modes
   - Snapshot locations
4. **Monitor quorum status**: Set up alerts for quorum loss scenarios
5. **One host at a time**: Always work on one host at a time during recovery
6. **Verify after each step**: Don't proceed to the next step until the current one is verified

## Summary

Total quorum loss recovery is complex but follows a systematic process:

1. **Restore snapshot** on one server-mode host
2. **Rewrite cluster membership** with `--force-new-cluster`
3. **Start recovery host** and verify it's working
4. **Remove lost hosts** from etcd records
5. **Rejoin server-mode hosts** until quorum is restored
6. **Rejoin client-mode hosts** after quorum is restored
7. **Verify everything** is working correctly

The key is to work methodically, one host at a time, and verify each step before proceeding.
