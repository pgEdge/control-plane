# Changelog
## v0.5.1 - 2025-11-24
### Added
- Added support for Postgres 16.11, 17.7, and 18.1.
### Changed
- Default Postgres version to 18.1.
### Removed
- Removed unsupported Postgres 17.5.## v0.5.0 - 2025-11-04
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
- Fixed a bug with scheduled instance restarts.## v0.4.0 - 2025-10-06
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
- Ensure that join and init cluster calls only return once the server is ready to take requests. Any errors during the initialization process will now be returned to callers.## v0.3.0 - 2025-08-19
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
- Fixed a bug in the restore database workflow where, sometimes, the restore would start before Postgres had finished shutting down.## v0.2.0 - 2025-07-22
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
- Method to determine default IPv4 address to always return IPv4.## v0.1.0 - 2025-05-28
### Added
- Release process to publish Docker images for the Control Plane server.