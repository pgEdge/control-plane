# Container Image Management

The Control Plane manages the container images used for Postgres instances
through a version manifest - a JSON document that maps each supported
Postgres version and Spock major version to a specific, tested image tag.

When you create a database without specifying an image, the Control Plane looks
up the default image for the requested Postgres and Spock version combination
from the manifest and records the selection in the database's persisted state.
The recorded image is used for all subsequent reconciliations until you
explicitly apply an upgrade or provide an image override.

## The Version Manifest

The version manifest is the source of truth for which container images the
Control Plane uses. The manifest is embedded in the Control Plane binary as a
fallback, fetched from `downloads.pgedge.com` at startup, and cached on disk so
the Control Plane remains operational when the manifest URL is temporarily
unreachable. The resolution order for the default and custom URL modes is:

1. Remote URL (default: `downloads.pgedge.com`, configurable via
   [`docker_swarm.manifest_url`](../installation/configuration.md))
2. Disk cache (populated from a previous successful remote fetch)
3. Embedded binary (shipped with each Control Plane release)

For air-gapped deployments, `docker_swarm.manifest_path` provides a fourth
mode: the Control Plane reads a local file and skips all remote fetching.
See the [Configuration Reference](../installation/configuration.md) and
[Installing the Control Plane](../installation/swarm-installation.md) for details.

The Control Plane logs a warning when falling back to the cache or embedded
manifest. With the default manifest URL, startup always succeeds because the
embedded binary manifest serves as the final fallback. With a custom
`manifest_url`, the embedded fallback is disabled: if the URL and disk cache
are both unavailable at startup, the Control Plane returns an error.

!!! note

    The manifest is refreshed from the remote URL in the background
    approximately once per hour. A Control Plane restart is not required for
    routine manifest updates.

### Image Tag Format

pgEdge image tags follow a structured naming convention that encodes the
Postgres version, Spock version, image variant, and optional build number.
The format is:

```text
{pg_version}-spock{spock_version}-{variant}[-{build}]
```

For example: `17.9-spock5.0.6-standard-2`

The following table describes each component of the image tag format:

| Component | Description | Examples |
| :--- | :--- | :--- |
| `pg_version` | Postgres `major.minor` version | `17.9`, `18.4` |
| `spock_version` | Full Spock semver | `5.0.6`, `5.0.9` |
| `variant` | Image variant | `standard` |
| `build` | Optional build number | `1`, `2` |

### Image Stability

Every manifest entry carries a `stability` value that tells the Control Plane
how far along its release cycle the image is and what the Control Plane may do
with it. The `/v1/hosts` response reports the `stability` of each entry in
`supported_pgedge_versions` and `default_pgedge_version`.

| Value | Meaning |
| :--- | :--- |
| `dev` | A mutable channel tag. Its contents can change under a running deployment, and its declared `postgres_version` can drift from what the tag actually resolves to. For evaluation only. |
| `beta` | A feature-complete pre-release, immutable build. Behaviour may still change before general availability. |
| `rc` | A release candidate: an immutable build believed to be production quality, published to catch last regressions. Promoted to `stable` unchanged if no blocker is found. |
| `stable` | Generally available. |
| `deprecated` | Still resolvable and still supported for existing databases, but should not be chosen for a new database. |

An empty `stability` is treated as `stable`.

The value controls three things:

