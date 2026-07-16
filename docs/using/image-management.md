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
[Installing the Control Plane](../installation/installation.md) for details.

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
lists the Postgres and Spock version combinations available on that host.

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
