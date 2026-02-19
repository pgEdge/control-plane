# Disaster Recovery

This guide covers recovery of the Control Plane and Docker Swarm or etcd quorum loss. Use the same steps whether you lost one host (API still up) or lost quorum (API down until you restore one host); only the **starting point** differs.

## When to Use This Guide

| Scenario | API accessible? | Start at |
|----------|------------------|----------|
| Quorum intact — one or more hosts lost | Yes | [Removing lost hosts](#removing-lost-hosts) |
| etcd quorum lost — Swarm still works | No | [Restoring the Control Plane](#restoring-the-control-plane) (1A or 1B), then Removing lost hosts |
| etcd and Swarm quorum lost | No | [Restoring Docker Swarm](#restoring-docker-swarm), then [Restoring the Control Plane](#restoring-the-control-plane) (1C), then Removing lost hosts |

!!! warning

    Before attempting any recovery procedure, ensure you have recent backups of your Control Plane data volume.

## Prerequisites

- The failed or lost host(s) identified by host ID
- SSH access to healthy cluster hosts (for Docker Swarm and host operations)
- The Docker Swarm stack YAML file used to deploy Control Plane services
- When etcd quorum was lost: a backup of the Control Plane data volume and (optionally) an etcd snapshot file

Ensure you have a Control Plane stack definition file from your initial deployment. See [Creating the stack definition file](https://github.com/pgEdge/control-plane/blob/main/docs/installation/installation.md#deploying-the-stack) in the installation documentation.

## Set Variables

Set these variables for all recovery steps. When the API is already accessible, use any healthy host for `RECOVERY_HOST_IP` and the host you are recovering or the first restored host for `RECOVERY_HOST_ID` as needed.

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_ID="<recovery-host-id>"
RECOVERY_HOST_IP="<recovery-host-ip>"
API_PORT=<api-port>
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
```

**When both etcd and Docker Swarm quorum were lost (Section 1C only):**

```bash
RECOVERY_HOST_EXTERNAL_IP="<recovery-host-external-ip>"
ARCHIVE_VERSION="<control-plane-version>"
```

!!! note "Using a pre-created snapshot is optional"

    The procedures below restore etcd from the **existing data directory** on the recovery host (after moving it aside in the backup step). That data is up-to-date. Use a **pre-created snapshot file** only when the Control Plane database is corrupted; otherwise prefer the existing data directory.

---

## Restoring Docker Swarm

**Only when Docker Swarm has lost quorum** (e.g. majority of managers gone). Skip this section if Swarm is still functional.

### Reinitializing the Swarm

On the surviving manager host:

```bash
sudo docker swarm init --force-new-cluster --advertise-addr ${RECOVERY_HOST_IP}
```

Verify:

```bash
sudo docker node ls
```

### Removing old Swarm nodes

Demote dead managers, then remove dead nodes:

```bash
docker node demote <DEAD_HOSTNAME_1> <DEAD_HOSTNAME_2>
docker node rm --force <DEAD_HOSTNAME_1> <DEAD_HOSTNAME_2>
```

### Cleaning up orphaned services

Remove Control Plane and Postgres services that were constrained to destroyed nodes:

```bash
sudo docker service rm control-plane_<DEAD_HOST_ID_1> control-plane_<DEAD_HOST_ID_2>
sudo docker service ls
sudo docker service rm <orphaned-postgres-service-1> <orphaned-postgres-service-2>
```

If the container registry or Control Plane image was on a destroyed host, recreate the registry the Control Plane image on the surviving host before starting the Control Plane (see [Upgrading the Control Plane](https://github.com/pgEdge/control-plane/blob/main/docs/installation/upgrading.md#upgrading-the-control-plane)).

---

## Restoring the Control Plane

**Only when etcd quorum was lost** (Control Plane API unavailable). Skip this section if the API is already accessible and go to [Removing lost hosts](#removing-lost-hosts).

Complete **one** of the following paths (1A, 1B, or 1C) to get one server-mode host running with the API accessible.

### Quorum reference

**Formula:** `Quorum = floor(N/2) + 1`, where N = total server-mode hosts.

| Server-mode hosts | Quorum | Lost when |
|-------------------|--------|-----------|
| 3 | 2 | 2+ hosts lost |
| 5 | 3 | 3+ hosts lost |

### Path 1A: Total etcd quorum loss (Swarm still works)

**Use when:** All server-mode hosts are offline but Docker Swarm is still functional.

1. **Stop all Control Plane services** (on a Swarm manager):

   ```bash
   docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 control-plane_<host-id-3>=0
   docker service ls --filter name=control-plane
   ```

2. **Restore data volume** from your backup (procedure depends on your environment; see [installation/backup docs](../installation/installation.md)).

3. **Backup existing etcd data** (move aside so we can restore from it):

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

4. **Install etcdutl** (if not already installed):

   ```bash
   ETCD_VERSION="v3.6.8"
   ARCH=$(uname -m)
   if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
   curl -L "https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-${ARCH}.tar.gz" \
       | tar --strip-components 1 -xz -C /tmp "etcd-${ETCD_VERSION}-linux-${ARCH}/etcdutl"
   sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl
   ```

5. **Restore etcd** from the backup directory (or from a snapshot file path if you have no existing etcd dir):

   ```bash
   etcdutl snapshot restore "${ETCD_BACKUP_DIR}/member/snap/db" \
       --name "${RECOVERY_HOST_ID}" \
       --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
       --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
       --skip-hash-check \
       --data-dir "${PGEDGE_DATA_DIR}/etcd"
   ls -la "${PGEDGE_DATA_DIR}/etcd"
   ```

6. **Start Control Plane** and verify:

   ```bash
   docker service scale control-plane_${RECOVERY_HOST_ID}=1
   docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
   curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
   ```

   You should see one host with `status: "reachable"` and `etcd_mode: "server"`. Then continue at [Removing lost hosts](#removing-lost-hosts).

### Path 1B: Majority etcd quorum loss (one server-mode host still up)

**Use when:** At least one server-mode host is still online but etcd quorum is lost. Swarm still works.

1. **Backup existing etcd data** on the recovery host:

   ```bash
   if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
       ETCD_BACKUP_DIR="${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
       mv "${PGEDGE_DATA_DIR}/etcd" "${ETCD_BACKUP_DIR}"
   fi
   ```

2. **Install etcdutl** (same as in Path 1A, step 4).

3. **Restore etcd** (same command as Path 1A, step 5).

4. **Start Control Plane** and verify (same as Path 1A, step 6). Then continue at [Removing lost hosts](#removing-lost-hosts).

### Path 1C: etcd and Docker Swarm quorum both lost

**Use when:** Majority of hosts are destroyed; both etcd and Swarm have lost quorum. Complete [Restoring Docker Swarm](#restoring-docker-swarm) first, then:

1. **Restore data volume** on the surviving host if it was lost; otherwise skip.

2. **Backup existing etcd data** on the surviving host (same as Path 1B, step 1).

3. **Install etcdutl** and **restore etcd** (same as Path 1A, steps 4–5).

4. **Start Control Plane** using your normal deployment (e.g. `docker stack deploy` or `docker service create`). Do not use `PGEDGE_ETCD_SERVER__FORCE_NEW_CLUSTER`. Verify with:

   ```bash
   curl http://${RECOVERY_HOST_EXTERNAL_IP}:${API_PORT}/v1/databases
   ```

   Then continue at [Removing lost hosts](#removing-lost-hosts).

---

## Removing lost hosts

Do this section **whenever** you have lost one or more hosts and the Control Plane API is accessible (either it never went down or you restored it in the previous section). Repeat the host-removal steps for each lost host if multiple are gone.

### Updating databases to remove old hosts

For each database that has instances on a lost host, update the database with the `remove_host` query parameter and a spec that excludes that host. Wait for each update task to complete.

```sh
# Example: remove one or more hosts from a database
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

For multiple lost hosts, add `&remove_host=<HOST_ID>` for each. Monitor tasks using the [Tasks and Logs](../using/tasks-logs.md) documentation.

### Removing old hosts

After all affected databases have been updated, remove each lost host from the Control Plane (one at a time, wait for the task to complete):

```sh
curl -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>"
```

Monitor: `curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>/tasks/<TASK_ID>`

### Cleaning up Docker Swarm

On a healthy manager node, remove each failed node from the Swarm:

```bash
docker node ls
# If the failed node was a manager:
docker node demote <FAILED_HOSTNAME>
docker node rm <FAILED_HOSTNAME> --force
```

---

## Verification (after removing lost hosts)

Confirm only healthy hosts remain and databases are in a good state:

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

- Hosts list should show only remaining hosts with `status: "reachable"`.
- Databases should show `state: "available"` with only surviving instances.

---

## Re-adding hosts

For each host you want to bring back, either **rejoin an existing host** (still in Swarm, reachable) or **provision a new host**, then **join the Control Plane cluster**.

### Path A: Rejoin existing host (host still accessible)

When the lost host is still reachable via SSH and Docker is running:

1. **Stop the Control Plane service** (on a manager):

   ```bash
   docker service scale control-plane_<LOST_HOST_ID>=0
   ```

2. **Clear host state** on that host (SSH there):

   ```bash
   rm -rf "${PGEDGE_DATA_DIR}/etcd"
   rm -rf "${PGEDGE_DATA_DIR}/certificates"
   rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
   ```

3. **Start the service** (or redeploy stack if the service was removed):

   ```bash
   docker service scale control-plane_<LOST_HOST_ID>=1
   # or: docker stack deploy -c <path-to-stack-yaml> control-plane
   ```

4. Go to [Join Control Plane cluster](#join-control-plane-cluster).

### Path B: Provision new host (host destroyed)

When the host must be recreated:

1. **Create the new host** and install prerequisites (Docker, etc.) per your infrastructure.

2. **Join Docker Swarm** from the new host (get token on a manager with `docker swarm join-token manager` or `worker`):

   ```bash
   docker swarm join --token SWMTKN-1-xxx...xxx ${RECOVERY_HOST_IP}:2377
   ```

   Verify with `docker node ls` on the manager.

3. **Prepare data directory** on the new host:

   ```bash
   sudo mkdir -p /data/control-plane
   ```

4. **Deploy Control Plane** (on a manager):

   ```bash
   docker stack deploy -c <path-to-stack-yaml> control-plane
   ```

   Verify with `docker service ps control-plane_<HOST_ID>`.

5. Go to [Join Control Plane cluster](#join-control-plane-cluster).

### Join Control Plane cluster

Do this for each host you re-added (Path A or B):

1. **Get join token** (from any existing member):

   ```sh
   JOIN_TOKEN="$(curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token)"
   ```

2. **Call the join API on the host being added** (not on an existing member):

   ```sh
   curl -X POST http://<NEW_HOST_IP>:${API_PORT}/v1/cluster/join \
       -H 'Content-Type:application/json' \
       --data "${JOIN_TOKEN}"
   ```

3. **Verify:** `curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts` — the host should show `status: "reachable"` and the correct `etcd_mode`.

Repeat for each host. Recover **server-mode hosts first**, then client-mode.

---

## Updating databases to re-add hosts

After hosts are back in the cluster, add them back to each database spec so Control Plane can create instances and sync data:

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

To choose a specific source node for data sync, add `"source_node": "n2"` to the node entry. Monitor tasks and logs as in [Tasks and Logs](../using/tasks-logs.md).

---

## Final verification

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Confirm:

- [ ] All hosts show `status: "reachable"`
- [ ] Server-mode hosts show `etcd_mode: "server"`
- [ ] etcd health shows `has_quorum: true` with correct member count
- [ ] All databases show `state: "available"` and instances on all hosts
- [ ] All subscriptions show `status: "replicating"`
- [ ] Docker Swarm shows all nodes `Ready`
- [ ] Data replicates correctly across nodes

---

## Common issues

### "duplicate host ID" error when rejoining

**Cause:** Host record still exists in etcd.

**Fix:** Complete [Updating databases to remove old hosts](#updating-databases-to-remove-old-hosts) and [Removing old hosts](#removing-old-hosts) for that host and wait for the removal task to finish before rejoining.

### Host joins but shows as unreachable

**Cause:** Network or service not fully started.

**Fix:** Check `docker service logs control-plane_<HOST_ID>`, verify connectivity, and wait for the service to initialize.

### etcd certificate errors

**Cause:** Certificates don't match the data being restored.

**Fix:** Use the same certificate files that were used when the snapshot/data was created.

### Quorum not restored

**Cause:** Not enough server-mode hosts rejoined.

**Fix:** Rejoin enough server-mode hosts to meet quorum (e.g. 2 of 3 for a 3-node cluster).

### Docker Swarm commands hang

**Cause:** Swarm quorum lost.

**Fix:** Run [Reinitializing the Swarm](#reinitializing-the-swarm) on the surviving manager.

### "service already exists" when deploying stack

**Cause:** Manually created service conflicts with the stack.

**Fix:** `docker service rm <service-name>`, then redeploy the stack.

### Control Plane API hangs after etcd restore

**Cause:** etcd auth not fully re-enabled after restore.

**Fix:** Check `docker service logs control-plane_<HOST_ID>`. Restart the service if needed.

### Image pull fails on new hosts

**Cause:** Registry was on a destroyed host.

**Fix:** Recreate the registry on the surviving host and ensure new hosts can reach it.

### "etcd already initialized" error

**Cause:** Stale etcd data on the host being joined.

**Fix:** Clear the data directory on that host before joining (see [Path A: Rejoin existing host](#path-a-rejoin-existing-host-host-still-accessible), step 2).

### Control Plane fails to start

**Cause:** Old etcd processes or conflicting state.

**Fix:** Stop the service (`docker service scale control-plane_<host-id>=0`), clear host state (etcd, certificates, generated.config.json), then start again.

### Database instances don't restore

**Cause:** Database spec doesn't include the recovered host.

**Fix:** [Updating databases to re-add hosts](#updating-databases-to-re-add-hosts) for that database.

---

## Summary

| Section | When to do it |
|---------|----------------|
| [Restoring Docker Swarm](#restoring-docker-swarm) | Only when Swarm quorum was lost |
| [Restoring the Control Plane](#restoring-the-control-plane) (1A, 1B, or 1C) | Only when etcd quorum was lost |
| [Removing lost hosts](#removing-lost-hosts) | Always (once API is available) |
| [Verification](#verification-after-removing-lost-hosts) | After removing lost hosts |
| [Re-adding hosts](#re-adding-hosts) | For each host you want back |
| [Updating databases to re-add hosts](#updating-databases-to-re-add-hosts) | After re-adding hosts |
| [Final verification](#final-verification) | End of recovery |
