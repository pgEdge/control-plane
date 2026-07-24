# Changelog

## v0.10.0 - 2026-07-24

### Added

- The Control Plane now rejects database specs that remove `spock` from `postgresql_conf.shared_preload_libraries`, preventing accidental disabling of the replication extension.
- Added image upgrade support. Use `GET /v1/databases/{id}?include=available_upgrades` to list newer stable images within the same Postgres major and Spock major version bucket, and `POST /v1/databases/{id}/upgrade` to apply an upgrade without changing the Postgres major version.
- Added support for custom container image pinning per database or node via `orchestrator_opts.swarm.image`. pgEdge-formatted image tags are validated against the spec's declared versions; unrecognized tags are accepted without version checks. The Control Plane also persists the resolved image at creation time, preventing silent image changes during Control Plane upgrades.
- Version resolution is now fully manifest-driven. Supported versions update with manifest refreshes without requiring a Control Plane upgrade.
- The Control Plane now falls back to the first available IPv6 address when no IPv4 address is found during IP autodetection.
- Cancelling a task now propagates to any in-progress activities, interrupting long-running operations rather than waiting for them to complete.
- RPM and DEB packages are now published to the pgEdge dnf and apt repositories on every release, enabling installation via `dnf` and `apt-get` in addition to the existing Docker and binary distribution methods.
- Added support for Spock 5.0.10 on Postgres 16.14, 17.10, and 18.4.

### Changed

- **Breaking:** Changed the default `password_encryption` from `md5` to `scram-sha-256`. If you're using the default password encryption, every role in a database's `database_users` will be automatically updated to `scram-sha-256` the next time you update that database. Roles created outside of `database_users` will need their passwords updated manually. To keep using `md5`, set `postgresql_conf.password_encryption` to `md5` in your database spec.

### Fixed

- Fixed an issue where resources pending recreation were not correctly planned for creation in the right reconciliation phase, causing reconciliation errors.
- Fixed Patroni rejecting connections from IPv4 addresses presented as IPv4-mapped IPv6 addresses (e.g. `::ffff:192.168.1.1`) on dual-stack hosts.
- Fixed rolling updates failing on multi-node databases with replica instances due to replication slot timing during instance initialization.
- Fixed an issue where services configured with `"version": "latest"` failed at runtime because `"latest"` was not registered in the version map.

### Security

- Update vulnerable dependencies (pgx v5.10.0, OpenTelemetry v1.44.0, golang.org/x/crypto v0.54.0, golang.org/x/net v0.57.0) and bump the Go toolchain to 1.26.5 to remediate Trivy-flagged CVEs in the control-plane image (PLAT-686). github.com/docker/docker CVE-2026-41567/CVE-2026-42306 have no fix in the v27.x line in use; risk-accepted in .trivy/pgedge-control-plane.trivyignore.yaml since the vulnerable docker-cp/archive-copy code paths are never invoked by this codebase.
- Fixed a SQL injection vulnerability in role attribute handling during database creation and updates.

## v0.9.0 - 2026-06-16

### Added

- Added a feature to enable manual Postgres minor version updates in systemd clusters. The Control Plane will now update its copy of the database spec when it detects changes to an instance's Postgres or Spock version.
- Added post-update and pre-remove scriptlets to our RPM and deb packages to automatically restart the Control Plane service during upgrades and to stop and disable the service during uninstallation.
- Added support for Debian-based distributions to the systemd orchestrator.
- Added deb packages to our releases for Debian-based distributions.
- Added the ability to configure a separate override for the database owner GID to complement the existing UID override.
- Added support for Spock 5.0.9 on Postgres 16.14, 17.10, and 18.4.
- Added `pg_hba_conf` and `pg_ident_conf` fields to the database spec — Operators can now supply custom `pg_hba.conf` and `pg_ident.conf` entries at the database or per-node level, giving full control over client authentication rules and ident mappings. Per-node entries take first-match priority over database-level entries.
- Added Knowledge Base search support to the MCP service — Enable the `search_knowledgebase` tool via the `kb_enabled` option in the MCP service configuration, with support for OpenAI and Voyage AI embedding providers. The Knowledge Base file is loaded from a configurable host path and mounted read-only into the container.

