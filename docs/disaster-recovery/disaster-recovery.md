# Disaster Recovery

This guide describes how to recover the Control Plane and Docker Swarm after host loss or etcd quorum loss. Follow the sections in order; skip **Restoring Docker Swarm** if Swarm still has quorum, and skip **Restoring the Control Plane** if the API is already accessible.

## When to Use This Guide

| Scenario | API accessible? | Start at |
|----------|------------------|----------|
| Quorum intact — one or more hosts lost | Yes | [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts) |
| etcd quorum lost — Swarm still works | No | [Restoring the Control Plane](#restoring-the-control-plane) |
| etcd and Swarm quorum lost | No | [Restoring Docker Swarm](#restoring-docker-swarm), then [Restoring the Control Plane](#restoring-the-control-plane) |

!!! warning

    Ensure you have a recent backup of the Control Plane data volume before starting any recovery procedure.

## Prerequisites

- Host ID(s) of the lost host(s)
- SSH access to remaining cluster hosts (for Docker Swarm and host operations)
- The Control Plane stack definition file (YAML) from your initial deployment
- If etcd quorum was lost: a backup of the Control Plane data volume and (optionally) an etcd snapshot file

See [Creating the stack definition file](../installation/installation.md#creating-the-stack-definition-file) in the installation documentation.

## Set Variables

Set the following variables for all recovery steps. When the API is already accessible, use a healthy host for `RECOVERY_HOST_IP` and the host being recovered (or the first restored host) for `RECOVERY_HOST_ID`.

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_ID="<recovery-host-id>"
RECOVERY_HOST_IP="<recovery-host-ip>"
API_PORT=<api-port>
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
```

When both etcd and Docker Swarm quorum were lost (you will complete [Restoring Docker Swarm](#restoring-docker-swarm) first):

```bash
RECOVERY_HOST_EXTERNAL_IP="<recovery-host-external-ip>"
ARCHIVE_VERSION="<control-plane-version>"
```

!!! note "Pre-created etcd snapshot is optional"

    The procedure below restores etcd from the **existing data directory** on the recovery host (after moving it aside). Use a **pre-created snapshot file** only when that data is corrupted.

### Quorum reference

**Formula:** `Quorum = floor(N/2) + 1`, where N = number of server-mode hosts.

| Server-mode hosts | Quorum | Quorum lost when |
|-------------------|--------|------------------|
| 3 | 2 | 2 or more hosts lost |
| 5 | 3 | 3 or more hosts lost |

## Data volume restore

Restoring the Control Plane data volume from your backup is environment-specific; we cannot document every possible procedure. For examples, see your provider's documentation:

- [AWS](https://docs.aws.amazon.com/prescriptive-guidance/latest/backup-recovery/restore.html)
- [VMware vSphere](https://techdocs.broadcom.com/us/en/vmware-cis/vsphere/container-storage-plugin/3-0/getting-started-with-vmware-vsphere-container-storage-plug-in-3-0/using-vsphere-container-storage-plug-in/volume-snapshot-and-restore.html)
- [Azure](https://learn.microsoft.com/en-us/azure/backup/backup-azure-arm-restore-vms)
- [Google Cloud](https://docs.cloud.google.com/compute/docs/disks/restore-snapshot)

---

## Restoring Docker Swarm

**Do this section only when Docker Swarm has lost quorum** (e.g. a majority of managers are gone). Otherwise go to [Restoring the Control Plane](#restoring-the-control-plane).

### Reinitializing the Swarm

On a surviving manager:

```bash
sudo docker swarm init --force-new-cluster --advertise-addr ${RECOVERY_HOST_IP}
```

Verify:

```bash
sudo docker node ls
```

### Joining hosts to the new Swarm

If you have other surviving nodes that should be part of the new Swarm, join them now. On the manager, get the join token:

```bash
docker swarm join-token manager   # for manager nodes
docker swarm join-token worker    # for worker nodes
```

On each node to add, run:

```bash
docker swarm join --token SWMTKN-1-xxx...xxx ${RECOVERY_HOST_IP}:2377
```

Verify with `docker node ls` on the manager.

### Removing old Swarm nodes

Demote lost managers, then remove the lost nodes:

```bash
docker node demote <LOST_HOSTNAME_1> <LOST_HOSTNAME_2>
docker node rm --force <LOST_HOSTNAME_1> <LOST_HOSTNAME_2>
```

Remove Control Plane and Postgres services that were pinned to the lost nodes:

```bash
sudo docker service rm control-plane_<LOST_HOST_ID_1> control-plane_<LOST_HOST_ID_2>
sudo docker service ls
sudo docker service rm <orphaned-postgres-service-1> <orphaned-postgres-service-2>
```

If the container registry or Control Plane image resided on a lost host, recreate the registry and image on a surviving host before starting the Control Plane (see [Upgrading the Control Plane](../installation/upgrading.md)).

---

## Restoring the Control Plane

**Do this section only when etcd quorum was lost** (Control Plane API unavailable). If the API is already accessible, go to [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts).

### Reinitializing the Control Plane etcd cluster

Use one server-mode host as the recovery host. What you do first depends on the situation:

- **All server-mode hosts were offline:** On a Swarm manager, stop all Control Plane services (`docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 ...`). Restore the data volume from your backup (see [Data volume restore](#data-volume-restore)).
- **At least one server-mode host was still up:** Use that host as the recovery host. You do not need to stop services or restore the volume first.
- **You already completed [Restoring Docker Swarm](#restoring-docker-swarm):** Restore the data volume on the surviving host if it was lost (see [Data volume restore](#data-volume-restore)); otherwise skip.

Then on the recovery host, perform the following steps once.

1. **Back up existing etcd data** (move aside for restore):

   ```bash
   if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
       ETCD_BACKUP_DIR="${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
       mv "${PGEDGE_DATA_DIR}/etcd" "${ETCD_BACKUP_DIR}"
   fi
   if [ -d "${PGEDGE_DATA_DIR}/certificates" ]; then
       cp -r "${PGEDGE_DATA_DIR}/certificates" "${PGEDGE_DATA_DIR}/certificates.backup.$(date +%s)"
   fi
   if [ -f "${PGEDGE_DATA_DIR}/generated.config.json" ]; then
       cp "${PGEDGE_DATA_DIR}/generated.config.json" "${PGEDGE_DATA_DIR}/generated.config.json.backup.$(date +%s)"
   fi
   ```

2. **Install etcdutl** (if not already installed):

   ```bash
   ETCD_VERSION="v3.6.8"
   ARCH=$(uname -m)
   if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
   curl -L "https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-${ARCH}.tar.gz" \
       | tar --strip-components 1 -xz -C /tmp "etcd-${ETCD_VERSION}-linux-${ARCH}/etcdutl"
   sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl
   ```

3. **Restore etcd** from the backup directory (step 1 sets `ETCD_BACKUP_DIR`). If you have no existing etcd directory and are using a snapshot file instead, use that file path in place of `"${ETCD_BACKUP_DIR}/member/snap/db"`:

   ```bash
   etcdutl snapshot restore "${ETCD_BACKUP_DIR}/member/snap/db" \
       --name "${RECOVERY_HOST_ID}" \
       --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
       --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
       --skip-hash-check \
       --data-dir "${PGEDGE_DATA_DIR}/etcd"
   ls -la "${PGEDGE_DATA_DIR}/etcd"
   ```

4. **Start the Control Plane** and verify:

   - If Control Plane is already deployed as Swarm services (e.g. when only etcd quorum was lost and you did not run [Restoring Docker Swarm](#restoring-docker-swarm)):  
     `docker service scale control-plane_${RECOVERY_HOST_ID}=1`
   - If you completed [Restoring Docker Swarm](#restoring-docker-swarm) and deploy via stack:  
     `docker stack deploy -c <path-to-stack-yaml> control-plane` (do not set `PGEDGE_ETCD_SERVER__FORCE_NEW_CLUSTER`).

   Then:

   ```bash
   docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
   curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
   # or, if using RECOVERY_HOST_EXTERNAL_IP: curl "http://${RECOVERY_HOST_EXTERNAL_IP}:${API_PORT}/v1/databases"
   ```

   You should see one host with `status: "reachable"` and `etcd_mode: "server"`. Then continue with [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts).

### Updating databases to remove old hosts

For each database that has instances on a lost host, submit an update with the `remove_host` query parameter and a spec that excludes that host. Wait for each update task to complete.

```sh
curl -X POST "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID>?remove_host=<LOST_HOST_ID>" \
    -H "Content-Type: application/json" \
    -d '{
        "spec": {
            "database_name": "<DB_NAME>",
            "database_users": [{"username": "admin", "db_owner": true, "attributes": ["SUPERUSER", "LOGIN"]}],
            "port": 5432,
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] }
            ]
        }
    }'