| Behaviour | Allowed for |
| :--- | :--- |
| Chosen automatically when a create request omits `postgres_version` / `spock_version` (the manifest default) | `stable` only |
| Listed in a database's `available_upgrades` | `stable` only |
| Accepted as the target of an explicit [image upgrade](./upgrade-db.md#image-upgrades) | `stable`, `rc`, `deprecated` |

Any non-`stable` version is opt-in: it is never selected for you. To use one,
set `postgres_version` and `spock_version` on the create request to match the
manifest entry, or pin the image directly with
[`orchestrator_opts.swarm.image`](#using-a-custom-image).

## Checking Available Images

This section explains how to query the Control Plane for available Postgres
versions and pending image upgrades.

### Supported Postgres Versions

To see which Postgres versions the Control Plane can use on each host, query
the `/v1/hosts` endpoint:

=== "curl"

    ```sh
    curl http://host-3:3000/v1/hosts
    ```

The response includes a `supported_pgedge_versions` field for each host that
lists the Postgres and Spock version combinations available on that host. Each
entry carries a `stability` value (see [Image Stability](#image-stability)).

### Available Image Upgrades

To check for available upgrades you need a running database created at a
lower image version than the current manifest default. If you created the
database without specifying an image, the manifest selected the latest image
and `available_upgrades` will be empty. Create the database with an older
`postgres_version` (for example `17.9` when `17.10` is available in the
manifest) so that an upgrade is visible. See
[Creating a Database](./create-db.md) for instructions.

To check whether a newer stable image exists in the same Postgres major and
Spock major version bucket as your running database, include
`available_upgrades` in the response:

=== "curl"

    ```sh
    curl 'http://host-3:3000/v1/databases/example?include=available_upgrades'
    ```

The `available_upgrades` field in the response lists each candidate image:

```json
{
  "available_upgrades": [
    {
      "image": "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1",
      "postgres_version": "17.10",
      "spock_version": "5"
    }
  ]
}
```

An upgrade is listed when a stable manifest entry exists in the same
`(postgres_major, spock_major)` bucket and has a strictly higher Postgres
version than the database's current image.

For instructions on applying an image upgrade, see
[Image Upgrades](./upgrade-db.md#image-upgrades).

## Using a Custom Image

You can override the manifest-selected image by setting
`orchestrator_opts.swarm.image` in your create or update request. This
override is useful for:

- pinning to a specific patch level or build, such as a particular
  `standard-N` build or a Spock patch version not yet in the manifest.
- testing pre-release images by deploying a dev build that is not in
  the manifest.
- air-gapped deployments that reference an image from a private registry.

### Per-Database Override

Set `image` at the top-level `orchestrator_opts.swarm` to apply the same image
to all nodes. The following request creates a database pinned to a specific
image:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "postgres_version": "17.9",
                "spock_version": "5",
                "orchestrator_opts": {
                    "swarm": {
                        "image": "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2"
                    }
                },
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] }
                ]
            }
        }'
    ```

### Per-Node Override

Set `image` on a specific node's `orchestrator_opts.swarm` to use a different
image on that node only. The node-level value takes precedence over the
database-level value. The following request pins node `n1` to a specific image
while `n2` uses the manifest default:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "postgres_version": "17.9",
                "spock_version": "5",
                "nodes": [
                    {
                        "name": "n1",
                        "host_ids": ["host-1"],
                        "orchestrator_opts": {
                            "swarm": {
                                "image": "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2"
                            }
                        }
                    },
                    { "name": "n2", "host_ids": ["host-2"] }
                ]
            }
        }'
    ```

### Digest-Pinned Images

You can append a digest to an image reference for byte-exact pinning. The
Control Plane strips the digest before version parsing and checks accessibility
against the digest-qualified reference.

To obtain the digest for a tag, run the following command on any host that has
pulled the image:

```sh
docker inspect ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1 \
    --format='{{index .RepoDigests 0}}'
```

The output is the fully qualified `image:tag@sha256:<digest>` string. Use that
value in `orchestrator_opts.swarm.image` when creating the database:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "postgres_version": "17.10",
                "spock_version": "5",
                "orchestrator_opts": {
                    "swarm": {
                        "image": "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1@sha256:<digest>"
                    }
                },
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] }
                ]
            }
        }'
    ```

!!! tip

    Digest pinning guarantees that you run a specific immutable image even
    if the tag is later reassigned to a different image in the registry.

### Spock 6 Preview Images

Spock 6 is available as a preview on both Docker Swarm and systemd
deployments. The preview uses Spock 6.0.0-beta2.

On Docker Swarm, the version manifest offers Spock 6 entries with
[`"stability": "beta"`](#image-stability) for Postgres 16.16, 17.12,
and 18.7. Beta entries are excluded from image upgrades and are never
chosen for a database that omits `postgres_version` and `spock_version`.
To create a new database on Spock 6, set `postgres_version` and
`spock_version` to match a manifest entry:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {
                        "username": "admin",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "postgres_version": "18.7",
                "spock_version": "6",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] },
                    { "name": "n2", "host_ids": ["host-2"] }
                ]
            }
        }'
    ```