### Changed

- Changed the instance monitoring system to query the Postgres and Spock versions for replica instances and report them in the databases API.

### Fixed

- Fixed global Patroni parameters not being applied to running database clusters. Dynamic configuration changes are now patched directly via the Patroni API on the primary instance, ensuring parameters that cannot be changed via config file reload take effect immediately.
- Fixed valid instance IDs being incorrectly rejected by the API when the database ID was at its maximum allowed length — Instance IDs are an internal aggregate of the database ID, node name, and host hash, and are no longer subject to the same length limit as user-supplied identifiers.
- Database specs with duplicate allocated ports on a single host are now rejected by the API.

## v0.8.1 - 2026-05-19

### Added

- Added support for Spock 5.0.8 on Postgres 16.14, 17.10, and 18.4.

### Fixed

- Fixed add-node data safety by ensuring replication slot and origin are correctly advanced before the new node joins.

## v0.8.0 - 2026-05-06

### Added

- Renamed the server binary from `control-plane` to `pgedge-control-plane` to reduce conflicts with other system packages.
- Added PostgREST as a supported service type — Deploy the PostgREST REST API server alongside your database with automatic credential provisioning, upfront schema and role validation, and configurable connection pool settings.
- Added preliminary support for systemd as an alternative to Docker Swarm. This feature is currently in "preview" status. You can read more about it in the [systemd page](https://docs.pgedge.com/control-plane/installation/systemd) of our docs.
- Added the ability to run user-defined SQL scripts during database creation via the `scripts` field on the database spec.
- Added `connect_as` field for service credentials — Services can now explicitly specify which database user they authenticate as by referencing a `database_users` entry, replacing auto-generated service accounts with direct, auditable credential assignment.
- Added automatic role transfer when expanding a database cluster — PostgreSQL roles created outside the standard `database_users` configuration are now automatically transferred to new nodes when they join a database.
- Extended stable random port assignments to service instances — Ports assigned to MCP, PostgREST, and RAG services are now persisted and reused across restarts and database updates, consistent with the behaviour already in place for database instances.
- Added RAG as a supported service type — Deploy a retrieval-augmented generation server alongside your database with hybrid vector and keyword search, automatic credential provisioning, and support for OpenAI, Voyage AI, Anthropic, and Ollama providers.

### Changed

- **Breaking:** The `connect_as` field is now required when creating or updating services of any type (MCP, PostgREST, RAG) — requests that omit this field will be rejected with a validation error.
- **Breaking:** Database, host, cluster, and service identifiers are now validated to comply with RFC 1035 name requirements — IDs must be 1–36 characters, contain only lowercase letters, digits, and hyphens, and start and end with a letter or digit. The combined length of a database ID and service ID may not exceed 53 characters.
- Removed the `pgedge_application` and `pgedge_application_read_only` built-in database roles — These roles are no longer created for new databases. The names are no longer reserved and may be used freely for custom database users.
- Promoted Supporting Services from beta to generally available
- Enable Patroni's failsafe mode in single-host nodes to improve resilience in some Etcd outages. Failsafe mode is not enabled in nodes with more than one host.

### Fixed

- Fixed port conflicts between services on the same host producing opaque deployment errors — Port conflicts are now detected at creation time and rejected with a clear validation message.
- Fixed `extra_networks` specified in `orchestrator_opts` not being attached to service containers (MCP, PostgREST, RAG).
- Fixed upgrade path from v0.6.2 — Databases created before v0.7.0 that were missing replication slot resources are now automatically repaired during state migration.
- Fixed embedded etcd clients connecting to all cluster members instead of only their own endpoint — This could cause connectivity issues when cluster membership changed.

## v0.7.0 - 2026-03-25

### Added

- Supporting services (beta) — Deploy supporting services alongside databases. This release includes the pgEdge Postgres MCP Server, with automatic database credential provisioning, high-availability connection routing, and declarative configuration.
- Added `patroni_port` to the database and instance APIs.
- Added ability to configure per-component log levels.
- Added a default value for the `host_id` configuration setting. It will now default to the short hostname (`hostname -s`) of the host machine.
- Added guided walkthrough with GitHub Codespaces support.
- Added support for Postgres 16.13, 17.9, and 18.3. Default version is now 18.3.
- Added stop/start instance operations to the client library.
- Etcd mode reconfiguration — Hosts can now switch between etcd server and client modes at runtime, with support for host identity changes via peer URL updates.
- Scoped tasks and new task endpoints — Tasks are now scoped to databases and hosts, with new list/get endpoints for better observability. The `remove-host` API now returns a task for tracking removal progress.
- Stable random port assignments — Randomly assigned ports now persist across instance restarts and database updates.

### Changed

- Improved disaster recovery — Better quorum loss handling, host removal resilience, and crash recovery.
- **Breaking:** Replaced `hostname` and `ipv4_address` fields in the API with `peer_addresses` and `client_addresses` for host endpoints and `addresses` for instance and service instance endpoints.
- **Breaking:** Replaced `server_url` (string) with `server_urls` (string array) in the cluster join token, allowing multiple server URLs when joining a cluster.

### Fixed

- Fixed a bug that prevented database deletion when we failed to create the Swarm service.
- Replaced fixed 100-second sync wait when adding a node with configurable health-based polling.
- Fixed sync event refresh blocking updates when Spock node is not configured.
- Fixed panics during task and workflow cancellation.
- Fixed add-node failing silently when Spock sync event is not confirmed.
- Fixed incomplete server shutdown that could leave the workflow engine running without required services.
- Fixed stale resource state blocking updates.
- Fixed IPAM subnet exhaustion after repeated database create/delete cycles by releasing subnets on network deletion.

## v0.6.2 - 2025-12-22

### Removed

- Removed unused "tenant_id" field from GetCluster endpoint.

### Fixed

- Fixed in-place restores where the recovery target is not the latest point in the repository.

## v0.6.1 - 2025-12-19

### Fixed

- Fixed incorrect pgbackrest command format for database restore endpoint.
- Fixed task logs API to return a `last_entry_id` even if `after_entry_id` is the last entry.

## v0.6.0 - 2025-12-17

### Added

- "remove-host" now has a "force" attribute that allows users to recover from the loss of one or more hosts.
- Added support for new extensions in standard pgEdge Enterprise Postgres image, including pgedge_vectorizer, pg_tokenizer, vchord_bm25, pg_vectorize, pgmq, pg_cron, and pg_stat_monitor.

### Fixed

- Added validation to the "get-join-options" endpoint to ensure incoming host IDs are unique within the cluster.

## v0.5.1 - 2025-11-24

### Added

- Added support for Postgres 16.11, 17.7, and 18.1.

### Changed

- Default Postgres version to 18.1.

### Removed

- Removed unsupported Postgres 17.5.

### Fixed

- Fixed a bug where database instance state would not be properly set to unknown when status updates were missed for two consecutive monitor intervals.
- Fixed a bug where port validation would prevent databases from updating.

## v0.5.0 - 2025-11-04

### Added

- Added support for Postgres 18.0.
- Added a new "client-only" Etcd mode to enable larger clusters and clusters with an even number of hosts.
- Added access logging.
- Added ability to run the Control Plane on Docker Swarm worker nodes.

### Changed

- Moved the cluster ID configuration setting to be an optional parameter on the init-cluster endpoint.
- Changed the default behavior when adding a node. Instead of initializing to an empty state, new nodes will always be populated from an existing node unless the new node has a `restore_config`.
- Changed the shape of the return type for `list-hosts`, `restart-instance`, `stop-instance`, and `start-instance`.
- Renamed Etcd server and client configuration options.

### Removed

- Removed cohort ID from host API endpoints.

### Fixed

- Fixed unknown host status and missing component status in host API endpoints.
- Fixed a bug that prevented users from using Service Accounts for pgBackRest credentials in GCS.
- Fixed missing replication sets after restoring from backup.
- Fixed incorrect response in `update-database` when a non-existent host ID is specified.
- Fixed a bug with scheduled instance restarts.

## v0.4.0 - 2025-10-06

### Added

- Introduced stop-instance and start-instance APIs to allow users to manually trigger a stop/start of a specific Postgres instance.
- Added support for adding new database nodes with zero downtime.
- Added stopped state for instances
- Added switchover support via the control plane API
- Added a "cancel database task" API endpoint
- Added validation to update-database to reject requests that update the database name.
- Added support for mTLS to the Control Plane API via user-managed certificates.
- Implemented the "get host" and "get cluster" endpoints.
- Added Failover support via the control plane API

### Changed

- Switched to the new images from github.com/pgEdge/postgres-images.
- Added the postgres minor version to the `postgres_version` fields in the database and host APIs.
- Changed the database creation behavior so that the first host in `host_ids` gets the primary instance for a node.
- Database update process so that nodes are processed one at a time. Within a node, replicas are always updated before the primary.
- Added automated switchovers before and after an instance is restarted as part of an update.
- Added validation to the update database API to reject requests that update the Postgres major version.
- Changed MQTT interface in client library to take an endpoint. This removes the need to generate unique client IDs per server. Callers are responsible for calling `Connect()` and `Disconnect()` on the endpoint.
- Enable fast basebackup for new nodes. This noticeably speeds up the creation process for a node with replicas.
- Changed patroni configuration to use pg_rewind for faster recovery after a switchover.

### Fixed

- Fixed join cluster timeouts when requests were submitted to a member other than the raft leader.
- Ensure that join and init cluster calls only return once the server is ready to take requests. Any errors during the initialization process will now be returned to callers.

## v0.3.0 - 2025-08-19

### Added

- Added ability to override 'database modifiable state' check via `force` parameters on several endpoints.
- Added a `client` package that wraps the generated client code in a friendlier interface.

### Changed

- Merged separate Go modules into single top-level `github.com/pgEdge/control-plane` module.

### Fixed

- Fixed high CPU usage from Etcd after recovering from some network partition scenarios.
- Fixed error in restore database workflow when the request is submitted to a host that is not running the target instance.
- Fixed client-side validation errors from missing enum values in our generated client code.
- Fixed timing issue where a new database operation could not be started immediately after the task was marked as completed.
- Fixed a bug in the restore database workflow where, sometimes, the restore would start before Postgres had finished shutting down.

## v0.2.0 - 2025-07-22

### Added

- Tasks and task logs for every database operation.
- `parent_id`, `node_name`, `host_id`, and `instance_id` fields to task API entities.
- Support for performing database backups to a mounted file system using the posix repository type via pgBackRest.
- Enabled volume validation during database creation and update to verify that specified extra_volumes paths exist and are accessible.
- APIError type for simpler, more consistent API errors.
- Summaries and groups to API endpoints in OpenAPI spec.
- Introduced restart-instance API to allow users to manually trigger a restart of a specific Postgres instance.
- Added support for attaching database containers to additional Docker networks to enable routing via Traefik and other custom setups.
- Added ability to apply arbitrary labels to the database service/container.
- Add support to disable direct database port exposure on the host in Swarm deployments.
- `remove-host` endpoint.
- Added scheduling functionality in the Control Plane.

### Changed

- Create, update, delete, and restore API responses to include task information.
- `initiate-database-backup` operation name to `backup-database-node`
- Task logs entries from log lines to structured log entries.
- Renamed `inspect` endpoints to `get`: `get-cluster`, `get-host`, `get-database-task`.
- Introduced restore_options as a structured map, and renamed extra_options to backup_options for consistency.
- Moved existing API endpoints under a `/v1` prefix.
- Allow human-readable IDs in all user-specified ID fields, including `cluster_id`, `host_id`, `database_id` and backup repository IDs.
- Censor sensitive fields, such as user passwords or cloud credentials, in all API responses. Likewise, these fields can be omitted from update requests if they're not being modified.
- Updated `pgedge` images to 5.0.0-1
- Moved pgEdge and Control Plane images to ghcr.io
- `control-plane` base image from `scratch` to `gcr.io/distroless/static-debian12` for CA certificates

### Removed

- Unused fields from API specification.

### Fixed

- Delay when resuming workflows after a restart.
- Database operation errors when instance IPs change after restarting.
- Method to determine default IPv4 address to always return IPv4.

## v0.1.0 - 2025-05-28

### Added

- Release process to publish Docker images for the Control Plane server.
