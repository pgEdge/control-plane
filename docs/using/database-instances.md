# Managing Database Instances
A Database instance is:
* A running Postgres server
* Bound to a specific host
* Identified by a node name (e.g. n1)
* Identified globally by an instance ID (e.g., 68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi)

Each instance maps to exactly one physical host, but a host can run multiple database instances as long as ports do not clash. 

While a database is stable and persistent, database instances are runtime components with states that are not "identity-critical". This allows rolling updates, automatic failovers, and safe restarts.

Managing a database instance involves handling various operational needs throughout its lifecycle. For example, modifying certain PostgreSQL parameters can require a restart to take effect (e.g., shared_buffers), as the Control Plane does not automatically restart instances on a user’s behalf. Additionally, managing instances allows you to control costs by stopping unused development or test environments, troubleshoot issues when systems become unresponsive, and coordinate system updates in a controlled manner to minimize disruption to your applications.

## Monitoring Instances
To access important information about a database instance, call the GetDatabase endpoint by specifying its database ID as shown below for a database "example".

In the following example, the `curl` command retrieves database information for a database named "example":

=== "curl"
```sh
curl http://host-3:3000/v1/databases/example
```

The endpoint will return the following information pertaining to the database.

* `created_at`: Timestamp when the database was created
* `id`: Database identifier
* `instances`: Array of instance objects containing instance-specific information
* `spec`: Database specification
* `state`: Current state of the database
* `updated_at`: Timestamp of last update

### Finding Instance IDs

The `instances` array contains information about a database instance, specifically:

* `id`: The instance identifier (e.g., `storefront-n1-689qacsi`) required for instance operations
* `node_name`: The node this instance belongs to (e.g., `n1`)
* `host_id`: The host where this instance runs
* `state`: Current operational state of the instance
* `postgres`: Postgres status information, including the `pending_restart` field which indicates if a configuration change requires a restart
* `connection_info`: Connection details for this instance
* `created_at` and `updated_at`: Instance timestamps

The `pending_restart` field within the `postgres` object is particularly important—when `true`, it signals that configuration changes have been applied that will only take effect after the instance is restarted.

### Instance States

Postgres instances can be in different states as a result of database operations (start/stop/restart/etc):

* `available`
* `starting`
* `stopping`
* `stopped`
* `restarting`
* `failed`
* `error`
* `unknown`

## Stopping Instances

Stopping an instance shuts down the Postgres process for that specific instance by scaling it to zero. The instance no longer accepts connections and is taken out of service, but its data and configuration are preserved. Other instances in the same database can continue running. As Stop Instance removes a database instance from service without deleting it, it can be used to isolate an instance not currently in use but that is expected to be restarted later.

* Transition: available → stopping → stopped
* Port remains reserved for this instance
* Other instances remain unaffected
* A stopped instance continues to appear under list-databases with state: "stopped"

In the following example, the `curl` command stops an instance named "n1" for a database named "example":

=== "curl"
```sh
curl -X POST http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/stop-instance

```

## Starting Instances

Starts a specific instance within a database by scaling it back up. This operation is only valid when the instance is in a stopped state. A successful start instance operation will transition an instance state from stopped to starting to available, allowing normal access and use to continue and restarting any activities.

* Transition: stopped → starting → available
* Retains same port as before stop
* Fails if port is already taken (a safety check)

In the following example, the `curl` command starts an instance with ID "example-n1-689qacsi" for a database named "example":

=== "curl"
```sh
curl -X POST http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/start-instance
```

## Restarting Instances

Restarting an instance stops and then starts the same Postgres instance, either immediately or at a scheduled time. This is typically used to recover from errors or apply changes that require a restart. The instance keeps its identity and data, but experiences a brief downtime during the restart.

* If no scheduled_at is provided → restart immediately.
* Transition: available → restarting → available
* Restart is blocked if: No configuration changes require a restart, Another update is in progress, or Instance is not stable


In the following example, the `curl` command restarts an instance with ID "example-n1-689qacsi" for a database named "example":

=== "curl"
```sh
curl -X POST http://host-3:3000/v1/databases/example/instances/example-n1-689qacsi/restart-instance
```
