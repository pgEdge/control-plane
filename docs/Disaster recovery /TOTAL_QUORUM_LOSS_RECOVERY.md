# Total Quorum Loss Recovery Guide

Recovery guide for when **all server-mode hosts are lost** (100% of server-mode hosts offline).

## Prerequisites

- etcd data backup directory (from before outage)
- Certificate files and `generated.config.json` from a server-mode host (from before outage)
- Recovery host: A recovered or new server-mode host with matching certificates/config

## Set Variables

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_IP="<recovery-host-ip>"
RECOVERY_HOST_ID="<recovery-host-id>"
BACKUP_ETCD_DIR="<path-to-backup-etcd-directory>"
API_PORT=<api-port>
```

## Recovery Steps

This guide covers recovering **one server-mode host** from backup. After you have one host online, follow the [Majority Quorum Loss Recovery Guide](./MAJORITY_QUORUM_LOSS_RECOVERY.md).

### Step 1: Stop All Control Plane Services

```bash
# On Swarm manager node
docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 control-plane_<host-id-3>=0
```

### Step 2: Prepare Recovery Host

Copy certificate files and `generated.config.json` from before outage to:
- `${PGEDGE_DATA_DIR}/certificates/`
- `${PGEDGE_DATA_DIR}/generated.config.json`

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

### Step 4: Restore from Backup

```bash
# On recovery host
# Remove existing etcd directory if present
rm -rf "${PGEDGE_DATA_DIR}/etcd"

# Copy etcd data from backup directory
cp -r "${BACKUP_ETCD_DIR}" "${PGEDGE_DATA_DIR}/etcd"

# Verify restore
ls -la "${PGEDGE_DATA_DIR}/etcd"
```

**Example:** If your backup is at `/backup/etcd.backup.1234567890`:
```bash
BACKUP_ETCD_DIR="/backup/etcd.backup.1234567890"
rm -rf "${PGEDGE_DATA_DIR}/etcd"
cp -r "${BACKUP_ETCD_DIR}" "${PGEDGE_DATA_DIR}/etcd"
```

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
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster" | jq '.'

# Verify host is online and check host count
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts" | jq '.[] | {id, status, etcd_mode}'
```

**Expected Result:** 
- API should be accessible
- One host should show `status: "reachable"` and `etcd_mode: "server"`
- Server count should be **1**

**Status:** You now have **1 server-mode host online**. Quorum is **NOT YET RESTORED**.

## Next Steps

Continue with: [Majority Quorum Loss Recovery Guide](./MAJORITY_QUORUM_LOSS_RECOVERY.md)
