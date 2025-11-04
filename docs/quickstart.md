# Quickstart

This quickstart guide demonstrates how to use pgEdge Control Plane to create a sample three node pgEdge Distributed Postgres database on a single host, such as your laptop.

This configuration is intended to demonstrate basic use of the Control Plane and its API.

## Prerequisites

- Linux or MacOS
- Docker
    - For development environments, see [Docker Desktop](https://docs.docker.com/desktop/)
    - For server environments, see [Docker Engine](https://docs.docker.com/engine/)
- The cURL command-line HTTP client
- The `psql` command-line Postgres client (optional, but recommended)
    - This is typically installed alongside the Postgres server. Refer to the [Postgres server installation instructions](https://www.postgresql.org/download/) for your operating system.

!!! note

    If you are using Docker Desktop, you must also enable [host networking](https://docs.docker.com/engine/network/drivers/host/#docker-desktop).

## Installation

1.  Enable [Swarm mode](https://docs.docker.com/engine/swarm/) on your Docker daemon:

    ```sh
    docker swarm init
    ```

2.  Create a `data` directory:

    ```sh
    mkdir -p ~/pgedge/control-plane
    ```

    !!!note

        This directory will be used for Control Plane's internal database
        files as well as the Postgres `data` directories for any databases you create
        with Control Plane.

3.  Start the pgEdge Control Plane:

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

    !!! warning

        If you wish to use an alternate location for the `data` directory, keep in mind that the
        `data` directory path inside the container must be identical to the path on the
        host. This is important because Control Plane provides this path to Docker
        when it starts a Postgres database instance.

4.  Initialize the Control Plane cluster:

    === "curl"

        ```sh
        curl http://localhost:3000/v1/cluster/init
        ```

    This command will return a *join token*. A join token is used to initialize a Control Plane
    cluster across multiple hosts. We won't use a join token in this guide.

5.  Create a pgEdge database:

    === "curl"

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

    This command creates a three node distributed Postgres database on a single host with one instance per node and an `admin` database user. The creation process is asynchronous, meaning the server responds when the process has started rather than when it has finished.
    
    To track the progress of this task, fetch the database and inspect the `state`
    field:

    === "curl"

        ```sh
        curl http://localhost:3000/v1/databases/example
        ```

    The creation process is complete once the `state` field is `available`. This
    response also contains connection information for each available database
    instance.

    !!! tip

        This connection information shows the IP of the host that's running the
        database instance. Docker Desktop for MacOS and Windows utilizes a virtual machine that is not accessible by IP address, so this information will not be usable as-is on these platforms. This guide instructs you to use `localhost` because it works on all platforms.


## Connecting to Each Database Instance

You can use your choice of Postgres client or `docker exec` to connect to each database instance within your Postgres cluster.

### Connecting with `psql`

If you have the `psql` command-line client installed on your host, you can access each instance as follows:

```sh
# To connect to the 'n1' node's instance:
PGPASSWORD=password psql -h localhost -p 6432 -U admin example

# To connect to the 'n2' node's instance:
PGPASSWORD=password psql -h localhost -p 6433 -U admin example

# To connect to the 'n3' node's instance:
PGPASSWORD=password psql -h localhost -p 6434 -U admin example
```

### Connecting with `docker exec`

You can also use the `docker exec` command to run the `psql` client from within each database container. First, retrieve the container IDs with the command:

```sh
docker ps --filter label=pgedge.database.id=example
```

The first column in the output shows the ID for each container and the last
column displays the container name. Each container name is prefixed with
`postgres` and the node name, for example `postgres-n1`. You can use these names
to distinguish which node you're connecting to. Once you've identified the
container for a particular node, you can copy its container ID and run:

```sh
docker exec -it <container ID> psql -U admin example
```

## Trying out Replication

!!! tip

    These instructions demonstrate connecting with a copy of the `psql` client that resides on the host, but the same instructions will work with the `docker exec` approach described above under [Connecting to each database instance](#connecting-to-each-database-instance)

1. Create a table on the first node:

    ```sh
    PGPASSWORD=password psql -h localhost -p 6432 -U admin example -c "CREATE TABLE example (id int primary key, data text);"
    ```

2. Insert a row into our new table on the second node:

    ```sh
    PGPASSWORD=password psql -h localhost -p 6433 -U admin example -c "INSERT INTO example (id, data) VALUES (1, 'Hello, pgEdge!');"
    ```

3. Verify that the new row has replicated back to the first node:

    ```sh
    PGPASSWORD=password psql -h localhost -p 6432 -U admin example -c "SELECT * FROM example;"
    ```

## Loading the Northwind Sample Dataset

The Northwind sample dataset is a Postgres database dump that you can use to try replication with a more realistic database. To load the Northwind dataset into your pgEdge database, use the command:

```sh
curl https://downloads.pgedge.com/platform/examples/northwind/northwind.sql \
    | PGPASSWORD=password psql -h localhost -p 6432 -U admin example
```

Now, try querying one of the new tables from the other node:

```sh
PGPASSWORD=password psql -h localhost -p 6433 -U admin example -c "SELECT * FROM northwind.shippers"
```

## Teardown

To stop the Control Plane and remove all of the resources it created, first delete any databases that you've created with the command:

=== "curl"

    ```sh
    curl -X DELETE http://localhost:3000/v1/databases/example
    ```

Like the creation process, the deletion process is asynchronous. You can
track the progress of the `DELETE` by using the `list databases` endpoint:

=== "curl"

    ```sh
    curl http://localhost:3000/v1/databases
    ```

The database will disappear from this response when the deletion is complete.  Next, stop and remove the Control Plane server container:

```sh
docker stop host-1
docker rm host-1
```

Finally, you can delete the `data` directory created during installation:

```sh
rm -rf ~/pgedge/control-plane
```
