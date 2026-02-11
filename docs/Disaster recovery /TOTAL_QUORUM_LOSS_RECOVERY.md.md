# Total Quorum Loss Recovery Guide

## Overview

Recovery guide for when **all server-mode hosts are lost** (100% of server-mode hosts offline). In this scenario:
- ❌ No server-mode hosts are online
- ❌ Control Plane API is not accessible on any host
- ❌ etcd cannot accept writes (quorum lost)
- ❌ Database operations cannot proceed

## When to Use This Guide

**Use this guide when:**
- ❌ **No server-mode hosts are online**
- ❌ **Control Plane API is not accessible** on any host
- ❌ **All server-mode hosts are lost** (100% loss)

**Example:** 3 server-mode hosts total, all 3 lost (quorum = 2, 0 online = quorum lost)

**How to verify:**
```bash
# Try to access Control Plane API on server-mode hosts
curl -sS "http://<server-host-ip>:<api-port>/v1/cluster" | jq '.'
```

If API is **not accessible** on any host, use this guide.

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
2. **Certificate files** from a server-mode host (from before outage):
   - `certificates/ca.crt`
   - `certificates/etcd-server.crt`
   - `certificates/etcd-server.key`
   - `certificates/etcd-user.crt`
   - `certificates/etcd-user.key`
3. **Configuration:** `generated.config.json` (from before outage)
4. **Recovery host:** A recovered or new server-mode host with matching certificates/config

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

### Step 2: Prepare Recovery Host

**If using a recovered host:**
- Ensure it has matching certificate files and `generated.config.json` from before the outage
- Copy certificate files to: `${PGEDGE_DATA_DIR}/certificates/`
- Copy `generated.config.json` to: `${PGEDGE_DATA_DIR}/generated.config.json`

**If using a new host:**
- Copy certificate files from a server-mode host (from before outage) to: `${PGEDGE_DATA_DIR}/certificates/`
- Copy `generated.config.json` from a server-mode host (from before outage) to: `${PGEDGE_DATA_DIR}/generated.config.json`
- Ensure the host ID matches the certificate identity

### Step 3: Backup Existing etcd Data

```bash
# On recovery host
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
fi
```

### Step 4: Restore Snapshot

```bash
# On recovery host
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

**Optional (recommended for production):** Add `--bump-revision <revision-bump-value> --mark-compacted` to prevent revision issues with clients using watches.

### Step 5: Start Control Plane

```bash
# On Swarm manager node
docker service scale control-plane_${RECOVERY_HOST_ID}=1
sleep 10
```

**Note:** Control Plane automatically detects the restored snapshot and handles `--force-new-cluster` internally. You don't need to run etcd manually.

### Step 6: Verify Recovery Host

```bash
# Check cluster
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster" | jq '.'
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq '.'
```

**Status:** You now have **1 server-mode host online**. Quorum is **NOT YET RESTORED**. Continue to restore quorum.

### Step 7: Remove Lost Host Records

Remove stale host records **one at a time**, waiting for each task to complete:

```bash
# For each lost host
LOST_HOST_ID="<lost-host-id>"

