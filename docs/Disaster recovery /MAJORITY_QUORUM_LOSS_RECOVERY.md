# Majority Quorum Loss Recovery Guide

## Overview

Recovery guide for when **quorum is lost** but **at least one server-mode host is still online** (>50% but not 100% of server-mode hosts offline). In this scenario:
- ❌ Control Plane API may be accessible on remaining hosts
- ❌ etcd cannot accept writes (quorum lost)
- ❌ Database operations cannot proceed

## When to Use This Guide

**Use this guide when:**
- ✅ **At least one server-mode host is still online**
- ✅ **Control Plane API is accessible** on at least one host
- ❌ **Quorum is lost** (more than floor(N/2) server-mode hosts are offline)

**Example:** 3 server-mode hosts total, 2 lost, 1 still online (quorum = 2, only 1 online = quorum lost)

**How to verify:**
```bash
# Try to access Control Plane API on server-mode hosts
curl -sS "http://<server-host-ip>:<api-port>/v1/cluster"
```

If API is accessible on at least one host, use this guide.

## Prerequisites

### Calculate Quorum

**Formula:** `Quorum = floor(N/2) + 1`, where N = total server-mode hosts

| Server-Mode Hosts | Quorum | Lost When |
|-------------------|--------|-----------|
| 3 | 2 | 2+ hosts lost |
| 5 | 3 | 3+ hosts lost |
| 7 | 4 | 4+ hosts lost |

### Required Files

1. **etcd snapshot** (taken before outage)
2. **Recovery host:** One of the remaining online server-mode hosts (already has certificates/config)

### Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_IP="<recovery-host-ip>"
RECOVERY_HOST_ID="<recovery-host-id>"
SNAPSHOT_PATH="${PGEDGE_DATA_DIR}/snapshot.db"
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
API_PORT=<api-port>
```

## Recovery Steps

### Step 1: Stop All Control Plane Services

```bash
# On Swarm manager node
docker service scale control-plane_<host-id-1>=0
docker service scale control-plane_<host-id-2>=0
docker service scale control-plane_<host-id-3>=0
# ... repeat for all hosts

# Verify stopped
docker service ls --filter name=control-plane
```

### Step 2: Backup Existing etcd Data

```bash
# On recovery host
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
fi
```

### Step 3: Restore etcd Data

**When to use each approach:**
- **Restore from existing data directory (typical):** Use this when you're recovering from losing other machines in the cluster and the recovery host still has its etcd data directory intact.
- **Restore from snapshot:** Use this when you have a previously-created snapshot file, or when you want to restore to a specific point in time.

#### Install etcdutl

```bash
# On recovery host
# Install etcdutl if not present
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
curl -L https://github.com/etcd-io/etcd/releases/download/v3.6.5/etcd-v3.6.5-linux-${ARCH}.tar.gz | tar --strip-components 1 -xz -C /tmp etcd-v3.6.5-linux-${ARCH}/etcdutl
sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl
```

#### Option A: Restore from Existing Data Directory (Typical)

**Use this when:** The recovery host still has its etcd data directory intact from before the outage. This is the typical scenario when recovering from losing other machines in the cluster.

**Note:** Even though you're using the existing data directory, you still need to use `etcdutl snapshot restore` to reset the cluster membership to a single node. You'll need a snapshot file - either:
- Use a pre-existing snapshot file from your backups (recommended - use Option B)
- Create a snapshot from the existing directory if etcd is still accessible (see below)

**If etcd is still accessible** (Control Plane service is running, even if quorum is lost), you can create a snapshot from the existing directory:

```bash
# On recovery host
# Extract credentials from config (manually parse JSON or use available tools)
# ETCD_USER="<etcd-username-from-generated.config.json>"
# ETCD_PASS="<etcd-password-from-generated.config.json>"

# Create snapshot from existing etcd directory
# Note: This requires etcd to be accessible. If etcd is not running, use Option B with a pre-existing snapshot.
ETCDCTL_API=3 etcdctl snapshot save "${PGEDGE_DATA_DIR}/snapshot.db" \
    --endpoints "https://localhost:${ETCD_CLIENT_PORT}" \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}"

# Now restore from the snapshot to reset cluster membership
etcdutl snapshot restore "${PGEDGE_DATA_DIR}/snapshot.db" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --bump-revision 1000000000 \
    --mark-compacted \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

**If etcd is not accessible** (service stopped or quorum lost), use Option B with a pre-existing snapshot file from your backups.

#### Option B: Restore from Snapshot File

If you have a previously-created snapshot file:

```bash
# On recovery host
# Restore snapshot and reset cluster membership to single node
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --bump-revision 1000000000 \
    --mark-compacted \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

**Note:** The `--bump-revision` and `--mark-compacted` flags are recommended for production to prevent revision issues with clients using watches.

### Step 4: Start Control Plane

```bash
# On Swarm manager node
docker service scale control-plane_${RECOVERY_HOST_ID}=1
sleep 10
```

**Note:** Control Plane automatically detects the restored snapshot and handles `--force-new-cluster` internally. You don't need to run etcd manually.

### Step 5: Verify Recovery Host

```bash
# Check cluster
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster"
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
```

**Status:** You now have **1 server-mode host online**. Quorum is **NOT YET RESTORED**. Continue to restore quorum.

### Step 6: Remove Lost Host Records

Remove stale host records **one at a time**, waiting for each task to complete:

```bash
# For each lost host
LOST_HOST_ID="<lost-host-id>"