```

For multiple lost hosts, add `&remove_host=<HOST_ID>` for each. Monitor progress via [Tasks and Logs](../using/tasks-logs.md) in the Using Control Plane documentation.

### Removing old hosts

After all affected databases have been updated, remove each lost host from the Control Plane (one at a time; wait for each removal task to complete):

```sh
curl -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>"
```

Monitor: `curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>/tasks/<TASK_ID>`

On a healthy Swarm manager, remove each lost node from the Swarm:

```bash
docker node ls
# If the lost node was a manager:
docker node demote <LOST_HOSTNAME>
docker node rm <LOST_HOSTNAME> --force
```

**Verification:** Confirm only remaining hosts are listed and databases are in a good state:

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Hosts should show only remaining hosts with `status: "reachable"`. Databases should show `state: "available"` with instances only on remaining hosts.

### Re-adding hosts

For each host to restore, either rejoin an existing host (still in Swarm and reachable) or provision a new host, then join the Control Plane cluster.

**Rejoin existing host (host still accessible)** — when the lost host is reachable via SSH and Docker is running:

1. On a manager: `docker service scale control-plane_<LOST_HOST_ID>=0`
2. On that host (SSH), clear state:
   - **Server-mode:** `rm -rf "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/certificates" ; rm -f "${PGEDGE_DATA_DIR}/generated.config.json"`
   - **Client-mode:** `rm -f "${PGEDGE_DATA_DIR}/generated.config.json"`
3. On a manager: `docker service scale control-plane_<LOST_HOST_ID>=1` (or `docker stack deploy -c <path-to-stack-yaml> control-plane` if the service was removed).

**Provision new host (host destroyed)** — when the host must be recreated:

1. Create the new host and install prerequisites (Docker, etc.) per your environment.
2. On the new host, join Docker Swarm (obtain the token on a manager with `docker swarm join-token manager` or `worker`):
   `docker swarm join --token SWMTKN-1-xxx...xxx ${RECOVERY_HOST_IP}:2377`. Verify with `docker node ls` on the manager.
3. On the new host: `sudo mkdir -p /data/control-plane` (or your `PGEDGE_DATA_DIR`).
4. On a manager: `docker stack deploy -c <path-to-stack-yaml> control-plane`. Verify with `docker service ps control-plane_<HOST_ID>`.

**Join Control Plane cluster** (for each host re-added, whether rejoined or new):

1. From any existing member: `JOIN_TOKEN="$(curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token)"`
2. On the host being added (not on an existing member):  
   `curl -X POST http://<NEW_HOST_IP>:${API_PORT}/v1/cluster/join -H 'Content-Type:application/json' --data "${JOIN_TOKEN}"`
