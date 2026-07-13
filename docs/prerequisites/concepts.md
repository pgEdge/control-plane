# Concepts

The pgEdge Control Plane is designed to simplify the management and orchestration of Postgres databases. It provides a declarative API for defining, deploying, and updating databases across multiple hosts.

This section introduces the core concepts and terminology used throughout the Control Plane documentation to help you understand how databases, nodes, instances, and hosts interact within a cluster.

![Concepts Diagram](../img/concepts-light.png#only-light)
![Concepts Diagram](../img/concepts-dark.png#only-dark)

The above diagram illustrates the relationship between nodes, hosts, instances, and databases in a distributed cluster: a database is composed of one or more nodes, each node is made of one or more instances, and each instance runs on a host.

## Hosts

Hosts are the underlying compute resources used to run database instances. One Control Plane server should be deployed to each host that will run databases. For this reason, each Control Plane server is identified by a host ID.

## Cluster

A Cluster represents a collection of hosts that are joined together to provide a unified API for managing databases. Connecting the Control Plane server on each host into a cluster enables coordination and orchestration of resources, allowing databases to be deployed, replicated, and managed across hosts.

## Databases

A database in the Control Plane API is a Postgres database that is optionally replicated between multiple Postgres instances. A database is composed of one or more [nodes](#nodes).

You create and update databases by submitting a "database spec"
to the Control Plane API. See [Creating a Database](../using/create-db.md) and
[Updating a Database](../using/update-db.md) for more information.

## Nodes

The Control Plane uses an extension, called [Spock](https://github.com/pgEdge/spock), to replicate data between Postgres instances using logical replication. In the Control Plane API, nodes refer to Spock nodes.

Spock monitors changes made in the database on the primary [instance](#instances) of each node and uses logical replication to distribute those changes to other nodes.

## Instances

Each node is composed of one or more Postgres instances, where one instance is a primary and the others are read replicas. Writes can be made to the primary instance of any node in the database.

When a node which has multiple instances is created, the primary instance for the node will be placed on the first host specified for the node in the database spec. After a database is created, the primary instance may change due to a failover or switchover operation. 

## Orchestrators

The Control Plane is architected to support multiple orchestrators, giving you flexibility in how database instances are deployed and managed.

**Docker Swarm** is the default orchestrator. Each host corresponds to a Docker Swarm node, with the Control Plane running as a Docker container. Each database instance runs as a separate Docker container within the Swarm environment.

**systemd** is available as an alternative orchestrator for deployments on bare metal or VMs where you prefer not to use containers. With the systemd orchestrator, the Control Plane runs as a native Linux service and manages each Postgres instance as a systemd unit. This is a good fit for organizations with existing infrastructure built around system package managers, or for environments where standard Linux processes are preferred over containers. See [Installing via System Packages](../installation/systemd.md) for setup instructions.

All orchestrators provide the same core API and feature set: declarative database management, Patroni-based high availability, and pgBackRest backup/restore integration.
