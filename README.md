# pgEdge Control Plane

The pgEdge Control Plane is a distributed application designed to simplify the management and orchestration of Postgres databases. It provides a declarative API for defining, deploying, and updating databases across multiple hosts.

You interact with the Control Plane via an HTTP API. Once you've initialized a Control Plane cluster, you can submit requests to the API of any Control Plane server in the cluster to create and manage Postgres databases.

## Table of Contents

- [Features](#features)
- [Releases](#releases)
- [Development](#development)
- [Documentation](#documentation)

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

## Releases

Control Plane releases are available as [GitHub Releases](https://github.com/pgEdge/control-plane/releases). Each release includes a change log and downloadable binaries, along with generated Software Bill of Materials (SBOMs).

Control Plane server images are published upon release in the [pgEdge Github Container Repository](https://github.com/orgs/pgEdge/packages/container/package/control-plane).

A [CHANGELOG](CHANGELOG.md) is available with full release notes for each version.

You can learn more about the release process in our [documentation](docs/development//development.md#release-process).

## Development

The Control Plane is written in Golang, and includes a Docker Compose setup for ease of local development.

1. Install dependencies: Ensure you have Go 1.20+ and Docker installed, with [host networking](https://docs.docker.com/engine/network/drivers/host/#docker-desktop) enabled.

2. Clone the repository:  

    ```sh
    git clone https://github.com/pgEdge/control-plane.git
    cd control-plane
    ```

3. Build and run the Control Plane locally:

    ```sh
    make dev-watch
    ```

For information on interacting with the Control Plane locally as part of a development workflow, see [docs/development/running-locally.md](docs/development/running-locally.md).

## Documentation

The documentation for this project uses MkDocs with the Material theme to generate styled static HTML documentation from Markdown files in the docs directory.

The documentation can be accessed locally at http://localhost:8000 using:

``` sh
make docs
```