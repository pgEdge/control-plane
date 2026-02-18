# Complete Failure Recovery (No Quorum)

This guide covers recovery when **etcd quorum**, **Docker Swarm quorum**, or **both** are lost. When quorum is lost, the Control Plane API becomes unavailable and database operations are blocked until recovery is complete.

Quorum loss can occur in three scenarios:

1. **[Total Quorum Loss](#phase-1a-total-quorum-loss)** — All server-mode hosts are offline (100% loss). Docker Swarm is still functional.
2. **[Majority Quorum Loss](#phase-1b-majority-quorum-loss)** — More than 50% of server-mode hosts are offline, but at least one remains online. Docker Swarm is still functional.
3. **[etcd and Docker Swarm Quorum Loss](#phase-1c-etcd-and-docker-swarm-quorum-loss)** — Both etcd and Docker Swarm have lost quorum (majority of hosts destroyed). Requires Swarm re-initialization.

All three scenarios follow the same overall recovery flow:

1. **Phase 1** — Restore one server-mode host to a working state *(scenario-specific)*
2. **Phase 2** — Remove dead hosts and clean up databases *(common)*
3. **Phase 3** — Rejoin or provision replacement hosts *(common, with branching for existing vs new hosts)*
4. **Phase 4** — Restore database capacity *(common)*
5. **Phase 5** — Final verification *(common)*

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
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
```

**Additional variables for etcd and Docker Swarm Quorum Loss (Phase 1C only):**

```bash
RECOVERY_HOST_EXTERNAL_IP="<recovery-host-external-ip>"  # e.g., 192.168.105.4
ARCHIVE_VERSION="<control-plane-version>"                  # e.g., 0.6.2
```
### Creating the Stack Definition File
    // TO DO link the installation.md file section here
### Determine Your Scenario

| Condition | Scenario |
|-----------|----------|
| All server-mode hosts are offline, Docker Swarm works | [Phase 1A: Total Quorum Loss](#phase-1a-total-quorum-loss) |
| At least one server-mode host online, Docker Swarm works | [Phase 1B: Majority Quorum Loss](#phase-1b-majority-quorum-loss) |
| Both etcd and Docker Swarm quorum lost (hosts destroyed) | [Phase 1C: etcd and Docker Swarm Quorum Loss](#phase-1c-etcd-and-docker-swarm-quorum-loss) |

---

## Phase 1: Restore the Recovery Host

Complete **one** of the three paths below to get a single server-mode host running with the Control Plane API accessible. Then proceed to [Phase 2](#phase-2-remove-dead-hosts).

---

### Phase 1A: Total Quorum Loss

**Use when all server-mode hosts are offline (100% loss) but Docker Swarm is still functional.**

#### Prerequisites

- A snapshot or backup of the Control Plane data volume from before the outage
- Access to a recovery host with matching certificates and configuration from the backup

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    If your cluster previously had more than one node, you **must** use `etcdutl snapshot restore` to reset cluster membership. Simply copying the etcd directory will not work.

#### Step 1A.1: Stop All Control Plane Services

```bash
# On Swarm manager node
docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 control-plane_<host-id-3>=0

# Verify stopped
docker service ls --filter name=control-plane
```

#### Step 1A.2: Restore Data Volume

Restore the Control Plane data volume from your backup.

**Restore the entire volume (recommended):**

```bash
BACKUP_VOLUME_PATH="<path-to-restored-backup-volume>"
cp -r "${BACKUP_VOLUME_PATH}"/* "${PGEDGE_DATA_DIR}/"
```

**Selective restoration (only if you cannot restore the entire volume):**

```bash
cp -r <backup-path>/certificates "${PGEDGE_DATA_DIR}/certificates"
cp <backup-path>/generated.config.json "${PGEDGE_DATA_DIR}/generated.config.json"
```

#### Step 1A.3: Backup Existing etcd Data

```bash
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

#### Step 1A.4: Restore etcd from Snapshot

##### Install etcdutl

```bash
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
curl -L https://github.com/etcd-io/etcd/releases/download/v3.6.5/etcd-v3.6.5-linux-${ARCH}.tar.gz \ 
    | tar --strip-components 1 -xz -C /tmp etcd-v3.6.5-linux-${ARCH}/etcdutl
sudo mv /tmp/etcdutl /usr/local/bin/ && sudo chmod +x /usr/local/bin/etcdutl
```

##### Restore Snapshot

```bash

etcdutl snapshot restore <old etcd dir>/member/snap/db \
	--name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --skip-hash-check \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"

ls -la "${PGEDGE_DATA_DIR}/etcd"
```

!!! warning

    For multi-node clusters, you need a snapshot file. **Best practice: Include snapshot files (`.db`) in your volume backups.**
// To Do A snapshot file is optional. It's mainly useful when you're recovering from some type of database corruption. This best practice note would be much more helpful in the installation steps.

#### Step 1A.5: Start Control Plane

```bash
docker service scale control-plane_${RECOVERY_HOST_ID}=1
docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
```

#### Step 1A.6: Verify Recovery Host

```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster"
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
```

**Expected:** API accessible, one host with `status: "reachable"` and `etcd_mode: "server"`.

Now proceed to [Phase 2: Remove Dead Hosts](#phase-2-remove-dead-hosts).

---

### Phase 1B: Majority Quorum Loss

**Use when at least one server-mode host is still online but quorum is lost. Docker Swarm is still functional.**

#### Prerequisites

- **Recovery host:** One of the remaining online server-mode hosts

!!! important "Reset Cluster Membership for Multi-Node Clusters"

    If your cluster previously had more than one node, you **must** use `etcdutl snapshot restore` to reset cluster membership.

#### Step 1B.1: Backup Existing etcd Data

```bash
if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
    mv "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
fi
```

#### Step 1B.2: Restore etcd from Snapshot

##### Install etcdutl

See [Install etcdutl](#install-etcdutl) in Phase 1A.

##### Option A: Create Snapshot from Existing Data (if etcd is accessible)
// To Do The title of this section, "Majority Quorum Loss", implies that Etcd is unavailable. Please remove this step.
```bash
# Extract credentials from generated.config.json
# ETCD_USER="<etcd-username>"
# ETCD_PASS="<etcd-password>"

ETCDCTL_API=3 etcdctl snapshot save "${PGEDGE_DATA_DIR}/snapshot.db" \
    --endpoints "https://localhost:${ETCD_CLIENT_PORT}" \
    --cacert "${PGEDGE_DATA_DIR}/certificates/ca.crt" \
    --cert "${PGEDGE_DATA_DIR}/certificates/etcd-user.crt" \
    --key "${PGEDGE_DATA_DIR}/certificates/etcd-user.key" \
    --user "${ETCD_USER}" \
    --password "${ETCD_PASS}"
// To Do a snapshot is optional and useful for a very specific situation. In the scenarios that you're describing in this document, I would just use the existing data directory.

etcdutl snapshot restore "${PGEDGE_DATA_DIR}/snapshot.db" \
    --name "${RECOVERY_HOST_ID}" \
    --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
    --bump-revision 1000000000 \
    --mark-compacted \
    --data-dir "${PGEDGE_DATA_DIR}/etcd"
```

##### Option B: Use Pre-existing Snapshot File

If you have a snapshot file from your backups, follow the [Restore Snapshot](#restore-snapshot) steps in Phase 1A.

#### Step 1B.3: Start Control Plane

```bash
docker service scale control-plane_${RECOVERY_HOST_ID}=1
docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
```

#### Step 1B.4: Verify Recovery Host

```bash
curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster"
```

**Expected:** API accessible, one host with `status: "reachable"` and `etcd_mode: "server"`.

Now proceed to [Phase 2: Remove Dead Hosts](#phase-2-remove-dead-hosts).

---

### Phase 1C: etcd and Docker Swarm Quorum Loss

**Use when both etcd and Docker Swarm have lost quorum because the majority of hosts are destroyed.**

!!! note

    If Docker Swarm is still functional (only etcd lost quorum), use [Phase 1B](#phase-1b-majority-quorum-loss) instead.

#### Step 1C.1: Recover Docker Swarm

Re-initialize Swarm as a single-node cluster on the surviving host:

```bash
sudo docker swarm init --force-new-cluster --advertise-addr ${RECOVERY_HOST_IP}
```

Verify:

```bash
sudo docker node ls
```

Example output:

```
ID                            HOSTNAME      STATUS    AVAILABILITY   MANAGER STATUS
4aoqjp3q8jcny4kec5nadcn6x     lima-host-1   Down      Active         Unreachable
959g9937i62judknmr40kcw9r *   lima-host-2   Ready     Active         Leader
l0l51d890edg3f0ccd0xppw06     lima-host-3   Down      Active         Unreachable
```

#### Step 1C.2: Remove Dead Swarm Nodes

```bash
# Demote dead managers first (if they were managers)
docker node demote <DEAD_HOSTNAME_1> <DEAD_HOSTNAME_2>

# Force remove dead nodes
docker node rm --force <DEAD_HOSTNAME_1> <DEAD_HOSTNAME_2>
```

Example:

```bash
docker node demote lima-host-1 lima-host-3
docker node rm --force lima-host-1 lima-host-3
```

#### Step 1C.3: Clean Up Orphaned Services

Remove services constrained to destroyed nodes:

```bash
# Remove Control Plane services for dead hosts
sudo docker service rm control-plane_<DEAD_HOST_ID_1> control-plane_<DEAD_HOST_ID_2>

# List and remove orphaned Postgres services
sudo docker service ls
sudo docker service rm <orphaned-postgres-service-1> <orphaned-postgres-service-2>
```

Example:

```bash
sudo docker service rm control-plane_host-1 control-plane_host-3
sudo docker service rm postgres-storefront-n1-689qacsi postgres-storefront-n3-ant97dj4
```

#### Step 1C.6: Start Control Plane with ForceNewCluster

// To Do use etcdutl snapshot restore

#### Step 1C.7: Verify Recovery Host

```sh
curl http://${RECOVERY_HOST_EXTERNAL_IP}:${API_PORT}/v1/databases
```

Example response:

```json
{
  "databases": [
    {
      "id": "storefront",
      "state": "available",
      "instances": [
        { "host_id": "host-1", "node_name": "n1", "state": "unknown" },
        { "host_id": "host-2", "node_name": "n2", "state": "available" },
        { "host_id": "host-3", "node_name": "n3", "state": "unknown" }
      ]
    }
  ]
}
```

Instances on destroyed hosts show `state: "unknown"`, surviving instances show `state: "available"`.

Now proceed to [Phase 2: Remove Dead Hosts](#phase-2-remove-dead-hosts).

---

## Phase 2: Remove Dead Hosts 
// To Do we will keep one section in final doc 

After Phase 1, you have one server-mode host running. Now remove dead host records and clean up databases.

### Step 2.1: Update Databases to Remove Dead Hosts

Use the `remove_host` query parameter to remove instances from destroyed hosts:

```sh
curl -X POST "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID>?remove_host=<DEAD_HOST_1>&remove_host=<DEAD_HOST_2>" \
    -H "Content-Type: application/json" \
    -d '<updated-database-spec>'
```

Example:

```sh
curl -X POST "http://${RECOVERY_HOST_IP}:3000/v1/databases/storefront?remove_host=host-1&remove_host=host-3" \
    -H "Content-Type: application/json" \
    -d '{
        "spec": {
            "database_name": "storefront",
            "database_users": [
                {
                    "username": "admin",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "nodes": [
                { "name": "n2", "host_ids": ["host-2"] }
            ]
        }
    }'
```

**Important:** Wait for each database update task to complete before proceeding. Monitor task status using the [Tasks and Logs](../using/tasks-logs.md) documentation.

### Step 2.2: Force Remove Dead Host Records

Remove stale host records **one at a time**, waiting for each task to complete:

```sh
curl -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<DEAD_HOST_1>"
curl -X DELETE "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<DEAD_HOST_2>"
```

### Step 2.3: Verify Cleanup

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Example databases response after cleanup:

```json
{
  "databases": [
    {
      "id": "storefront",
      "state": "available",
      "instances": [
        { "host_id": "host-2", "node_name": "n2", "state": "available" }
      ]
    }
  ]
}
```

Expected:

- Only the recovery host remains in the hosts list with `etcd_mode: "server"`, `has_quorum: true`, `total_members: 1`, `started_members: 1`
- Databases show `state: "available"` with only surviving instances

---

## Phase 3: Rejoin or Provision Hosts

Determine which path applies for each host being restored:

| Condition | Path |
|-----------|------|
| Host is accessible (SSH works, Docker running, still in Swarm) | [Path A: Rejoin Existing Host](#phase-3a-rejoin-existing-host) |
| Host is destroyed (needs new infrastructure) | [Path B: Provision New Host](#phase-3b-provision-new-host) |

### Phase 3A: Rejoin Existing Host

**Use when the lost host is still reachable and part of Docker Swarm.**

#### Step 3A.1: Stop the Host Service

```bash
LOST_HOST_ID="<lost-host-id>"
docker service scale control-plane_${LOST_HOST_ID}=0
```

#### Step 3A.2: Clear Host State

SSH to the lost host:

**For server-mode hosts:**

```bash
rm -rf "${PGEDGE_DATA_DIR}/etcd"
rm -rf "${PGEDGE_DATA_DIR}/certificates"
rm -f "${PGEDGE_DATA_DIR}/generated.config.json"
```

#### Step 3A.3: Start the Host Service

```bash
docker service scale control-plane_${LOST_HOST_ID}=1
```

If Swarm no longer has the service definition:

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
```

Now proceed to [Phase 3C: Join Control Plane Cluster](#phase-3c-join-control-plane-cluster).

---

### Phase 3B: Provision New Host

**Use when the host is destroyed and must be recreated from scratch.**

#### Step 3B.1: Create New Host

Create and deploy a new host.

#### Step 3B.2: Join Docker Swarm

On the recovery host, get the Swarm join token:

```bash
docker swarm join-token manager   # for manager nodes
docker swarm join-token worker    # for worker nodes
```

On the new host:

```bash
docker swarm join --token SWMTKN-1-xxx...xxx ${RECOVERY_HOST_IP}:2377
```

Verify:

```bash
docker node ls
```

#### Step 3B.3: Deploy Control Plane Service

On the new host:

```bash
sudo mkdir -p /data/control-plane
```

On any manager node:

```bash
docker stack deploy -c <path-to-stack-yaml> control-plane
```

Verify:

```bash
docker service ps control-plane_<HOST_ID>
```

Now proceed to [Phase 3C: Join Control Plane Cluster](#phase-3c-join-control-plane-cluster).

---

### Phase 3C: Join Control Plane Cluster

This step is the same regardless of whether the host was rejoined (3A) or provisioned new (3B).

#### Step 3C.1: Get Join Token

```sh
JOIN_TOKEN="$(curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token)"
```

#### Step 3C.2: Join the Cluster

Call the join API **on the host being added** (not on an existing member):

```sh
curl -X POST http://<NEW_HOST_IP>:${API_PORT}/v1/cluster/join \
    -H 'Content-Type:application/json' \
    --data "${JOIN_TOKEN}"
```

!!! important

    The join-cluster API must be called on the host being added, not on an existing cluster member.

#### Step 3C.3: Verify Host Joined

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
```

The host should appear with `status: "reachable"` and the correct `etcd_mode`.

#### Repeat for Each Host

Repeat Phase 3 (A or B, then C) for each lost host. Recover **server-mode hosts first**, then client-mode hosts.

---

## Phase 4: Restore Database Capacity

### Step 4.1: Update Database with All Nodes

Add the restored hosts back to the database spec:

```sh
curl -X POST http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID> \
    -H 'Content-Type:application/json' \
    --data '{
        "spec": {
            "database_name": "<DB_NAME>",
            "database_users": [
                {
                    "username": "admin",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

To use a specific source node for data synchronization:

```json
{ "name": "n3", "host_ids": ["host-3"], "source_node": "n2" }
```

### Step 4.2: Monitor Database Update

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID>/tasks/<TASK_ID>
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID>/tasks/<TASK_ID>/log
```

---

## Phase 5: Final Verification

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Example databases response after full recovery:

```json
{
  "databases": [
    {
      "id": "storefront",
      "state": "available",
      "instances": [
        { "host_id": "host-1", "node_name": "n1", "state": "available" },
        { "host_id": "host-2", "node_name": "n2", "state": "available" },
        { "host_id": "host-3", "node_name": "n3", "state": "available" }
      ]
    }
  ]
}
```

Confirm:

- [ ] All hosts show `status: "reachable"`
- [ ] Server-mode hosts show `etcd_mode: "server"`
- [ ] Client-mode hosts show `etcd_mode: "client"`
- [ ] etcd health shows `has_quorum: true` with correct member count
- [ ] All databases show `state: "available"`
- [ ] All database instances show `state: "available"`
- [ ] All subscriptions show `status: "replicating"`
- [ ] Docker Swarm shows all nodes `Ready` with correct manager status
- [ ] Data replicates correctly across all nodes

---

## Common Issues

### "duplicate host ID" error

**Cause:** Host record still exists in etcd.

**Fix:** Complete Phase 2 (remove dead hosts) and wait for tasks to complete before rejoining.

### etcd certificate errors

**Cause:** Certificates don't match snapshot data.

**Fix:** Use the same certificate files that were used when the snapshot was taken.

### Quorum not restored

**Cause:** Not enough server-mode hosts rejoined.

**Fix:** Verify you've rejoined enough hosts to meet quorum threshold. Count only server-mode hosts.

### Docker Swarm commands hang

**Cause:** Swarm quorum is lost.

**Fix:** Run `docker swarm init --force-new-cluster` on the surviving manager (Phase 1C, Step 1C.1).

### "service already exists" when deploying stack

**Cause:** Manually created services conflict with the stack deployment.

**Fix:** Remove the conflicting service first (`docker service rm <service-name>`), then redeploy the stack.

### Control Plane API hangs after ForceNewCluster

**Cause:** etcd auth may not have been properly re-enabled during ForceNewCluster recovery.

**Fix:** Check service logs (`docker service logs control-plane_<HOST_ID>`). The service handles auth disable/re-enable automatically. If issues persist, restart the service.

### Image pull fails on new hosts

**Cause:** Container registry was running on a destroyed host.

**Fix:** Recreate the registry on the surviving host (Phase 1C, Step 1C.4) and ensure new hosts can reach it.

### "etcd already initialized" error

**Cause:** Stale etcd data on a host being joined.

**Fix:** Clear the data directory before joining:

```bash
rm -rf ${PGEDGE_DATA_DIR}/etcd
rm -rf ${PGEDGE_DATA_DIR}/certificates
rm -f ${PGEDGE_DATA_DIR}/generated.config.json
```

### Control Plane fails to start

**Cause:** Old etcd processes still running or conflicting state.

**Fix:**

- Stop service: `docker service scale control-plane_<host-id>=0`
- Clear host state (see Step 3A.2)
- Restart: `docker service scale control-plane_<host-id>=1`

---

## Summary

| Phase | Step | Action | Applies To |
|-------|------|--------|------------|
| 1A | 1A.1–1A.6 | Restore from snapshot, start CP | Total Quorum Loss |
| 1B | 1B.1–1B.4 | Snapshot from existing data, start CP | Majority Quorum Loss |
| 1C | 1C.1–1C.7 | Recover Swarm, registry, ForceNewCluster | etcd + Swarm Loss |
| 2 | 2.1–2.3 | Remove dead hosts and clean databases | All |
| 3A | 3A.1–3A.3 | Clear state and restart existing host | Host Accessible |
| 3B | 3B.1–3B.3 | Provision new host, join Swarm, deploy | Host Destroyed |
| 3C | 3C.1–3C.3 | Join Control Plane cluster | All |
| 4 | 4.1–4.2 | Restore database capacity | All |
| 5 | — | Final verification | All |
