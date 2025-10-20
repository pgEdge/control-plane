# Installing the pgEdge Control Plane

This document  instructions for deploying the Control Plane on each host. The Control Plane manages and orchestrates Postgres database instances across these hosts.

## Prerequisites

- Three Linux hosts that are joined in a Docker Swarm cluster
    - See [the Docker Swarm
      tutorial](https://docs.docker.com/engine/swarm/swarm-tutorial/) for
      instructions to configure a Docker Swarm
- A volume on each host with enough space for your databases
    - This volume will be used to store configuration and data files for the
      control plane and any [database instances](concepts.md#instances) that
      run on this host.
- Open protocols and ports between hosts. By default, these are:
    - Port `3000` TCP for HTTP communication
    - Port `2379` TCP for etcd peer communication
    - Port `2380` TCP for etcd client communication

## Deploying the Control Plane on Docker Swarm

The Control Plane server should run on each Swarm node. We recommend using
"placement constraints" in your stack definition to deploy each control plane
instance onto a specific Swarm node. 

You can run the following comamnd on one of the Swarm nodes to get the node ID for each node in the Docker Swarm cluster. The node with an asterisk (`*`) next to its ID is the node you're running the command on.

```sh
docker node ls
```

### Example `control-plane.yaml` stack definition

This example demonstrates a [Docker Swarm
stack](https://docs.docker.com/engine/swarm/stack-deploy/) that deploys three
Control Plane instances to a three node Docker Swarm cluster, where each
[host](concepts.md#hosts) is named after the AWS region that the host runs
in.


!!! note

    This configuration uses the `host` network to attach each Control Plane
    container to the host's network interface. This automatically publishes all
    ports used by the container, and it's required for the Control Plane to
    function.

!!! note

    The path to the data volume **must be the same** inside the
    container as it is outside the container. The Control Plane provides this path
    to Docker when it runs database containers, so it needs to be accessible on
    the host and inside the container.

```yaml
services:
  us-east-1:
    image: ghcr.io/pgedge/control-plane
    command: run
    environment:
      - PGEDGE_CLUSTER_ID=production
      - PGEDGE_HOST_ID=us-east-1
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
  eu-central-1:
    image: ghcr.io/pgedge/control-plane
    command: run
    environment:
      - PGEDGE_CLUSTER_ID=production
      - PGEDGE_HOST_ID=eu-central-1
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
  ap-south-1:
    image: ghcr.io/pgedge/control-plane
    command: run
    environment:
      - PGEDGE_CLUSTER_ID=production
      - PGEDGE_HOST_ID=ap-south-1
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

### Deploying the stack

To deploy the stack using the example `control-plane.yaml`, run the following command on a Swarm manager node:

```sh
docker stack deploy -c control-plane.yaml control-plane
```

This will create the services and networks defined in the stack file.

Once the pgEdge Control Plane server is deployed, you must initialize the Control Plane cluster before beginning to deploy databases. See [Initializing a Control Plane cluster](guides/initialization.md) for more information.

## Configuration

Additional configuration settings are supported when deploying the pgEdge Control Plane. See the [configuration reference](./configuration.md) for descriptions of all configuration settings.
