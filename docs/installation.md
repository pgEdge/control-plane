# Installing the pgEdge Control Plane

This guide contains instructions for deploying the Control Plane on a set of Linux hosts, such as virtual machines or bare metal servers.

## Prerequisites

Before installing the Control Plane, you must:

- Create a set of Linux hosts where you want to deploy Postgres instances:
    - Hosts should have a stable IP address or hostname from which they can access each other.
    - Hosts should have Docker installed by following the [Docker installation guide](https://docs.docker.com/engine/install/) for your operating system.
- Create a volume on each host with enough space for your databases:
    - This volume will be used to store configuration and data files for the
      control plane and any [database instances](concepts.md#instances) that
      run on this host.
- Open protocols and ports between hosts. By default, these are:
    - Port `3000` TCP for HTTP communication
    - Port `2379` TCP for Etcd peer communication
    - Port `2380` TCP for Etcd client communication
    - Port `2377` TCP for communication between manager nodes in Docker Swarm
    - Port `7946` TCP/UDP for overlay network node discovery in Docker Swarm
    - Port `4789` UDP for overlay network traffic in Docker Swarm

## Initializing Docker Swarm

After provisioning hosts that meet the prerequisites, the next step is to provision a Docker Swarm cluster. Docker Swarm is used to deploy the Control Plane server on each host, and will also be used by the Control Plane to deploy Postgres instances across hosts when requested.

To initialize a new Docker Swarm cluster, run the following command on one of your hosts. This host will become the first manager in the swarm. Use the command:

```sh
docker swarm init --advertise-addr 192.168.99.100
Swarm initialized: current node (dxn1zf6l61qsb1josjja83ngz) is now a manager.

To add a worker to this swarm, run the following command:

    docker swarm join \
    --token SWMTKN-1-49nj1cmql0jkz5s954yi3oex3nedyz0fb0xx14ie39trti4wxv-8vxv8rssmk743ojnwacrr2e7c \
    192.168.99.100:2377

To add a manager to this swarm, run 'docker swarm join-token manager' and follow the instructions.
```

This command will output a `docker swarm join` command with a token. Invoke this command on each of the other hosts to join them to the cluster. After all hosts have joined the cluster, you can verify the cluster status by running:

```sh
docker node ls
```

This will list all nodes in the Swarm and their current status.

### Configuring Swarm Managers

Swarm manager nodes are responsible for orchestrating and maintaining the state of the Docker Swarm cluster. For high availability, we recommend using an odd number of managers depending on your cluster size to ensure consensus.

!!! hint

    We recommend running 3 manager nodes for clusters with up to 7 nodes, and at most 5 managers for clusters with more than 7 nodes. 

To promote a node to manager, run the following command on a Swarm manager node:

```sh
docker node promote <node-name>
```

You can find a list of existing nodes in the swarm by running the following command:

```sh
docker node ls
```

Nodes with the `Leader` or `Reachable` status under the `MANAGER STATUS` column are managers.

**Best Practices:**

- Always use an odd number of managers to ensure quorum.
- Spread your managers across regions / availability zones.
- In large clusters, not every node should be a manager.

For more details, see the [Docker Swarm documentation](https://docs.docker.com/engine/swarm/admin_guide/#add-manager-nodes-for-fault-tolerance).

## Deploying the Control Plane

Once your swarm is setup, use a stack definition file to deploy the Control Plane server on every node in your Docker Swarm cluster. We recommend using placement constraints in your stack definition to deploy the Control Plane server onto specific Swarm nodes.

### Creating the Stack Definition File

You can run the following command on a Swarm node to get the node ID for each node in the Docker Swarm cluster. The node with an asterisk (*) next to its ID is the node on which you're running the command.

``` sh
docker node ls
```

The output will look like this:

```sh
ID                            HOSTNAME         STATUS    AVAILABILITY   MANAGER STATUS   ENGINE VERSION
vzou89zyd4n3xz6p6jvoohqxx *   host-1           Ready     Active         Leader           28.3.3
5sa7m11ub62t1n22feuhg0mbp     host-2           Ready     Active         Reachable        28.3.3
our0m7sn7gjops9klp7j1nvu7     host-3           Ready     Active         Reachable        28.3.3
```

Given that output, the following stack definition file will deploy a single Control Plane server to each node, where each [host](concepts.md#hosts) is named sequentally (`host-1`, `host-2`, and `host-3`).

```yaml
services:
  host-1:
    image: ghcr.io/pgedge/control-plane:<< control_plane_version >>
    command: run
    environment:
      - PGEDGE_HOST_ID=host-1
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==vzou89zyd4n3xz6p6jvoohqxx
  host-2:
    image: ghcr.io/pgedge/control-plane:<< control_plane_version >>
    command: run
    environment:
      - PGEDGE_HOST_ID=host-2
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==5sa7m11ub62t1n22feuhg0mbp
  host-3:
    image: ghcr.io/pgedge/control-plane:<< control_plane_version >>
    command: run
    environment:
      - PGEDGE_HOST_ID=host-3
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==our0m7sn7gjops9klp7j1nvu7
networks:
  host:
    name: host
    external: true
```

Placement constraints in Docker Swarm are used to control where services run within your cluster. In the configuration above, each service defined under the `deploy` property specifies a placement constraint:

``` yaml
    deploy:
      placement:
        constraints:
          - node.id==our0m7sn7gjops9klp7j1nvu7
```

This tells Docker Swarm to run the service only on the node with the matching `node.id`. By setting constraints like this, you ensure that each Control Plane container is deployed to a specific host in your cluster.

!!! note

    This configuration uses the `host` network to attach each Control Plane container to the host's network interface. This automatically publishes all ports used by the container, and is required for the Control Plane to function.

!!! note

    The path to the data volume **must be the same** inside the container as it is outside the container. The Control Plane provides this path to Docker when it runs database containers, so it needs to be accessible on the host and inside the container.

#### Etcd Server vs Client Mode

By default, each Control Plane server also acts as an Etcd server. However, in
larger clusters or in clusters with an even number of nodes, some Control Plane
servers should run in client mode.

Similar to the Docker Swarm configuration, you should configure your Control
Plane cluster with three Etcd servers for clusters with up to seven hosts. For
clusters with more than seven hosts, you should have no more than five Etcd
servers.

We recommend mirroring your Docker Swarm configuration so that the Control Plane
serves Etcd on each Swarm manager node and is a client on each Swarm worker
node. Add this environment variable to a Control Plane server's service
definition to configure the client mode:

```yaml
      - PGEDGE_ETCD_MODE=client
```

For example:

```yaml
  host-4:
    image: ghcr.io/pgedge/control-plane:<< control_plane_version >>
    command: run
    environment:
      - PGEDGE_HOST_ID=host-4
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
      - PGEDGE_ETCD_MODE=client
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==g0nw8mhfox4hox1ny2fgk4x6h
```

!!! tip

    The [Stack Definition Generator](#stack-definition-generator) below can
    produce this configuration for you.

#### Stack Definition Generator

To make it easier to generate the stack definition file, you can use the generator below to create a stack definition file based on the nodes in your Docker Swarm.

First, run the following command from any node within the swarm to list the Node IDs:

```sh
docker node ls --format '{{ .ID }} {{ .ManagerStatus }}'
```

Paste the output below and click "Generate Stack." This generator is fully local to this page, and doesn't transmit any data.

<textarea id="nodes" rows="8" style="width:100%; font-family:monospace;"></textarea>

<button id="generateBtn" data-input="nodes" data-output="global-output" data-version="<< control_plane_version >>" class="md-button yaml-generate">Generate Stack Definition</button>

``` yaml {#global-output}
# Once submitted, the generated stack will appear here.
```

### Deploying the Stack

To deploy the stack definition file, copy the contents to a file named `control-plane.yaml` on a manager node and run the following command from the same directory as the file:

```sh
docker stack deploy -c control-plane.yaml control-plane
```

This creates the services and networks defined in the stack definition file.

Once the stack is deployed, the pgEdge Control Plane server will be running on each node. From there, you can proceed with initializing the Control Plane before beginning to deploy databases. 

## Initializing the Control Plane

Once the Control Plane server is deployed on all hosts, you can initialize the Control Plane. 

Each Control Plane server starts in an uninitialized state until it's added to a Control Plane cluster. In a typical configuration, you will submit a request to one Control Plane server to initialize a new cluster, then submit requests to all other servers to join them to the new cluster.

For example, to initialize a cluster with three hosts, you would:

1. Initialize the cluster on `host-1`.
2. Join `host-2` to `host-1`'s cluster.
3. Join `host-3` to `host-1`'s cluster.

To initialize a cluster, make a `GET` request to the `/v1/cluster/init`
endpoint. The response will contain a "join token", which can be provided to
other instances via a `POST` request to the `/v1/cluster/join` endpoint. Using
the same example above, the initialization steps would be:

1.  Initialize the cluster on `host-1`:

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

    We'll submit this to the other Control Plane server instances to join them to the new cluster.

2.  Join `host-2` to `host-1`'s cluster:

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

3.  Join `host-3` to `host-1`'s cluster:

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
Plane. You can *pin* to a specific version by including a version in the `image`
fields in your service specification, such as `ghcr.io/pgedge/control-plane:v0.5.0`. 

If you do not include a version, Docker will pull the
`ghcr.io/pgedge/control-plane:latest` tag by default. 

!!! note

    We recommend pinning the version in production environments so that upgrades are explicit and predictable.

### Upgrading with a Pinned Version

To upgrade from a pinned version:

1. Modify the `image` fields in your service specification to reference the new version, such as
   updating `ghcr.io/pgedge/control-plane:<< control_plane_version >>` to
   `ghcr.io/pgedge/control-plane:v0.5.0`.
2. Re-run `docker stack deploy -c control-plane.yaml control-plane` as in the
   [Deploying the stack](#deploying-the-stack) section.

### Upgrading with the `latest` Tag

By default, `docker stack deploy` will always query the registry for updates
unless you've specified a different `--resolve-image` option; updating with
the `latest` tag is a single step:

1. Re-run `docker stack deploy -c control-plane.yaml control-plane` as described in the
   [Deploying the stack](#deploying-the-stack) section.

### How to Check the Current Version

If you're not sure which version you're running, such as when you're using the
`latest` tag, you can check the version of a particular Control Plane server
with the `/v1/version` API endpoint:

=== "curl"

    ```sh
    curl http://host-1:3000/v1/version
    ```
