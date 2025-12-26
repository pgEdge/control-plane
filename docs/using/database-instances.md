# Database Instances

A database instance is:
* A running Postgres server
* Bound to a specific host
* Identified by a node name (e.g. n1)
* Identified globally by an instance ID 
(eg 68f50878-44d2-4524-a823-e31bd478706d-n1-689qacsi)  
<br>

Each instance maps to exactly one physical host, but a host can run multiple database instances as long as ports do not clash. 
<br>

While a database is stable and persistent, database instances are runtime components with states that are not “identity-critical”. This allows rolling updates, automatic failovers, and safe restarts.

Instances and port ownership
* Ports are unique per host
* Stopping an instance does not “free” the port
* Restarting preserves the same port
* Changing ports requires a plan update and a restart
* Port conflicts cause update failure    
 

Users in the database spec:
* Are reconciled onto each instance
* Get created / updated / dropped during instance configuration
* Must remain consistent across all instances
* Cannot conflict with system users

States
Instances can be in different states as a result of database operations (start/stop/restart/etc):

* `available`
* `starting`
* `stopping`
* `restarting`
* `failed`
* `error`
* `unknown`
<br><br>



# Database Instance Operations
## Stop Instance
Stopping an instance shuts down the Postgres process for that specific instance by scaling it to zero. The instance no longer accepts connections and is taken out of service, but its data and configuration are preserved. Other instances in the same database can continue running.
As Stop Instance removes a database instance from service without deleting it, it can be used to isolate an instance not currently in use but that is expected to be restarted later.

* Transition: available → stopping → stopped
* Port remains reserved for this instance
* Other instances remain unaffected
* A stopped instance continues to appear under list-databases with state: "stopped"

Example: Stop a database “example” whose instance ID is “n1”

=== "curl"


   ```sh
   curl  http://host-3:3000/v1/databases/example/instances/n1/stop
   ```


## Start Instance
Starts a specific instance within a database by scaling it back up. This operation is only valid when the instance is in a stopped state. A successful start instance operation will transition an instance state from stopped to starting to available, allowing normal access and use to continue and restarting any activities 
* Transition: stopped → starting → available
* Retains same port as before stop
* Fails if port is already taken (a safety check)

Example: Start a database “example” whose instance ID is “n1”

=== "curl"

   ```sh
   curl  http://host-3:3000/v1/databases/example/instances/n1/start
   ```
## Restart Instance
Restarting an instance stops and then starts the same Postgres instance, either immediately or at a scheduled time. This is typically used to recover from errors or apply changes that require a restart. The instance keeps its identity and data, but experiences a brief downtime during the restart.
* If no scheduled_at is provided → restart immediately.
* Transition: available → restarting → available
* Restart is blocked if:
No configuration changes require a restart
Another update is in progress
Instance is not stable

Example: Start a database “example” whose instance ID is “n1”
=== "curl"


   ```sh
   curl  http://host-3:3000/v1/databases/example/instances/n1/restart
   ```