3. Verify: `curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts` — the host should show `status: "reachable"` and the correct `etcd_mode`.

Repeat for each host. Re-add **server-mode hosts first**, then client-mode hosts.

### Updating databases to re-add hosts

After hosts are back in the cluster, add them to each database spec so the Control Plane can create instances and sync data:

```sh
curl -X POST http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID> \
    -H 'Content-Type:application/json' \
    --data '{
        "spec": {
            "database_name": "<DB_NAME>",
            "database_users": [{"username": "admin", "db_owner": true, "attributes": ["SUPERUSER", "LOGIN"]}],
            "port": 5432,
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

To choose a specific source node for data sync, add `"source_node": "n2"` to the node entry. Monitor progress via [Tasks and Logs](../using/tasks-logs.md) in the Using Control Plane documentation.

**Final verification:**

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Confirm: all hosts have `status: "reachable"`; server-mode hosts have `etcd_mode: "server"`; etcd health shows `has_quorum: true` with the expected member count; all databases have `state: "available"` with instances on all hosts; all subscriptions have `status: "replicating"`; Docker Swarm reports all nodes as `Ready`; data is replicating correctly across nodes.

---

## Common issues

| Issue | Cause | Fix |
|-------|-------|-----|
| "duplicate host ID" when rejoining | Host record still in etcd | Complete [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts) and [Removing old hosts](#removing-old-hosts) for that host and wait for the removal task to complete before rejoining. |
| Host joins but shows unreachable | Network or service not fully up | Check `docker service logs control-plane_<HOST_ID>`, verify connectivity, allow service to finish initializing. |
| etcd certificate errors | Certificates do not match restored data | Use the same certificate files that were in use when the snapshot or data was created. |
| Quorum not restored | Too few server-mode hosts rejoined | Rejoin enough server-mode hosts to reach quorum (e.g. 2 of 3 for a 3-node cluster). |
| Docker Swarm commands hang | Swarm has lost quorum | Run [Reinitializing the Swarm](#reinitializing-the-swarm) on a surviving manager. |
| "service already exists" when deploying stack | Manually created service conflicts with stack | Run `docker service rm <service-name>`, then redeploy the stack. |
| Control Plane API hangs after etcd restore | etcd auth not fully re-enabled after restore | Check `docker service logs control-plane_<HOST_ID>`. Restart the service if necessary. |
| Image pull fails on new hosts | Registry was on a lost host | Recreate the registry on a surviving host and ensure new hosts can reach it. |
| "etcd already initialized" | Stale etcd data on host being joined | Clear the data directory on that host before joining (see [Re-adding hosts](#re-adding-hosts), rejoin step 2). |
| Control Plane fails to start | Stale etcd processes or conflicting state | Stop the service (`docker service scale control-plane_<host-id>=0`), clear host state (etcd, certificates, generated.config.json), then start again. |
| Database instances do not restore | Database spec does not include recovered host | [Updating databases to re-add hosts](#updating-databases-to-re-add-hosts) for that database. |

---

## Summary

| Section | When to run |
|---------|-------------|
| [Restoring Docker Swarm](#restoring-docker-swarm) | Only when Swarm quorum was lost |
| [Restoring the Control Plane](#restoring-the-control-plane) | Only when etcd quorum was lost; otherwise start at [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts) |
| [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts) | Always, once the API is available |
| [Removing old hosts](#removing-old-hosts) | Always, after updating databases |
| [Re-adding hosts](#re-adding-hosts) | For each host to restore |
| [Updating databases to re-add hosts](#updating-databases-to-re-add-hosts) | After re-adding hosts |