RESP=$(curl -sS -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/${LOST_HOST_ID}?force=true")
echo "${RESP}"
# Extract task_id from the response (look for "task_id" or "id" field in the JSON)
# TASK_ID="<task-id-from-response>"

# Wait for completion
# Check task status by calling the tasks endpoint and looking for "status": "completed" or "status": "failed"
while true; do
    STATUS_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/tasks/${TASK_ID}")
    echo "${STATUS_RESP}"
    # Look for "status": "completed" or "status": "failed" in the response
    # If found, break
    sleep 5
done
```

**Order:** Remove server-mode hosts first, then client-mode hosts.

### Step 7: Rejoin Server-Mode Hosts Until Quorum Restored

Rejoin server-mode hosts **one at a time** until quorum threshold is reached.

For each lost server-mode host:

#### 7a. Stop Service
```bash
# On Swarm manager node
LOST_SERVER_HOST_ID="<lost-server-host-id>"
docker service scale control-plane_${LOST_SERVER_HOST_ID}=0
```

#### 7b. Clear State
```bash
# On lost host node
sudo rm -rf ${PGEDGE_DATA_DIR}/etcd
sudo rm -rf ${PGEDGE_DATA_DIR}/certificates
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### 7c. Start Service
```bash
# On Swarm manager node
docker service scale control-plane_${LOST_SERVER_HOST_ID}=1
sleep 10
```

#### 7d. Get Join Token
```bash
JOIN_TOKEN_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token")
echo "${JOIN_TOKEN_RESP}"
# Extract token from the response (look for "token" field in the JSON)
# JOIN_TOKEN="<token-from-response>"
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### 7e. Join Host
```bash
LOST_SERVER_HOST_IP="<lost-server-host-ip>"
curl -sS -X POST "http://${LOST_SERVER_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

#### 7f. Check Quorum Status
```bash
TOTAL_SERVER_HOSTS=<total-server-mode-hosts>
QUORUM_THRESHOLD=$(( (TOTAL_SERVER_HOSTS / 2) + 1 ))

# Check hosts endpoint to count server-mode hosts
HOSTS_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts")
echo "${HOSTS_RESP}"
# Count the number of hosts with "etcd_mode": "server" in the response
# If count >= QUORUM_THRESHOLD, quorum is restored
```

**Decision:**
- If count < threshold: Repeat 7a-7f for next server-mode host
- If count >= threshold: Quorum restored! Proceed to Step 8

### Step 8: Rejoin Remaining Server-Mode Hosts

After quorum is restored, rejoin any remaining server-mode hosts using Steps 7a-7e (skip 7f).

### Step 9: Rejoin Client-Mode Hosts

**Important:** Only after quorum is restored (Step 7f confirms).

For each lost client-mode host:

#### 9a. Stop Service
```bash
LOST_CLIENT_HOST_ID="<lost-client-host-id>"
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=0
```

#### 9b. Clear Credentials
```bash
# On lost host node
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### 9c. Start Service
```bash
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=1
sleep 10
```

#### 9d. Get Join Token
```bash
JOIN_TOKEN_RESP=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token")
echo "${JOIN_TOKEN_RESP}"
# Extract token from the response (look for "token" field in the JSON)
# JOIN_TOKEN="<token-from-response>"
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### 9e. Join Host
```bash
LOST_CLIENT_HOST_IP="<lost-client-host-ip>"
curl -sS -X POST "http://${LOST_CLIENT_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

### Step 10: Restart All Hosts

```bash
# Scale all to zero
docker service scale control-plane_<host-id-1>=0
docker service scale control-plane_<host-id-2>=0
# ... repeat for all hosts

# Scale all to one
docker service scale control-plane_<host-id-1>=1
docker service scale control-plane_<host-id-2>=1
# ... repeat for all hosts

sleep 30
```

### Step 11: Final Verification

```bash
# Check hosts
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"

# Check databases
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases"
```

**Expected:**
- All hosts: `status: "reachable"`
- All databases: `state: "available"`

## Recovery Order Summary

1. ✅ Restore snapshot on one of the remaining online hosts
2. ✅ Start Control Plane (auto-handles `--force-new-cluster`)
3. ✅ Remove lost host records (one at a time)
4. ✅ Rejoin server-mode hosts until quorum restored
5. ✅ Rejoin remaining server-mode hosts
6. ✅ Rejoin client-mode hosts
7. ✅ Restart all hosts
8. ✅ Verify everything

**Key Points:**
- Work **one host at a time**
- **Server-mode hosts first** (restore quorum)
- **Client-mode hosts after** (need quorum restored)
- Control Plane handles `--force-new-cluster` automatically

## Common Issues

### "duplicate host ID" error

**Cause:** Host record still exists in etcd.

**Fix:** Remove host record (Step 6) and wait for task completion before rejoining.

### etcd certificate errors

**Cause:** Certificates don't match snapshot data.

**Fix:** Use the same certificate files that were used when the snapshot was taken.

### Quorum not restored

**Cause:** Not enough server-mode hosts rejoined.

**Fix:** Verify you've rejoined enough hosts to meet quorum threshold. Count only server-mode hosts.

### Control Plane fails to start

**Cause:** Old etcd processes still running.

**Fix:** 
- Stop all Control Plane services (Step 1)
- Kill any remaining etcd processes: `sudo pkill -9 etcd`

## Best Practices

1. **Take regular snapshots** - Schedule automated etcd snapshots
2. **Test recovery** - Practice in non-production environments
3. **Document cluster** - Record host IDs, modes, and quorum threshold
4. **One host at a time** - Never parallelize recovery steps
5. **Verify each step** - Don't proceed until current step is confirmed

## Summary

**Quick Recovery Flow:**

1. Restore snapshot on remaining online host → Start Control Plane → Remove lost hosts
2. Rejoin server-mode hosts until quorum restored
3. Rejoin client-mode hosts → Restart all → Verify

**Remember:** Control Plane automatically handles `--force-new-cluster` when it detects a restored snapshot. Just restore and start.
