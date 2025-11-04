# Concepts

The pgEdge Control Plane is designed to simplify the management and orchestration of Postgres databases. It provides a declarative API for defining, deploying, and updating databases across multiple hosts.

This section introduces the core concepts and terminology used throughout the Control Plane documentation to help you understand how databases, nodes, instances, and hosts interact within a cluster.

![Concepts Diagram](img/concepts-light.png#only-light)
![Concepts Diagram](img/concepts-dark.png#only-dark)

The above diagram illustrates the relationship between nodes, hosts, instances, and databases in a distributed cluster: a database is composed of one or more nodes, each node is made of one or more instances, and each instance runs on a host.

## Hosts

Hosts are the underlying compute resources used to run database instances. One Control Plane server should be deployed to each host that will run databases. For this reason, each Control Plane server is identified by a host ID.

## Cluster

A Cluster represents a collection of hosts that are joined together to provide a unified API for managing databases. Connecting the Control Plane server on each host into a cluster enables coordination and orchestration of resources, allowing databases to be deployed, replicated, and managed across hosts.

## Databases

A database in the Control Plane API is a Postgres database that is optionally replicated between multiple Postgres instances. A database is composed of one or more [nodes](#nodes).

You create and update databases by submitting a "database spec"
to the Control Plane API. See [Creating a Database](guides/create-db.md) and
[Updating a Database](guides/update-db.md) for more information.

## Nodes

The Control Plane uses an extension, called [Spock](https://github.com/pgEdge/spock), to replicate data between Postgres instances using logical replication. In the Control Plane API, nodes refer to Spock nodes.

Spock monitors changes made in the database on the primary [instance](#instances) of each node and uses logical replication to distribute those changes to other nodes.

## Instances

Each node is composed of one or more Postgres instances, where one instance is a primary and the others are read replicas. Writes can be made to the primary instance of any node in the database.

When a node which has multiple instances is created, the primary instance for the node will be placed on the first host specified for the node in the database spec. After a database is created, the primary instance may change due to a failover or switchover operation. 

## Orchestrators

The Control Plane is architected to support various orchestrators, allowing for flexible deployment and management of database instances. At present, Docker Swarm is the only supported orchestrator, enabling containerized deployment of databases across multiple hosts.

For the Docker Swarm orchestrator, each host corresponds to a Docker Swarm node, with the Control Plane running as a Docker container. Each database instance runs as a separate Docker container within the Swarm environment. 

We plan to support additional orchestration approaches in the near future, including direct deployment to hosts without containerization.
