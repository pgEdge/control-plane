# Quorum Loss Recovery

Quorum loss occurs when the majority of server-mode hosts are offline, preventing etcd from accepting writes and blocking database operations.

Quorum loss can occur in two scenarios:

1. **[Total Quorum Loss](#total-quorum-loss-recovery)** - All server-mode hosts are offline (100% loss)
2. **[Majority Quorum Loss](#majority-quorum-loss-recovery)** - More than 50% of server-mode hosts are offline, but at least one server-mode host remains online

## Prerequisites

### Calculate Quorum

The quorum threshold determines how many server-mode hosts must be online for etcd to function. Use this formula:

**Formula:** `Quorum = floor(N/2) + 1`, where N = total server-mode hosts

| Server-Mode Hosts | Quorum | Lost When |
|-------------------|--------|-----------|
| 3 | 2 | 2+ hosts lost |
| 5 | 3 | 3+ hosts lost |
| 7 | 4 | 4+ hosts lost |

### Set Variables

Before proceeding, set the following variables with values appropriate for your environment:

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_IP="<recovery-host-ip>"
RECOVERY_HOST_ID="<recovery-host-id>"
SNAPSHOT_PATH="${PGEDGE_DATA_DIR}/snapshot.db"
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
API_PORT=<api-port>
```

## Total Quorum Loss Recovery

This section covers recovery when **all server-mode hosts are lost** (100% loss). In this scenario, the Control Plane API is not accessible on any host.

### When to Use This Section

Use this section when:

- ❌ **All server-mode hosts are offline** (100% loss)
- ❌ **Control Plane API is not accessible** on any host
- ❌ **etcd quorum is completely lost**

**Example:** A cluster with 3 server-mode hosts (`host-1`, `host-2`, `host-3`) where all three hosts have failed.

### Prerequisites for Total Quorum Loss

Before beginning recovery, ensure you have:

- A snapshot or other backup of the Control Plane data volume from before the outage
- Access to a recovery host (either a restored host or a new server-mode host)
- The recovery host must have matching certificates and configuration files from the backup

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    If your cluster previously had more than one node, you **must** use `etcdutl snapshot restore` to reset cluster membership. Simply copying the etcd directory will not work—the cluster membership needs to be reset to a single node.

### Recovery Steps for Total Quorum Loss

#### Step 1: Stop All Control Plane Services

Stop all Control Plane services across all hosts to prevent conflicts during recovery:

```bash
# On Swarm manager node
docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 control-plane_<host-id-3>=0
# ... repeat for all hosts

# Verify stopped
docker service ls --filter name=control-plane
```

!!! warning "Expected Errors During Quorum Loss"

    You may see "cannot elect leader" errors when stopping services during quorum loss. These errors are expected and safe to ignore. If Docker Swarm commands fail, you can manually stop containers:

    ```bash
    # Alternative: Stop containers directly on each host
    docker ps --filter label=com.docker.swarm.service.name=control-plane_<host-id> --format "{{.ID}}" | xargs docker stop
    ```

#### Step 2: Restore Data Volume

Restore the Control Plane data volume from your backup. The method depends on your backup solution.

!!! tip "When to Restore the Entire Volume"

    **Recommended:** Restore the entire data volume when:
    - You want to recover Postgres instance data along with Control Plane state
    - You're using volume-level snapshots (e.g., AWS EBS, Azure disk snapshots)
    - The backup is from the same host or a compatible host

    **Selective restoration:** Only restore specific files when:
    - You only need to recover Control Plane cluster state, not Postgres data
    - The backup volume contains data from a different cluster
    - You're recovering to a different host with a different host ID

**Restore the entire volume:**

```bash
# Restore the entire data volume from your backup/snapshot
# The exact method depends on your backup solution
# Example for a file-based backup:
BACKUP_VOLUME_PATH="<path-to-restored-backup-volume>"
cp -r "${BACKUP_VOLUME_PATH}"/* "${PGEDGE_DATA_DIR}/"
```

**Selective restoration (only if you cannot restore the entire volume):**

```bash
# Only restore essential Control Plane files
# Note: This approach will NOT preserve Postgres instance data
cp -r <backup-path>/certificates "${PGEDGE_DATA_DIR}/certificates"
cp <backup-path>/generated.config.json "${PGEDGE_DATA_DIR}/generated.config.json"
# Extract etcd snapshot if available
```

#### Step 3: Backup Existing etcd Data

Before restoring, create a backup of any existing etcd data on the recovery host:

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

#### Step 4: Restore etcd Data

You can restore etcd data using one of two approaches:

- **Option A: Restore from Snapshot File** - Use this when you have a previously-created snapshot file (recommended)
- **Option B: Restore from Existing Data Directory** - Use this only if you have the etcd directory from your volume backup

##### Install etcdutl

```bash
# On recovery host
# Install etcdutl if not present
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
curl -L https://github.com/etcd-io/etcd/releases/download/v3.6.5/etcd-v3.6.5-linux-${ARCH}.tar.gz | tar --strip-components 1 -xz -C /tmp etcd-v3.6.5-linux-${ARCH}/etcdutl
sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl
```

##### Option A: Restore from Snapshot File (Recommended)

If you have a previously-created snapshot file:

```bash
# On recovery host
# Backup the restored etcd directory if it exists
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

##### Option B: Restore from Existing Data Directory

!!! warning

    For multi-node clusters, you need to create a snapshot file first. However, this typically requires etcd to be running, which may not be possible during total quorum loss. **Best practice: Include snapshot files (`.db`) in your volume backups.**

    If your original cluster was single-node, the restored etcd directory may work, but using `etcdutl snapshot restore` is still recommended for consistency.

#### Step 5: Start Control Plane on Recovery Host

Start the Control Plane service on the recovery host:

```bash
# On Swarm manager node
# Start only the recovery host (one server-mode host)
docker service scale control-plane_${RECOVERY_HOST_ID}=1

# Verify service is running
docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
```

!!! note "Embedded etcd Handles Initialization Automatically"

    Control Plane automatically detects the restored snapshot and initializes etcd. No manual etcd commands are needed. The recovery host will start as a single-node etcd cluster.

#### Step 6: Verify Recovery Host

Verify that the recovery host is online and accessible:

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

After restoring one server-mode host, you now have the same situation as **Majority Quorum Loss** (one host online, but quorum not yet fully restored for multi-node clusters). Continue with the [Majority Quorum Loss Recovery](#step-5-remove-lost-hosts-from-database-specs) section starting from **Step 5: Remove Lost Hosts from Database Specs** to restore the remaining hosts.

## Majority Quorum Loss Recovery

This section covers recovery when **quorum is lost** but **at least one server-mode host is still online**. In this scenario, the Control Plane API may be accessible, but etcd cannot accept writes and database operations cannot proceed until quorum is restored.

### When to Use This Section

Use this section when:

- ✅ **At least one server-mode host is still online**
- ✅ **Control Plane API is accessible** on at least one host
- ❌ **Quorum is lost** (more than floor(N/2) server-mode hosts are offline)

**Example:** A cluster with 3 server-mode hosts where 2 hosts have failed. Quorum requires 2 hosts (floor(3/2) + 1 = 2), but only 1 is online, so quorum is lost.

**How to verify:**

```bash
# Try to access Control Plane API on server-mode hosts
curl -sS "http://<server-host-ip>:<api-port>/v1/cluster"
```

If the API is accessible on at least one host, use this section.

### Prerequisites for Majority Quorum Loss

Before beginning recovery, ensure you have:

- **etcd snapshot** (taken before outage)
- **Recovery host:** One of the remaining online server-mode hosts (already has certificates/config)

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    If your cluster previously had more than one node, you **must** use `etcdutl snapshot restore` to reset cluster membership. Simply copying the etcd directory will not work—the cluster membership needs to be reset to a single node.

### Recovery Steps for Majority Quorum Loss

#### Step 1: Backup Existing etcd Data

Before restoring, create a backup of any existing etcd data on the recovery host:

```bash
# On recovery host
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
fi
```

#### Step 2: Restore etcd Data

You can restore etcd data using one of two approaches:

- **Option A: Restore from Existing Data Directory** - Use this when the recovery host still has its etcd data directory intact from before the outage (typical scenario)
- **Option B: Restore from Snapshot File** - Use this when you have a previously-created snapshot file

##### Install etcdutl

See [Install etcdutl](#install-etcdutl) in the Total Quorum Loss Recovery section.

##### Option A: Restore from Existing Data Directory

Use this when the recovery host still has its etcd data directory intact from before the outage. This is the typical scenario when recovering from losing other machines in the cluster.

!!! note

    Even though you're using the existing data directory, you still need to use `etcdutl snapshot restore` to reset the cluster membership to a single node. You'll need a snapshot file—either use a pre-existing snapshot file from your backups (recommended—use Option B), or create a snapshot from the existing directory if etcd is still accessible.

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

##### Option B: Restore from Snapshot File

If you have a previously-created snapshot file, see [Option A: Restore from Snapshot File (Recommended)](#option-a-restore-from-snapshot-file-recommended).

#### Step 3: Start Control Plane

See [Step 5: Start Control Plane on Recovery Host](#step-5-start-control-plane-on-recovery-host).

#### Step 4: Verify Recovery Host

See [Step 6: Verify Recovery Host](#step-6-verify-recovery-host).

**Status:** You now have **1 server-mode host online**. Quorum is **not yet restored**. Continue to restore quorum.

#### Step 5: Remove Lost Hosts from Database Specs

Before removing host records, update all databases to remove lost hosts from their node's `host_ids` arrays. This ensures databases only reference hosts that are available or will be recovered.

For each database, update the spec to remove lost host IDs from each node's `host_ids` array. Only include hosts that are currently online or will be recovered. See [Updating a Database](../using/update-db.md) for details.

**Important:** Wait for each database update task to complete before proceeding. Monitor task status using the [Tasks and Logs](../using/tasks-logs.md) documentation.

#### Step 6: Remove Lost Host Records

Remove stale host records **one at a time**, waiting for each task to complete:

```bash
# For each lost host
LOST_HOST_ID="<lost-host-id>"

RESP=$(curl -sS -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/${LOST_HOST_ID}?force=true")
echo "${RESP}"
```

**Important:** The delete operation is asynchronous and returns a task. Monitor the task status using the [Tasks and Logs](../using/tasks-logs.md) documentation. Wait for each deletion task to complete before proceeding to the next host.

!!! important "Remove Hosts in Order"

    Remove server-mode hosts first, then client-mode hosts. Work **one host at a time** and wait for each deletion task to complete before proceeding.

#### Step 7: Rejoin Server-Mode Hosts Until Quorum Restored

Rejoin server-mode hosts **one at a time** until quorum threshold is reached.

For each lost server-mode host:

##### 7a. Stop Service

```bash
# On Swarm manager node
LOST_SERVER_HOST_ID="<lost-server-host-id>"
docker service scale control-plane_${LOST_SERVER_HOST_ID}=0
```

##### 7b. Clear State

```bash
# On lost host node
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

##### 7c. Start Service

```bash
# On Swarm manager node
docker service scale control-plane_${LOST_SERVER_HOST_ID}=1
```

##### 7d. Join the Lost Host to the Cluster

Join the lost host to the cluster. See [Initializing the Control Plane](../installation/installation.md#initializing-the-control-plane).

##### 7e. Verify Host Joined

```bash
# Verify the host is online and reachable
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
# Look for the host with id matching ${LOST_SERVER_HOST_ID}
# Verify it shows status: "reachable" and etcd_mode: "server"
```

##### 7f. Check Quorum Status

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

#### Step 8: Rejoin Remaining Server-Mode Hosts

After quorum is restored, rejoin any remaining server-mode hosts using Steps 7a-7e (skip 7f, as quorum is already restored).

#### Step 9: Rejoin Client-Mode Hosts

!!! important "Only After Quorum is Restored"

    Only proceed with rejoining client-mode hosts after Step 7f confirms quorum is restored.

For client-mode host recovery, see the [Partial Recovery](partial-recovery.md) guide. The process for rejoining client-mode hosts is the same whether quorum is intact or was lost and restored.

#### Step 10: Restart All Hosts

Restart all Control Plane services to ensure everything is synchronized:

```bash
# Scale all to zero
docker service scale control-plane_<host-id-1>=0
docker service scale control-plane_<host-id-2>=0
# ... repeat for all hosts

# Scale all to one
docker service scale control-plane_<host-id-1>=1
docker service scale control-plane_<host-id-2>=1
# ... repeat for all hosts
```

#### Step 11: Final Verification

Verify that all hosts are online and all databases are available:

```bash
# Check hosts
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"

# Check databases
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases"
```

**Expected:**

- All hosts: `status: "reachable"`
- All databases: `state: "available"`

## Common Issues

### "duplicate host ID" error

**Cause:** Host record still exists in etcd.

**Fix:** Remove lost hosts from database specs (Step 5 for Majority Quorum Loss), then remove host record (Step 6 for Majority Quorum Loss) and wait for task completion before rejoining. For Total Quorum Loss, follow the Next Steps section which directs you to Majority Quorum Loss Step 5.

### etcd certificate errors

**Cause:** Certificates don't match snapshot data.

**Fix:** Use the same certificate files that were used when the snapshot was taken.

### Quorum not restored

**Cause:** Not enough server-mode hosts rejoined.

**Fix:** Verify you've rejoined enough hosts to meet quorum threshold. Count only server-mode hosts.

### Control Plane fails to start

**Cause:** Old etcd processes still running or conflicting state.

**Fix:**
- Stop all Control Plane services: `docker service scale control-plane_<host-id>=0`
- Verify services are stopped: `docker service ls --filter name=control-plane`
- Clear host state:
  - For Majority Quorum Loss: See Step 7b (Clear State)
  - For Total Quorum Loss: Remove the etcd directory, certificates, and generated.config.json, then repeat Steps 3-6
- Restart the service: `docker service scale control-plane_<host-id>=1`
