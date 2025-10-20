# pgEdge Control Plane

The **pgEdge Control Plane** is a distributed application designed to simplify the management and orchestration of Postgres databases. It provides a declarative API for defining, deploying, and updating databases across multiple hosts.

In its default configuration, it uses an embedded [etcd](https://etcd.io/) server to store configuration and coordinate database operations with other instances.

You interact with the Control Plane via an [HTTP API](api/reference.md). Once you've [initialized](guides/initialization.md) a Control Plane cluster, you can submit requests to any Control Plane instance in the cluster to create and manage Postgres databases.

Most Control Plane API operations, such as database modifications, are
idempotent. If an operation fails, you can safely retry the operation after
resolving the underlying issue.

## Features

At a high level, the **pgEdge Control Plane** supports:

- Deploying Postgres 16, 17, and 18 with support for managed extensions.
    - Extension support includes: Spock, LOLOR, Snowflake, pgAudit, PostGIS, pgVector.
- Support for deploying multiple Postgres instances on the same host, enabling efficient resource utilization and consolidation of workloads
- Flexible deployment options for both single-region and multi-region deployments.
    - Deploy to a single region with optional standby replicas.
    - Deploy across multiple regions with Spock active-active replication, with optional standby replicas.
- Performing failover and switchover operations via API to manage primary and replica instances
- Ability to start, stop, and restart database instances via API
- Managing Spock active-active replication configuration when deploying distributed databases with multiple nodes
    - Support for Automatic DDL Replication (AutoDDL) by default.
    - Support for adding nodes using Spock with zero downtime
- Backup and restore operations for databases via pgBackRest integration.
    - Scheduled backups with customizable configuration for distributed setups
    - Perform on-demand backups to protect your data and support operational needs
    - Perform in-place restores to enable rapid disaster recovery and minimize downtime
    - Create new databases from existing pgBackRest repositories, supporting migration and cloning use cases
    - Add distributed nodes via pgBackRest restore
- Monitoring database operations through detailed task logs, enabling visibility into deployment progress, troubleshooting, and historical activity tracking
- Secure API access with certificate-based authentication
- Performing in-place minor version upgrades of Postgres and surrounding components

## Supported Operating Systems

Currently, the pgEdge Control Plane supports deplying databases to virtual machines and bare metal hosts using the Docker Swarm orchestrator, with both the Control Plane and Postgres instances running in containers.

With this model, the pgEdge Control Plane can be deployed to hosts running any Linux distribution supported by [Docker Engine for Linux](https://docs.docker.com/engine/install/), on either x86_64 or arm64 architectures.

We plan to support additional orchestration approaches in the near future, including direct deployment to hosts without containerization.
