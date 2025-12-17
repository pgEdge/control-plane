# pgEdge Control Plane

The [**pgEdge Control Plane** ](https://github.com/pgedge/control-plane) is a distributed application designed to simplify the management and orchestration of Postgres databases. It provides a declarative API for defining, deploying, and updating databases across multiple hosts.

In its default configuration, it uses an embedded [etcd](https://etcd.io/) server to store configuration and coordinate database operations with other instances.

You interact with the Control Plane via an [HTTP API](api/reference.md). Once you've initialized a Control Plane cluster, you can submit requests to the API of any Control Plane server in the cluster to create and manage Postgres databases.

Most Control Plane API operations, such as database modifications, are
idempotent. If an operation fails, you can safely retry the operation after
resolving the underlying issue.

## Features

The pgEdge Control Plane supports:

- deploying Postgres 16, 17, and 18 with support for managed extensions.
    - Extension support includes: Spock, LOLOR, Snowflake, pgAudit, PostGIS, pgVector, pgEdge Vectorizer, pg_tokenizer, vchord_bm25, pg_vectorize, pgmq, pg_cron, pg_stat_monitor.
- deploying multiple Postgres instances on the same host, enabling efficient resource utilization and consolidation of workloads.
- flexible deployment options for both single-region and multi-region deployments. You can:
    - deploy to a single region with optional standby replicas.
    - deploy across multiple regions with Spock active-active replication, with optional standby replicas.
- failover and switchover operations via the API to manage primary and replica instances.
- starting, stopping, and restarting database instances via the API.
- managing Spock active-active replication configuration when deploying distributed databases with multiple nodes. Spock provides support for:
    - automatic DDL Replication (AutoDDL) by default.
    - zero downtime node addition.
- backup and restore operations for databases via pgBackRest integration. This enables:
    - scheduled backups with customizable configuration for distributed setups.
    - on-demand backups to protect your data and support operational needs.
    - in-place restores to enable rapid disaster recovery and minimize downtime.
    - database creation from existing pgBackRest repositories, supporting migration and cloning use cases.
    - distributed node addition via pgBackRest restore.
- monitoring database operations through detailed task logs, enabling visibility into deployment progress, troubleshooting, and historical activity tracking.
- secure API access with certificate-based authentication.
- performing in-place minor version upgrades of Postgres and supporting components.
- performing major version upgrades of Postgres using zero downtime node addition.