RESP=$(curl -sS -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/${LOST_HOST_ID}?force=true")
TASK_ID=$(echo "${RESP}" | jq -r '.task.task_id // .task.id // .id // empty')

# Wait for completion
while true; do
    STATUS=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/tasks/${TASK_ID}" | jq -r '.task.status // .status // empty')
    if [ "${STATUS}" = "completed" ] || [ "${STATUS}" = "failed" ]; then
        break
    fi
    sleep 5
done
```

**Order:** Remove server-mode hosts first, then client-mode hosts.

### Step 8: Rejoin Server-Mode Hosts Until Quorum Restored

Rejoin server-mode hosts **one at a time** until quorum threshold is reached.

For each lost server-mode host:

#### 8a. Stop Service
```bash
# On Swarm manager node
LOST_SERVER_HOST_ID="<lost-server-host-id>"
docker service scale control-plane_${LOST_SERVER_HOST_ID}=0
```

#### 8b. Clear State
```bash
# On lost host node
sudo rm -rf ${PGEDGE_DATA_DIR}/etcd
sudo rm -rf ${PGEDGE_DATA_DIR}/certificates
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### 8c. Start Service
```bash
# On Swarm manager node
docker service scale control-plane_${LOST_SERVER_HOST_ID}=1
sleep 10
```

#### 8d. Get Join Token
```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### 8e. Join Host
```bash
LOST_SERVER_HOST_IP="<lost-server-host-ip>"
curl -sS -X POST "http://${LOST_SERVER_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

#### 8f. Check Quorum Status
```bash
TOTAL_SERVER_HOSTS=<total-server-mode-hosts>
QUORUM_THRESHOLD=$(( (TOTAL_SERVER_HOSTS / 2) + 1 ))
ETCD_USER=$(jq -r ".etcd_username" "${PGEDGE_DATA_DIR}/generated.config.json")
ETCD_PASS=$(jq -r ".etcd_password" "${PGEDGE_DATA_DIR}/generated.config.json")
SERVER_COUNT=$(ETCDCTL_API=3 etcdctl endpoint status --endpoints "https://${RECOVERY_HOST_IP}:${ETCD_CLIENT_PORT}" --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" --user "${ETCD_USER}" --password "${ETCD_PASS}" -w json | jq 'length')
[ "${SERVER_COUNT}" -ge "${QUORUM_THRESHOLD}" ] && echo "✅ Quorum RESTORED!" || echo "⚠️  Continue rejoining server-mode hosts"
```

**Decision:**
- If count < threshold: Repeat 8a-8f for next server-mode host
- If count >= threshold: Quorum restored! Proceed to Step 9

### Step 9: Rejoin Remaining Server-Mode Hosts

After quorum is restored, rejoin any remaining server-mode hosts using Steps 8a-8e (skip 8f).

### Step 10: Rejoin Client-Mode Hosts

**Important:** Only after quorum is restored (Step 8f confirms).

For each lost client-mode host:

#### 10a. Stop Service
```bash
LOST_CLIENT_HOST_ID="<lost-client-host-id>"
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=0
```

#### 10b. Clear Credentials
```bash
# On lost host node
sudo rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

#### 10c. Start Service
```bash
docker service scale control-plane_${LOST_CLIENT_HOST_ID}=1
sleep 10
```

#### 10d. Get Join Token
```bash
JOIN_TOKEN=$(curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token" | jq -r ".token")
SERVER_URL="http://${RECOVERY_HOST_IP}:${API_PORT}"
```

#### 10e. Join Host
```bash
LOST_CLIENT_HOST_IP="<lost-client-host-ip>"
curl -sS -X POST "http://${LOST_CLIENT_HOST_IP}:${API_PORT}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${JOIN_TOKEN}\",\"server_url\":\"${SERVER_URL}\"}"
```

### Step 11: Restart All Hosts

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

### Step 12: Final Verification

```bash
# Check hosts
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq '.[] | {id, status, etcd_mode}'

# Check databases
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases" | jq '.[] | {id, state}'

# Check etcd cluster
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

**Expected:**
- All hosts: `status: "reachable"`
- All databases: `state: "available"`
- etcd shows all server-mode hosts

## Recovery Order Summary

1. ✅ Prepare recovery host with matching certificates/config
2. ✅ Restore snapshot on recovery host
3. ✅ Start Control Plane (auto-handles `--force-new-cluster`)
4. ✅ Remove lost host records (one at a time)
5. ✅ Rejoin server-mode hosts until quorum restored
6. ✅ Rejoin remaining server-mode hosts
7. ✅ Rejoin client-mode hosts
8. ✅ Restart all hosts
9. ✅ Verify everything

**Key Points:**
- Work **one host at a time**
- **Server-mode hosts first** (restore quorum)
- **Client-mode hosts after** (need quorum restored)
- Control Plane handles `--force-new-cluster` automatically
- **Critical:** Must have matching certificates/config from before outage

## Common Issues

### "duplicate host ID" error

**Cause:** Host record still exists in etcd.

**Fix:** Remove host record (Step 7) and wait for task completion before rejoining.

### etcd certificate errors

**Cause:** Certificates don't match snapshot data.

**Fix:** Ensure you're using the same certificate files that were used when the snapshot was taken. Certificates must match the etcd authentication in the snapshot.

### Quorum not restored

**Cause:** Not enough server-mode hosts rejoined.

**Fix:** Verify you've rejoined enough hosts to meet quorum threshold. Count only server-mode hosts.

### Control Plane fails to start

**Cause:** Old etcd processes still running or incorrect certificates.

**Fix:** 
- Stop all Control Plane services (Step 1)
- Kill any remaining etcd processes: `sudo pkill -9 etcd`
- Verify certificate files match the snapshot identity

## Best Practices

1. **Take regular snapshots** - Schedule automated etcd snapshots
2. **Backup certificates/config** - Store certificate files and `generated.config.json` separately from hosts
3. **Test recovery** - Practice in non-production environments
4. **Document cluster** - Record host IDs, modes, quorum threshold, and certificate locations
5. **One host at a time** - Never parallelize recovery steps
6. **Verify each step** - Don't proceed until current step is confirmed

## Summary

**Quick Recovery Flow:**

1. Prepare recovery host with matching certificates/config → Restore snapshot → Start Control Plane
2. Remove lost hosts → Rejoin server-mode hosts until quorum restored
3. Rejoin client-mode hosts → Restart all → Verify

**Remember:** 
- Control Plane automatically handles `--force-new-cluster` when it detects a restored snapshot
- You must have matching certificate files and `generated.config.json` from before the outage
- If all hosts are lost, you need a recovered or new host with matching identity
