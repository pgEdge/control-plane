# Quickstart

This quickstart guide demonstrates how to run the pgEdge Control Plane and an
example three-node pgEdge database to a single host, such as your laptop. This
configuration is intended to demonstrate basic usage of the Control Plane and
its API

- [Quickstart](#quickstart)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Connecting to each database instance](#connecting-to-each-database-instance)
    - [With `psql`](#with-psql)
    - [With `docker exec`](#with-docker-exec)
  - [Try out replication](#try-out-replication)
  - [Load the Northwind example dataset](#load-the-northwind-example-dataset)
  - [Teardown](#teardown)
  - [What's next?](#whats-next)

## Prerequisites

- Linux or MacOS
- Docker
  - For development environments, see [Docker Desktop](https://docs.docker.com/desktop/)
  - For server environments, see [Docker Engine](https://docs.docker.com/engine/)
- The cURL command-line HTTP client
- The `psql` command-line PostgreSQL client (optional, but recommended)
  - This is typically installed alongside the PostgreSQL server. Refer to the
    [PostgreSQL server installation instructions](https://www.postgresql.org/download/)
    for your operating system.

> [!IMPORTANT]
> If you are using Docker Desktop, you must also enable [host networking](https://docs.docker.com/engine/network/drivers/host/#docker-desktop).

## Installation

1. Enable "Swarm mode" on your Docker daemon

```sh
docker swarm init
```

2. Create a data directory[^1]

```sh
mkdir -p ~/pgedge/control-plane
```

[^1]: This directory will be used for the Control Plane's internal database
files as well as the PostgreSQL data directories for any databases you create
with the Control Plane.

3. Start the pgEdge Control Plane

> [!IMPORTANT]
> If you use an alternate location for the data directory, keep in mind that the
> data directory path inside the container must be identical to the path on the
> host. This is important because the Control Plane provides this path to Docker
> when it starts a pgEdge database instance.

```sh
docker run --detach \
    --env PGEDGE_HOST_ID=host-1 \
    --env PGEDGE_DATA_DIR=${HOME}/pgedge/control-plane \
    --volume ${HOME}/pgedge/control-plane:${HOME}/pgedge/control-plane \
    --volume /var/run/docker.sock:/var/run/docker.sock \
    --network host \
    --name host-1 \
    ghcr.io/pgedge/control-plane \
    run
```

4. Initialize the Control Plane cluster

```sh
curl http://localhost:3000/v1/cluster/init
```

This will print out a "join token". This is used to initialize a Control Plane
cluster across multiple hosts. We won't use it in this guide.

5. Create a pgEdge database

```sh
curl -X POST http://localhost:3000/v1/databases \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "database_users": [
                {
                    "username": "admin",
                    "password": "password",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "nodes": [
                { "name": "n1", "port": 6432, "host_ids": ["host-1"] },
                { "name": "n2", "port": 6433, "host_ids": ["host-1"] },
                { "name": "n3", "port": 6434, "host_ids": ["host-1"] }
            ]
        }
    }'
```

This will create a three-node pgEdge database cluster with one instance per node
and an `admin` database user. The creation process is asynchronous, meaning the
server responds when the process has started rather than when it has finished.
To track the progress of this task, fetch the database and inspect the `state`
field:

```sh
curl http://localhost:3000/v1/databases/example
```

The creation process is complete once the `state` field is `available`. This
response also contains connection information for each available database
instance.

> [!TIP]
> This connection information shows the IP of the host that's running the
> database instance. Docker Desktop for MacOS and Windows utilizes a virtual
> machine that is not accessible by IP address, so this information will not be
> usable as-is on these platforms. This guide instructs you to use `localhost`
> because it works on all platforms.

## Connecting to each database instance

### With `psql`

If you have the `psql` command-line client installed on your host, you can
access each instance as follows:

```sh
# The 'n1' node's instance
PGPASSWORD=password psql -h localhost -p 6432 -U admin example

# The 'n2' node's instance
PGPASSWORD=password psql -h localhost -p 6433 -U admin example

# The 'n3' node's instance
PGPASSWORD=password psql -h localhost -p 6434 -U admin example
```

### With `docker exec`

You can also use `docker exec` to run the `psql` client from within each
database container. First, list the databases containers by running:

```sh
docker ps --filter label=pgedge.database.id=example
```

The first column in the output shows the ID for each container and the last
column shows the name of the container. Each container name is prefixed with
`postgres` and the node name, for example `postgres-n1`. You can use these names
to distinguish which node you're connecting to. Once you've identified the
container for a particular node, you can copy its container ID and run:

```sh
docker exec -it <container ID> psql -U admin example
```

## Try out replication

> [!TIP]
> These instructions use the `psql` client on the host, but the same
> instructions will work with the `docker exec` approach described above under
> [Connecting to each database instance](#connecting-to-each-database-instance)

1. Create a table on the first node:
```sh
PGPASSWORD=password psql -h localhost -p 6432 -U admin example -c "create table example (id int primary key, data text);"
```
2. Insert a row into our new table on the second node:
```sh
PGPASSWORD=password psql -h localhost -p 6433 -U admin example -c "insert into example (id, data) values (1, 'Hello, pgEdge!');"
```
3. See that the new row has replicated back to the first node:
```sh
PGPASSWORD=password psql -h localhost -p 6432 -U admin example -c "select * from example;"
```

## Load the Northwind example dataset

The Northwind example dataset is a PostgreSQL database dump that you can use to
try replication with a more realistic database.  To load the Northwind dataset
into your pgEdge database, run:

```sh
curl https://downloads.pgedge.com/platform/examples/northwind/northwind.sql \
    | PGPASSWORD=password psql -h localhost -p 6432 -U admin example
```

Now, try querying one of the new tables from the other node:

```sh
PGPASSWORD=password psql -h localhost -p 6433 -U admin example -c "select * from northwind.shippers"
```

## Teardown

In order to stop the Control Plane and remove all resources it created, first
delete any databases that you've created

```sh
curl -X DELETE http://localhost:3000/v1/databases/example
```

Similar to the creation process, the deletion process is asynchronous. You can
track the progress of the delete by using the "list databases" endpoint:

```sh
curl http://localhost:3000/v1/databases
```

The database will disappear from this response once the deletion is complete.

Next, stop and remove the Control Plane server container:

```sh
docker stop host-1
docker rm host-1
```

Finally, you can delete the data directory that you created during installation:

```sh
rm -rf ~/pgedge/control-plane
```

## What's next?

See the [User Guide](./user-guide.md) for more information about running the
Control Plane in a production environment along with detailed descriptions of
its API operations.