Overriding `orchestrator_opts.swarm.image` is only necessary if you need
an image other than the manifest default for that version pair, such as
pinning a specific build. See
[Using a Custom Image](#using-a-custom-image).

On systemd, there's no version manifest or image to pin. Install the
Spock 6.0.0-beta2 packages (`pgedge-spock60_<postgres-major>` on
RPM-based distributions, `pgedge-postgresql-<postgres-major>-spock60` on
Debian/Ubuntu) from the pgEdge repositories alongside the matching
`pgedge-postgresql*` package, following the same steps as
[Installing via systemd](../installation/systemd-installation.md).
Then request the preview the same way, setting `spock_version` to
`"6"`. Unlike the Swarm manifest, systemd doesn't exclude preview
versions from a database that omits `spock_version`. A host advertises
every Spock and Postgres major version combination it has packages
for, and the highest combination becomes that host's default. Only
install the Spock 6 packages on hosts where you intend to use the
preview.

!!! warning

    Preview images are for evaluation, not production use. The Control
    Plane rejects preview images as targets for the
    [image upgrade](./upgrade-db.md#image-upgrades) action, and they are
    excluded from `available_upgrades`.

#### Upgrading a Database from Spock 5 to Spock 6

You can upgrade an existing database from Spock 5 to Spock 6 by
updating its spec and changing `spock_version` from `"5"` to `"6"`.
Keep `postgres_version` at a version that has a Spock 6 entry, such as
17.12. On systemd, install the Spock 6 packages on every host first.

The Control Plane accepts the update only when:

- every instance of the database runs Spock 5.0.14 or later; update to
  an image with Spock 5.0.14 first if needed.
- every instance is running and has recently reported its status, since
  the Control Plane uses that status to check the Spock version.
- the same update doesn't add, remove, or move nodes.
- every host supports the requested Postgres and Spock 6 version.

The Control Plane upgrades one Spock major version at a time, and it
rejects a change back to Spock 5.

During the upgrade, the Control Plane:

- pauses Spock's automatic DDL replication on every instance, and
  resumes it when the upgrade ends, including when the upgrade fails or
  is canceled.
- enables failover on existing Spock replication slots on Postgres 17
  and later, so that replicas keep copies of the slots.
- upgrades one node at a time, updating the node's replicas before its
  primary.
- checks that each node's Spock extension is up to date and that its
  subscriptions are replicating.

On Docker Swarm, the Control Plane switches over to an upgraded replica
before it restarts a node's primary. To enable failover on a slot that
a subscriber is using, the Control Plane briefly disconnects the
subscriber; the subscriber reconnects and resumes without losing
changes. The task log shows each step of the upgrade.

Spock updates its extension on each node when the node starts on the
new version; if the update hasn't happened after two minutes, the
Control Plane runs `ALTER EXTENSION spock UPDATE` itself. The extension
update can't be undone, so take a backup before you start.

If the upgrade fails, fix the cause and submit the same update again.
The Control Plane continues with the instances that still run Spock 5.
If every instance already runs Spock 6, the next update of the database
checks each node and resumes DDL replication. Until the upgrade
finishes, the Control Plane rejects updates that add or move nodes; you
can still remove a node, for example one whose host has failed.

Spock 6 also changes some replication behavior. Direct DDL against the
`spock` and `snowflake` schemas fails while DDL replication is on, and
`DELETE` operations now go through conflict resolution. Spock 6 also
renames some conflict types in `spock.resolutions`; the extension
update migrates existing rows. See the Spock release notes for details.

## Image Validation

When `orchestrator_opts.swarm.image` is set, the Control Plane validates the
image on every host before accepting the request. Validation runs in parallel
across all hosts and returns a `400 Bad Request` if any validation fails.

The Control Plane checks the following conditions:

- registry accessibility: the image reference is reachable in the registry.
  The check uses a lightweight manifest fetch; no image layers are downloaded.
- Postgres version prefix match: if the tag follows the pgEdge format
  (`{pg_version}-spock{spock_version}-{variant}`), the `pg_version` in the
  tag must start with the `postgres_version` in the database spec.
- Spock version prefix match: the `spock_version` in the tag must start with
  all components of `spock_version` in the spec. For example, a spec declaring
  `"spock_version": "5"` accepts tag versions `5`, `5.0.6`, or `5.0.9`.

Tags that do not follow the pgEdge format (for example, a custom dev tag with
no version structure) skip the version match checks - only registry
accessibility is verified.

### Validation Error Examples

The following examples show the error responses returned for common validation
failures.

The following request triggers a Postgres version mismatch error because the
spec requests `17.9` but the tag contains `17.10`. The image must exist in
the registry for version validation to run - the registry check runs first
and short-circuits if the image is not found:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {"username": "admin", "db_owner": true, "attributes": ["SUPERUSER", "LOGIN"]}
                ],
                "postgres_version": "17.9",
                "spock_version": "5",
                "orchestrator_opts": {
                    "swarm": {
                        "image": "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1"
                    }
                },
                "nodes": [{"name": "n1", "host_ids": ["host-1"]}]
            }
        }'
    ```

The Control Plane returns a `400 Bad Request` with the following body:

```json
{
  "message": "validation error for node n1, host host-1: image tag postgres version 17.10 does not match spec version 17.9",
  "name": "invalid_input"
}
```

The following request triggers an image-not-found error because the tag does
not exist in the registry:

=== "curl"

    ```sh
    curl -X POST http://host-3:3000/v1/databases \
        -H 'Content-Type:application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "database_users": [
                    {"username": "admin", "db_owner": true, "attributes": ["SUPERUSER", "LOGIN"]}
                ],
                "postgres_version": "17.9",
                "spock_version": "5",
                "orchestrator_opts": {
                    "swarm": {
                        "image": "ghcr.io/pgedge/pgedge-postgres:17.9-spock4.0.0-standard-1"
                    }
                },
                "nodes": [{"name": "n1", "host_ids": ["host-1"]}]
            }
        }'
    ```

The Control Plane returns a `400 Bad Request` with the following body:

```json
{
  "message": "validation error for node n1, host host-1: image \"ghcr.io/pgedge/pgedge-postgres:17.9-spock4.0.0-standard-1\" could not be verified: image \"ghcr.io/pgedge/pgedge-postgres:17.9-spock4.0.0-standard-1\" not found or inaccessible: Error response from daemon: manifest unknown",
  "name": "invalid_input"
}
```

When the registry is unreachable due to a network error, the Control Plane
returns the following error:

```json
{
  "message": "validation error for node n1, host host-1: image \"ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2\" could not be verified: context deadline exceeded",
  "name": "invalid_input"
}
```

!!! warning

    Registry reachability checks add latency to create and update requests. In
    environments where the registry is unavailable from all hosts (such as a
    fully air-gapped deployment), use `manifest_path` to load images from a
    local manifest and ensure all referenced images are pre-pulled on each
    host.

## Image Persistence

Once an image is selected - either from the manifest or from an
`orchestrator_opts.swarm.image` override - the Control Plane records the
selection in the database's persisted state as the `resolved_image`. The
Control Plane uses this recorded value on every subsequent reconciliation.

A database's image does not change automatically across Control Plane upgrades.
The Control Plane updates the recorded `resolved_image` only when you:

- Apply an explicit image upgrade via `POST /v1/databases/{id}/upgrade`
  (see [Upgrading a Database](./upgrade-db.md)).
- Update the database spec with a different `postgres_version`,
  `spock_version`, or `orchestrator_opts.swarm.image`.

!!! note

    If you update a database image directly via `docker service update` outside
    the Control Plane, the change will be overwritten on the next
    reconciliation. The Control Plane is the authoritative source of truth for
    Docker service configuration.

## Next Steps

The following documents provide additional context for managing container
images in the Control Plane:

- The [Upgrading a Database](./upgrade-db.md) document explains how to apply
  image upgrades and perform minor and major Postgres version upgrades.
- The [Configuration Reference](../installation/configuration.md) document
  describes the `docker_swarm.manifest_url` and `docker_swarm.manifest_path`
  settings used to customize manifest resolution.
- The [Creating a Database](./create-db.md) document explains how to create
  a database and specify image options at creation time.
