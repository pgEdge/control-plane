# Managing Database Instances

A Database instance is:

* a running Postgres server.
* bound to a specific host.
* identified by a node name (for example, n1).
* identified globally by an instance ID
    (for example, 68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi).

Each instance maps to exactly one physical host, but a host can run
multiple database instances as long as ports do not clash. 

While a database is stable and persistent, database instances are runtime
components with states that are not *identity-critical*. This allows rolling
updates, automatic failovers, and safe restarts.

Managing a database instance involves handling various operational needs
throughout its lifecycle. For example, modifying certain Postgres parameters
can require a restart to take effect (e.g., shared_buffers), as the Control
Plane does not automatically restart instances on a user's behalf.
Additionally, managing instances allows you to control costs by stopping
unused development or test environments, troubleshoot issues when systems
become unresponsive, and coordinate system updates in a controlled manner to
minimize disruption to your applications.

## Monitoring Instances
To access important information about a database instance, call the
`GetDatabase` endpoint by specifying its database ID. In the following 
command, `curl` retrieves database information for a database named `example`:

=== "curl"
```sh
curl http://host-3:3000/v1/databases/example
```

The endpoint will return the following information pertaining to the database:

* `created_at` is the timestamp when the database was created.
* `id` is the database identifier.
* `instances` is an array of instance objects containing instance-specific
  information.
* `spec` contains the database specification.
* `state` shows the current state of the database.
* `updated_at` is the timestamp of the last update.

### Finding Instance IDs

The `instances` array contains information about a database instance,
specifically:

* `id` is the instance identifier (e.g., `storefront-n1-689qacsi`) required
  for instance operations.
* `node_name` is the node this instance belongs to (e.g., `n1`).
* `host_id` is the host where this instance runs.
* `state` is the current operational state of the instance.
* `postgres` contains Postgres status information, including the
  `pending_restart` field which indicates if a configuration change requires a
  restart.
* `connection_info` contains connection details for this instance.
* `created_at` and `updated_at` are instance timestamps.

The `pending_restart` field within the `postgres` object is particularly
important—when `true`, it signals that configuration changes have been applied
that will only take effect after the instance is restarted.

### Instance States

Postgres instances can be in different states as a result of database
operations; Postgres states may be:

* `available`
* `starting`
* `stopping`
* `stopped`
* `restarting`
* `failed`
* `error`
* `unknown`

## Stopping Instances

Stopping an instance shuts down the Postgres process for that specific
instance by scaling it to zero. The instance no longer accepts connections and
is taken out of service, but its data and configuration are preserved. Other
instances in the same database can continue running. 

!!! note

    As `Stop Instance` removes a database instance from service without 
    deleting it, it can be used toisolate an instance not currently in use but that is expected to be restarted later.

* The transition is: available → stopping → stopped.
* The port remains reserved for this instance.
* Other instances remain unaffected.
* A stopped instance continues to appear under list-databases in a
  `stopped` state.

In the following example, the `curl` command stops an instance named
"example-n1-689qacsi" for a database named "example":

=== "curl"
```sh
curl -X POST \
  http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/stop-instance
```

## Starting Instances

Starts a specific instance within a database by scaling it back up. This
operation is only valid when the instance is in a stopped state. A successful
start instance operation will transition an instance state from stopped to
starting to available, allowing normal access and use to continue and
restarting any activities.

* The transition is: stopped → starting → available.
* The instance retains the same port as before stop.
* The operation fails if the port is already taken (a safety check).

In the following example, the `curl` command starts an instance with the ID
`example-n1-689qacsi` for a database named `example`:

=== "curl"
```sh
curl -X POST \
  http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/start-instance
```

## Restarting Instances

Restarting an instance stops and then starts the same Postgres instance,
either immediately or at a scheduled time. This is typically used to recover
from errors or apply changes that require a restart. The instance keeps its
identity and data, but experiences a brief downtime during the restart.

* If no `scheduled_at` is provided, the restart happens immediately.
* The transition is: available → restarting → available.
* Restart is blocked if no configuration changes require a restart, another
  update is in progress, or the instance is not stable.


In the following example, the `curl` command restarts an instance with ID
`example-n1-689qacsi` for a database named `example`:

=== "curl"
```sh
curl -X POST \
  http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/restart-instance
```
