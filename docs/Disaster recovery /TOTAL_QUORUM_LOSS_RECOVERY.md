# Total Quorum Loss Recovery Guide

Recovery guide for when **all server-mode hosts are lost** (100% of server-mode hosts offline).

## Prerequisites

- A snapshot or other backup of the Control Plane data volume from before the outage
- Recovery host: A recovered or new server-mode host with matching certificates/config

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    **If your cluster previously had more than one node, you MUST use `etcdutl snapshot restore` to reset cluster membership.** From your volume-level backup, extract the etcd data and use `etcdutl snapshot restore` to reset the cluster to a single node. Simply copying the etcd directory will not work - the cluster membership needs to be reset.

## Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_IP="<recovery-host-ip>"
RECOVERY_HOST_ID="<recovery-host-id>"
SNAPSHOT_PATH="<path-to-etcd-snapshot.db>"  # Extract from volume backup if needed
ETCD_PEER_PORT=<etcd-peer-port>  # Usually 2380
API_PORT=<api-port>
```

## Recovery Steps

This guide covers recovering **one server-mode host** from backup. After you have one host online, follow the [Majority Quorum Loss Recovery Guide](./MAJORITY_QUORUM_LOSS_RECOVERY.md).

### Step 1: Stop All Control Plane Services

```bash
# On Swarm manager node
docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 control-plane_<host-id-3>=0
```

### Step 2: Restore Data Volume

**When to restore the entire volume (recommended):**
- ✅ Normal recovery scenario - restore everything to preserve all data
- ✅ You want to recover Postgres instance data along with Control Plane state
- ✅ The backup is from the same host or a compatible host
- ✅ You're using volume-level snapshots (e.g., AWS EBS, Azure disk snapshots)

**When NOT to restore the entire volume (selective restoration):**
- ❌ You only need to recover Control Plane cluster state, not Postgres data
- ❌ The backup volume contains data from a different cluster
- ❌ You're recovering to a different host with a different host ID and want a clean start
- ❌ The volume backup is corrupted or incomplete

**Restore the entire volume:**
```bash
# Restore the entire data volume from your backup/snapshot
# The exact method depends on your backup solution (e.g., AWS EBS snapshot, Azure disk snapshot, etc.)
# Example for a file-based backup:
BACKUP_VOLUME_PATH="<path-to-restored-backup-volume>"
cp -r "${BACKUP_VOLUME_PATH}"/* "${PGEDGE_DATA_DIR}/"
```

**Selective restoration (only if you cannot restore the entire volume):**
```bash
# Only restore essential Control Plane files (not recommended - you'll lose Postgres data)
cp -r <backup-path>/certificates "${PGEDGE_DATA_DIR}/certificates"
cp <backup-path>/generated.config.json "${PGEDGE_DATA_DIR}/generated.config.json"
# Extract etcd snapshot if available
# Note: This approach will NOT preserve Postgres instance data
```

### Step 3: Backup Existing etcd Data

```bash
# On recovery host
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
fi
if [ -d "${PGEDGE_DATA_DIR}/certificates" ]; then
    cp -r "${PGEDGE_DATA_DIR}/certificates" "${PGEDGE_DATA_DIR}/certificates.backup.$(date +%s)"
fi
if [ -f "${PGEDGE_DATA_DIR}/generated.config.json" ]; then
    cp "${PGEDGE_DATA_DIR}/generated.config.json" "${PGEDGE_DATA_DIR}/generated.config.json.backup.$(date +%s)"
fi
```

### Step 4: Reset etcd Cluster Membership

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    **If your cluster previously had more than one node, you MUST use `etcdutl snapshot restore` to reset cluster membership.** Even after restoring the entire data volume, the etcd cluster membership needs to be reset to a single node. Simply using the restored etcd directory will not work for multi-node clusters.

**If you have a snapshot file (`.db`) from your backup:**

```bash
# On recovery host
# Install etcdutl if not present
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
curl -L https://github.com/etcd-io/etcd/releases/download/v3.6.5/etcd-v3.6.5-linux-${ARCH}.tar.gz | tar --strip-components 1 -xz -C /tmp etcd-v3.6.5-linux-${ARCH}/etcdutl
sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl

# Backup the restored etcd directory
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.restored.$(date +%s)"
fi

# Restore snapshot and reset cluster membership to single node
etcdutl snapshot restore "${SNAPSHOT_PATH}" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --bump-revision 1000000000 \
    --mark-compacted \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"

# Verify restore
ls -la "${PGEDGE_DATA_DIR}/etcd"
```

**If you only have the etcd directory from your volume backup (no snapshot file):**

For multi-node clusters, you need to create a snapshot file first. However, this typically requires etcd to be running, which may not be possible during total quorum loss. **Best practice: Include snapshot files (`.db`) in your volume backups.**

If your original cluster was single-node, the restored etcd directory may work, but using `etcdutl snapshot restore` is still recommended for consistency.

**Note:** The `--bump-revision` and `--mark-compacted` flags are recommended for production to prevent revision issues with clients using watches.

### Step 5: Start Control Plane on Recovery Host

```bash
# On Swarm manager node
# Start only the recovery host (one server-mode host)
docker service scale control-plane_${RECOVERY_HOST_ID}=1

# Wait for service to start
sleep 10

# Verify service is running
docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
```

### Step 6: Verify Recovery Host

```bash
# Verify API is accessible
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster"

# Verify host is online and check host count
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
```

**Expected Result:** 
- API should be accessible
- One host should show `status: "reachable"` and `etcd_mode: "server"`
- Server count should be **1**

**Status:** You now have **1 server-mode host online** with quorum restored (1 of 1 = quorum). You need to re-add the other Control Plane server-mode hosts to restore redundancy and full cluster functionality.

## Next Steps

Continue with [Step 6: Remove Lost Host Records](./MAJORITY_QUORUM_LOSS_RECOVERY.md#step-6-remove-lost-host-records) from the Majority Quorum Loss Recovery Guide.

