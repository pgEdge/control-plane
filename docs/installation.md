# Installing the pgEdge Control Plane

This guide contains instructions for deploying the Control Plane on a set of Linux hosts, such as virtual machines or bare metal servers.

## Prerequisites

- A set of Linux hosts where you want to deploy Postgres instances
    - Hosts should have a stable IP address or hostname from which they can access each other
    - Hosts should have Docker installed by following the [Docker installation guide](https://docs.docker.com/engine/install/) for your operating system.
- A volume on each host with enough space for your databases
    - This volume will be used to store configuration and data files for the
      control plane and any [database instances](concepts.md#instances) that
      run on this host.
- Open protocols and ports between hosts. By default, these are:
    - Port `3000` TCP for HTTP communication
    - Port `2379` TCP for etcd peer communication
    - Port `2380` TCP for etcd client communication
    - Port `2377` TCP for communication between manager nodes in Docker Swarm
    - Port `7946` TCP/UDP for overlay network node discovery in Docker Swarm
    - Port `4789` UDP for overlay network traffic in Docker Swarm

## Initializing Docker Swarm

Once you've provisioned hosts that meet the prerequisites, the next step is to provision a Docker Swarm cluster. Docker Swarm is used to deploy the Control Plane server on each host, and will also be used by the Control Plane to deploy Postgres instances across hosts when requested.

To initialize a new Docker Swarm cluster, run the following command on one of your hosts. This host will become the first manager in the swarm.

```sh
docker swarm init --advertise-addr 192.168.99.100
Swarm initialized: current node (dxn1zf6l61qsb1josjja83ngz) is now a manager.

To add a worker to this swarm, run the following command:

    docker swarm join \
    --token SWMTKN-1-49nj1cmql0jkz5s954yi3oex3nedyz0fb0xx14ie39trti4wxv-8vxv8rssmk743ojnwacrr2e7c \
    192.168.99.100:2377

To add a manager to this swarm, run 'docker swarm join-token manager' and follow the instructions.
```

This command will output a `docker swarm join` command with a token. Run this command on each of the other hosts to join them to the cluster.

After all hosts have joined the cluster, you can verify the cluster status by running:

```sh
docker node ls
```

This will list all nodes in the Swarm and their current status.

### Configuring Swarm Managers

Swarm manager nodes are responsible for orchestrating and maintaining the state of the Docker Swarm cluster. For high availability, it is recommended to run an odd number of managers depending on your cluster size to ensure consensus.

We recommend running 3 manager nodes for clusters with up to 7 nodes, and at most 5 managers for clusters with more than 7 nodes. 

To promote a node to manager, run the following command on a Swarm manager node:

```sh
docker node promote <node-name>
```

You can find a list of existing nodes in the swarm by running the following command:

```sh
docker node ls
```

Nodes with the `Leader` or `Reachable` status under the "MANAGER STATUS" column are managers.

**Best practices:**

- Always use an odd number of managers to ensure quorum.
- Spread your managers across regions / availability zones
- Do not make every node a manager in larger clusters

For more details, see the [Docker Swarm documentation](https://docs.docker.com/engine/swarm/admin_guide/#add-manager-nodes-for-fault-tolerance).

## Deploying the Control Plane

Once your swarm is setup, the Control Plane server should be deployed onto every node in your Docker Swarm cluster using a stack definition file.

### Creating the stack definition file

The following stack definition file will deploy a single Control Plane server to all hosts in the swarm cluster.

```yaml
services:
  server:
    image: ghcr.io/pgedge/control-plane:v0.4.0
    command: run
    environment:
      - PGEDGE_HOST_ID={{.Node.ID}}
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      mode: global
networks:
  host:
    name: host
    external: true
```

This configuration will run one Control Plane container on each Swarm node automatically. As new Swarm nodes are added, the Control Plane server will be automatically deployed to them.

!!! note

    This configuration uses the `host` network to attach each Control Plane
    container to the host's network interface. This automatically publishes all
    ports used by the container, and it's required for the Control Plane to
    function.

!!! note

    The path to the data volume **must be the same** inside the
    container as it is outside the container. The Control Plane provides this path to Docker when it runs database containers, so it needs to be accessible on the host and inside the container.

#### Alternative stack definition

If you require more granularity when customizing the Control Plane's [configuration options](./configuration.md) on a per-host basis, you can use an alternative stack definition file which uses "placement constraints" to deploy the Control Plane server onto specific Swarm nodes, with differing configuration.

In order to make it easier to generate such a configuration, you can use the generator below to create a stack definition file based on the nodes in your Docker Swarm.

First, run the following command from any node within the swarm to list the Node IDs:

```sh
docker node ls --format '{{ .ID }}'
```

Paste the output below and click "Generate Stack". This generator is fully local to this page, and does not transmit any data.

<textarea id="nodes" rows="8" style="width:100%; font-family:monospace;"></textarea>

<button id="generateBtn" data-input="nodes" data-output="global-output" class="md-button yaml-generate">Generate Stack Definition</button>

``` yaml {#global-output}
# Once submitted, the generated stack will appear here.
```

### Deploying the stack

To deploy the stack definition file, copy the contents to a file named `control-plane.yaml` on a manager node and run the following command from the same directory as the file:

```sh
docker stack deploy -c control-plane.yaml control-plane
```

This will create the services and networks defined in the stack definition file.

Once the stack is deployed, the pgEdge Control Plane server will be running on each node. From there, you can proceed with initializing the Control Plane before beginning to deploy databases. 

## Initializing the Control Plane

Once the Control Plane server is deployed on all hosts, you can proceed to initializing the Control Plane. 

Each Control Plane server starts in an uninitialized state until it's added to a Control Plane cluster. In a typical configuration, you will submit a request to one Control Plane server to initialize a new cluster, then submit requests to all other servers to join them to the new cluster.

For example, the steps to initialize a cluster with three hosts, you would:

1. Initialize the cluster on `host-1`
2. Join `host-2` to `host-1`'s cluster
3. Join `host-3` to `host-1`'s cluster

To initialize a cluster, make a `GET` request to the `/v1/cluster/init`
endpoint. The response will contain a "join token", which can be provided to
other instances via a `POST` request to the `/v1/cluster/join` endpoint. Using
the same example above, the initialization steps would be:

1.  Initialize the cluster on `host-1`

    === "curl"

        ```sh
        curl http://host-1:3000/v1/cluster/init
        ```

    This returns a response like:

    ```json
    {
      "token": "PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
      "server_url": "http://198.19.249.2:3000"
    }
    ```

    We'll submit this to the other Control Plane server instances to join them to
    the new cluster.

2.  Join `host-2` to `host-1`'s cluster

    === "curl"

        ```sh
        curl -X POST http://host-2:3000/v1/cluster/join \
            -H 'Content-Type:application/json' \
            --data '{
                "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
                "server_url":"http://198.19.249.2:3000"
            }'
        ```

    This will return a `204` response on success.

3.  Join `host-3` to `host-1`'s cluster

    === "curl"

        ```sh
        curl -X POST http://host-3:3000/v1/cluster/join \
            -H 'Content-Type:application/json' \
            --data '{
                "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
                "server_url":"http://198.19.249.2:3000"
            }'
        ```

    The "join token" can also be fetched from any host in the cluster with a `GET`
    request the `/v1/cluster/join-token` endpoint:

    === "curl"

        ```sh
        curl http://host-1:3000/v1/cluster/join-token
        ```

    After initializing the cluster, you can submit requests to any host in the
    cluster.

## Upgrading the Control Plane

We publish a new Docker image whenever we release a new version of the Control
Plane. You can "pin" to a specific version by including a version in the `image`
fields in your service spec, such as `ghcr.io/pgedge/control-plane:v0.5.0`. 

If you do not include a version, Docker will pull the
`ghcr.io/pgedge/control-plane:latest` tag by default. 

!!! note

    We recommend pinning the version in production environments so that upgrades are explicit and predictable.

### Upgrading with a pinned version

To upgrade from a pinned version:

1. Modify the `image` fields in your spec to reference the new version, such as
   updating `ghcr.io/pgedge/control-plane:v0.4.0` to
   `ghcr.io/pgedge/control-plane:v0.5.0`
2. Re-run `docker stack deploy -c control-plane.yaml control-plane` as in the
   [Deploying the stack](#deploying-the-stack) section

### Upgrading with the `latest` tag

By default, `docker stack deploy` will always query the registry for updates
unless you've specified a different `--resolve-image` option. So, updating with
the `latest` tag is a single step:

1. Re-run `docker stack deploy -c control-plane.yaml control-plane` as in the
   [Deploying the stack](#deploying-the-stack) section

### How to check the current version

If you're not sure which version you're running, such as when you're using the
`latest` tag, you can check the version of a particular Control Plane server
with the `/v1/version` API endpoint:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/version
    ```