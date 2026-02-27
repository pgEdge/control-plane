# Disaster Recovery

This page describes how to recover the Control Plane and Docker Swarm after
loss of a host or etcd quorum. Follow the sections in order, skipping:

- [Restoring Docker Swarm](#restoring-docker-swarm) if the Swarm still has
  quorum.
- [Restoring the Control Plane](#restoring-the-control-plane) if the API is
  already accessible.

The following table will direct you to your recovery starting point; find
the `Scenario` that describes the condition of your Swarm, and navigate to
the corresponding section in the `Start at` column to begin:

| Scenario | API accessible? | Start at |
|----------|------------------|----------|
| Quorum intact — one or more hosts lost | Yes | [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts) |
| etcd quorum lost — Swarm still works | No | [Restoring the Control Plane](#restoring-the-control-plane) |
| etcd and Swarm quorum lost | No | [Restoring Docker Swarm](#restoring-docker-swarm), then [Restoring the Control Plane](#restoring-the-control-plane) |

!!! warning

    Ensure you have a recent backup of the Control Plane data volume before
    starting any recovery procedure.

    
## Prerequisites

Before starting the recovery process, ensure you have:

- the host ID(s) of the lost host(s).
- SSH access to remaining cluster hosts (for Docker Swarm and host
  operations).
- the Control Plane stack definition file (YAML) from your initial
  deployment.
- a current backup.  If **every** server-mode host was lost (total etcd
  loss), you need a backup of the Control Plane data volume and
  (optionally) an etcd snapshot file. If at least one server-mode host
  remains, you can recover using that host's existing data; a backup is not
  required.

See [Creating the stack definition file](../installation/installation.md#creating-the-stack-definition-file)
in the installation documentation.

## Set Variables

Set the following variables for all recovery steps. If the API is already
accessible, use a healthy host for the `RECOVERY_HOST_IP` and the host
being recovered (or the first restored host) for `RECOVERY_HOST_ID`.

```bash
PGEDGE_DATA_DIR="<path-to-control-plane-data-dir>"
RECOVERY_HOST_ID="<recovery-host-id>"
RECOVERY_HOST_IP="<recovery-host-ip>"
API_PORT=<api-port>
ETCD_CLIENT_PORT=<etcd-client-port>
ETCD_PEER_PORT=<etcd-peer-port>
```

!!! note "Pre-created etcd snapshot is optional"

    The procedure below restores etcd from the **existing data directory**
    on the recovery host (after moving it aside). Use a pre-created
    snapshot file only when that data is corrupted.

### Quorum Reference

Use the following formula and table to determine if quorum has been
lost:

**Formula:** `Quorum = floor(N/2) + 1`, where N = number of
server-mode hosts.

| Server-mode hosts | Quorum | Quorum lost when |
|-------------------|--------|------------------|
| 3 | 2 | 2 or more hosts lost |
| 5 | 3 | 3 or more hosts lost |


## Data Volume Restore

You will need to restore from a previously created backup if you
have lost 100% of your Control Plane servers configured to serve etcd.
This could be a snapshot of the data volume or any other type of
backup that includes the Control Plane data directory for one of the
lost servers. Only one Control Plane server backup is needed to
restore the cluster.

If you have lost 100% of your database instances, you will need the
data directory from at least one instance from each database. Your
data volume backup will also include this data if you are restoring
a host that was running an instance of each database. If not, you
will also need to restore at least one more host that has this
instance data.

If you do not have any data volume backups that include your
database instances, we recommend creating a new Control Plane
cluster and restoring your databases from pgBackRest backups
instead. See [Creating a new database from a backup](../using/backup-restore.md#creating-a-new-database-from-a-backup)
for more information.

---

## Restoring Docker Swarm

Use the steps in this section when Docker Swarm has lost quorum (if a
majority of managers are gone). If your Swarm still has a quorum, go to
[Restoring the Control Plane](#restoring-the-control-plane).

1.  Reinitialize the Swarm; on a surviving manager, invoke the
    following command:

    ```bash
    docker swarm init --force-new-cluster \
        --advertise-addr ${RECOVERY_HOST_IP}
    ```

    Verify:

    ```bash
    docker node ls
    ```

2.  Join Hosts to the New Swarm.  If you have other surviving nodes
    that should be part of the new Swarm, attach them now. On the
    manager, get the join token:

    ```bash
    docker swarm join-token manager   # for manager nodes
    docker swarm join-token worker    # for worker nodes
    ```

    On each node to add, run:

    ```bash
    docker swarm join --token SWMTKN-1-xxx...xxx \
        ${RECOVERY_HOST_IP}:2377
    ```

    Use the command, `docker node ls` to verify the swarm on the manager.

3.  Removing Old Swarm Nodes - first, demote lost managers, then remove
    the lost nodes:

    ```bash
    docker node demote <LOST_HOSTNAME_1> <LOST_HOSTNAME_2>
    docker node rm --force <LOST_HOSTNAME_1> <LOST_HOSTNAME_2>
    ```

    Remove Control Plane and Postgres services that were pinned to the lost
    nodes:

    ```bash
    docker service rm control-plane_<LOST_HOST_ID_1> \
        control-plane_<LOST_HOST_ID_2>
    docker service ls
    docker service rm <orphaned-postgres-service-1> \
        <orphaned-postgres-service-2>
    ```

    If the container registry or Control Plane image resided on a lost
    host, recreate the registry and image on a surviving host before
    starting the Control Plane; for details, see
    [Upgrading the Control Plane](../installation/upgrading.md).

---

## Restoring the Control Plane

Perform the steps in this section only if the etcd quorum was lost, and
the Control Plane API is unavailable. If the API is still accessible, go
to [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts).

Use one server-mode host as the recovery host; what you do first depends
on your Swarm.  If:

- all server-mode hosts were offline: On a Swarm manager, stop all
  Control Plane services with the command:
  `docker service scale control-plane_<host-id-1>=0 control-plane_<host-id-2>=0 ...`
  Then, restore the data volume from your backup (see
  [Data Volume Restore](#data-volume-restore)).
- at least one server-mode host was still up: Use that host as the
  recovery host. On a Swarm manager, stop the Control Plane service on
  that host so the following steps do not move live etcd data:
  `docker service scale control-plane_<recovery-host-id>=0`. You do not
  need to restore the volume first.
- you already completed [Restoring Docker Swarm](#restoring-docker-swarm):
  Restore the data volume on the surviving host if it was lost (see
  [Data volume restore](#data-volume-restore)); otherwise skip.

Then on the recovery host, perform the following steps:

1.  Back up existing etcd data and set aside for restore:

    ```bash
    if [ -d "${PGEDGE_DATA_DIR}/etcd" ]; then
        ETCD_BACKUP_DIR="${PGEDGE_DATA_DIR}/etcd.backup.$(date +%s)"
        mv "${PGEDGE_DATA_DIR}/etcd" "${ETCD_BACKUP_DIR}"
    fi
    if [ -d "${PGEDGE_DATA_DIR}/certificates" ]; then
        cp -r "${PGEDGE_DATA_DIR}/certificates" \
            "${PGEDGE_DATA_DIR}/certificates.backup.$(date +%s)"
    fi
    if [ -f "${PGEDGE_DATA_DIR}/generated.config.json" ]; then
        cp "${PGEDGE_DATA_DIR}/generated.config.json" \
            "${PGEDGE_DATA_DIR}/generated.config.json.backup.$(date +%s)"
    fi
    ```

2.  Install etcdutl:

    ```bash
    ETCD_VERSION="v3.6.8"
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; \
        elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
    curl -L \
        "https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-${ARCH}.tar.gz" \
        | tar --strip-components 1 -xz -C /tmp \
            "etcd-${ETCD_VERSION}-linux-${ARCH}/etcdutl"
    sudo mv /tmp/etcdutl /usr/local/bin/ && \
        sudo chmod +x /usr/local/bin/etcdutl
    ```

3.  Restore etcd from the backup directory (Step 1 sets the value for
    `ETCD_BACKUP_DIR`). If you have no existing etcd directory and are
    using a snapshot file instead, use that file path in place of
    `"${ETCD_BACKUP_DIR}/member/snap/db"`. This will restore quorum by
    reinitializing etcd with a single cluster member:

    ```bash
    etcdutl snapshot restore "${ETCD_BACKUP_DIR}/member/snap/db" \
        --name "${RECOVERY_HOST_ID}" \
        --initial-cluster "${RECOVERY_HOST_ID}=https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
        --initial-advertise-peer-urls "https://${RECOVERY_HOST_IP}:${ETCD_PEER_PORT}" \
        --skip-hash-check \
        --data-dir "${PGEDGE_DATA_DIR}/etcd"
    ls -la "${PGEDGE_DATA_DIR}/etcd"
    ```

4.  Start the Control Plane and verify:

    - If Control Plane is already deployed as Swarm services (if the etcd
      quorum was lost and you did not run
      [Restoring Docker Swarm](#restoring-docker-swarm)):
      `docker service scale control-plane_${RECOVERY_HOST_ID}=1`

    - If you completed [Restoring Docker Swarm](#restoring-docker-swarm)
      and deploy with the `stack` command:
      `docker stack deploy -c <path-to-stack-yaml> control-plane`
      Without setting `PGEDGE_ETCD_SERVER__FORCE_NEW_CLUSTER`.

    Then:

    ```bash
    docker service ps control-plane_${RECOVERY_HOST_ID} --no-trunc
    curl -sS "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts"
    ```

    You should see one host with `status: "reachable"` and
    `etcd_mode: "server"`. Then continue with the next section:
    [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts).

## Updating Databases to Remove Old Hosts

For each database that has instances on a lost host, submit an update
with the `remove_host` query parameter and a spec that excludes that
host. Wait for each update task to complete.

```sh
curl -X POST \
    "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID>?remove_host=<LOST_HOST_ID>" \
    -H "Content-Type: application/json" \
    -d '{
        "spec": {
            "database_name": "<DB_NAME>",
            "database_users": [{"username": "admin", "db_owner": true,
                                "attributes": ["SUPERUSER", "LOGIN"]}],
            "port": 5432,
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] }
            ]
        }
    }'
```

For multiple lost hosts, add `&remove_host=<HOST_ID>` for each. Monitor
progress via [Tasks and Logs](../using/tasks-logs.md) in the Using Control
Plane documentation.

### Removing Old Hosts

After all affected databases have been updated, remove each lost host
from the Control Plane (one at a time; wait for each removal task to
complete):

```sh
curl -X DELETE \
    "http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>"
```

To monitor the progress:

`curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts/<LOST_HOST_ID>/tasks/<TASK_ID>`

On a healthy Swarm manager, remove each lost node from the Swarm:

```bash
docker node ls
# If the lost node was a manager:
docker node demote <LOST_HOSTNAME>
docker node rm <LOST_HOSTNAME> --force
```

Use the following commands to confirm only remaining hosts are listed and
databases are in a good state:

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Hosts should show only remaining hosts with `status: "reachable"`.
Databases should show `state: "available"` with instances only on
remaining hosts.

### Re-adding Hosts

For each host you need to restore, either rejoin an existing host (still
in the Swarm and reachable) or provision a new host, and then join the
Control Plane cluster.

**To rejoin an existing host (host still accessible)** — when the lost
host is reachable via SSH and Docker is running:

1.  On a manager, invoke:
    `docker service scale control-plane_<LOST_HOST_ID>=0`
2.  On that host (with SSH), clear the state:
    - Server-mode:
      `rm -rf "${PGEDGE_DATA_DIR}/etcd" "${PGEDGE_DATA_DIR}/certificates" ; rm -f "${PGEDGE_DATA_DIR}/generated.config.json"`
    - Client-mode:
      `rm -f "${PGEDGE_DATA_DIR}/generated.config.json"`
3.  On a manager, invoke:
    `docker service scale control-plane_<LOST_HOST_ID>=1` (or
    `docker stack deploy -c <path-to-stack-yaml> control-plane` if the
    service was removed).

**To provision a new host because the host was destroyed** — when the
host must be recreated:

1.  Create the new host and install prerequisites (Docker, etc.) per your
    environment.
2.  On the new host, join Docker Swarm (obtain the token on a manager
    with `docker swarm join-token manager` or `worker`):
    `docker swarm join --token SWMTKN-1-xxx...xxx \
     ${RECOVERY_HOST_IP}:2377`. Verify with `docker node ls` on the
    manager.
3.  On the new host: `sudo mkdir -p /data/control-plane` (or your
    `PGEDGE_DATA_DIR`).
4.  On a manager:
    `docker stack deploy -c <path-to-stack-yaml> control-plane`.
    Verify with `docker service ps control-plane_<HOST_ID>`.

**To join the Control Plane cluster** (for each host re-added, whether
rejoined or new):

1.  From any existing member:
    `JOIN_TOKEN="$(curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/cluster/join-token)"`
2.  On the host being added (not on an existing member):
    `curl -X POST http://<NEW_HOST_IP>:${API_PORT}/v1/cluster/join -H 'Content-Type:application/json' --data "${JOIN_TOKEN}"`
3.  Verify: `curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts` — the
    host should show `status: "reachable"` and the correct `etcd_mode`.

Repeat for each host. Re-add **server-mode hosts first**, then
client-mode hosts.

### Updating Databases to Re-add Hosts

After hosts are back in the cluster, add them to each database spec so
the Control Plane can create instances and sync data:

```sh
curl -X POST \
    http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases/<DB_ID> \
    -H 'Content-Type:application/json' \
    --data '{
        "spec": {
            "database_name": "<DB_NAME>",
            "database_users": [{"username": "admin", "db_owner": true,
                                "attributes": ["SUPERUSER", "LOGIN"]}],
            "port": 5432,
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

To choose a specific source node for data sync, add `"source_node":"n2"`
to the node entry. Monitor progress via [Tasks and Logs](../using/tasks-logs.md)
in the Using Control Plane documentation.

**Final verification:**

Run the following commands to verify the cluster state:

```sh
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/hosts
curl http://${RECOVERY_HOST_IP}:${API_PORT}/v1/databases
```

Confirm: all hosts have `status: "reachable"`; server-mode hosts have
`etcd_mode: "server"`; etcd health shows `has_quorum: true` with the
expected member count; all databases have `state: "available"` with
instances on all hosts; all subscriptions have `status: "replicating"`;
Docker Swarm reports all nodes as `Ready`; data is replicating correctly
across nodes.

---

## Common Issues

| Issue | Cause | Fix |
|-------|-------|-----|
| "duplicate host ID" when rejoining | Host record still in etcd | Complete [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts) and [Removing Old Hosts](#removing-old-hosts) for that host and wait for the removal task to complete before rejoining. |
| Host joins but shows unreachable | Network or service not fully up | Check `docker service logs control-plane_<HOST_ID>`, verify connectivity, allow service to finish initializing. |
| etcd certificate errors | Certificates do not match restored data | Use the same certificate files that were in use when the snapshot or data was created. |
| Quorum not restored | Too few server-mode hosts rejoined | Rejoin enough server-mode hosts to reach quorum (e.g. 2 of 3 for a 3-node cluster). |
| Docker Swarm commands hang | Swarm has lost quorum | Run [Reinitializing the Swarm](#restoring-docker-swarm) on a surviving manager. |
| "service already exists" when deploying stack | Manually created service conflicts with stack | Run `docker service rm <service-name>`, then redeploy the stack. |
| "etcd already initialized" | Stale etcd data on host being joined | Clear the data directory on that host before joining (see [Re-adding Hosts](#re-adding-hosts), rejoin step 2). |
| Control Plane fails to start | Stale etcd processes or conflicting state | Stop the service (`docker service scale control-plane_<host-id>=0`), clear host state (etcd, certificates, generated.config.json), then start again. |
| Database instances do not restore | Database spec does not include recovered host | [Updating Databases to Re-add Hosts](#updating-databases-to-re-add-hosts) for that database. |

---

## In Summary

If you encounter issues with your Swarm, use the following table to find a
starting point for disaster recovery:

| Section | When to run |
|---------|-------------|
| [Restoring Docker Swarm](#restoring-docker-swarm) | Run this section only when Swarm quorum was lost. |
| [Restoring the Control Plane](#restoring-the-control-plane) | Run this section only when etcd quorum was lost; otherwise start at [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts). |
| [Updating Databases to Remove Old Hosts](#updating-databases-to-remove-old-hosts) | Always run this section, once the API is available. |
| [Removing Old Hosts](#removing-old-hosts) | Always run this section, after updating databases. |
| [Re-adding Hosts](#re-adding-hosts) | Run this section for each host to restore. |
| [Updating Databases to Re-add Hosts](#updating-databases-to-re-add-hosts) | Run this section after re-adding hosts. |
