# Supporting Services (Beta)

The pgEdge Control Plane lets you run services alongside your
databases. Services are applications that attach to a database, run on
any host in the cluster, and connect via automatically-managed
database credentials.

## What Are Supporting Services?

A supporting service is an application that runs alongside a database.
Each service instance runs on a single host and receives its own set of
database credentials scoped to that instance. The Control Plane supports
the following service types:

- The [pgEdge Postgres MCP Server](mcp.md) connects AI agents and
  LLM-powered applications to your database, enabling natural language
  queries and AI-powered data access.
- The pgEdge RAG Server *(coming soon)* enables retrieval-augmented
  generation workflows using your database as a knowledge store.
- [PostgREST](postgrest.md) automatically generates a REST API from
  your PostgreSQL schema, making your data accessible over HTTP without
  writing backend code.

## Service Instances

When you add a service to a database, the Control Plane creates one
service instance per host listed in the service's `host_ids`. Each
instance runs on a single host and receives its own database
credentials. Services can run on any host in the cluster; they do not
need to be co-located with database instances.

The following table describes the lifecycle states for service
instances:

| State | Description |
|-------|-------------|
| `creating` | The Control Plane is provisioning the service instance. |
| `running` | The service instance is healthy and operational. |
| `failed` | The service instance exited or failed its health check. |
| `deleting` | The Control Plane is removing the service instance. |

## Deployment Topologies

Services are independent of your database node topology, so you can
place service instances on any host in the cluster. The following
deployment patterns are common:

- In a co-located topology, the service runs on the same host as a
  database instance, which minimizes network latency between the
  service and Postgres.
- In a separate-host topology, the service runs on a dedicated host
  with no database instance, which isolates the service workload from
  the database.
- In a multiple-instances topology, one service instance runs per host
  for redundancy or regional proximity; each instance receives its own
  credentials and connects to the database independently.

In the following example, the service runs on the same host as the
database node (`host-1`):

```json
"nodes":    [ { "name": "n1", "host_ids": ["host-1"] } ],
"services": [ { ..., "host_ids": ["host-1"] } ]
```

In the following example, the service runs on a dedicated host
(`host-3`) with no database instance:

```json
"nodes":    [ { "name": "n1", "host_ids": ["host-1"] },
              { "name": "n2", "host_ids": ["host-2"] } ],
"services": [ { ..., "host_ids": ["host-3"] } ]
```

In the following example, the service runs on each database host,
creating one instance per host for redundancy:

```json
"nodes":    [ { "name": "n1", "host_ids": ["host-1"] },
              { "name": "n2", "host_ids": ["host-2"] } ],
"services": [ { ..., "host_ids": ["host-1", "host-2"] } ]
```

## Database Credentials

Each service instance is automatically provisioned with two dedicated
database users. The Control Plane manages these credentials; you do not
need to create or rotate them manually. The credentials are:

- `svc_{service_id}_ro` is a read-only user with read access to the
  database; this user is the default for most service types.
- `svc_{service_id}_rw` is a read-write user with read and write access
  to the database; this user is provisioned when the service needs
  read/write access.

## Next Steps

To add a service to a database, see [Managing Services](managing.md).
Then refer to the page for your specific service type for configuration
details.
