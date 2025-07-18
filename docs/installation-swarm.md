# Installing the Control Plane on Docker Swarm

- [Installing the Control Plane on Docker Swarm](#installing-the-control-plane-on-docker-swarm)
  - [Prerequisites](#prerequisites)
  - [Control Plane Docker Swarm stack](#control-plane-docker-swarm-stack)
    - [Example `control-plane.yaml` stack definition](#example-control-planeyaml-stack-definition)
  - [Configuration](#configuration)

## Prerequisites

- Three Linux hosts that are joined in a Docker Swarm cluster
  - See [the Docker Swarm
    tutorial](https://docs.docker.com/engine/swarm/swarm-tutorial/) for
    instructions to configure a Docker Swarm
- A volume on each host with enough space for your databases
  - This volume will be used to store configuration and data files for the
    control plane and any [database instances](../userguide.md#instances) that
    run on this host.
- Open protocols and ports between hosts. By default, these are:
  - Port `3000` TCP for HTTP communication
  - Port `2379` TCP for Etcd peer communication
  - Port `2380` TCP for Etcd client communication

## Control Plane Docker Swarm stack

The Control Plane server should run on each Swarm node. We recommend using
"placement constraints" in your stack definition to deploy each control plane
instance onto a specific Swarm node. You can run

```sh
docker node ls
```

on one of the Swarm nodes to get the node ID for each node in the Docker Swarm
cluster. The node with an asterisk (`*`) next to its ID is the node you're
running the command on.


### Example `control-plane.yaml` stack definition

This example demonstrates a [Docker Swarm
stack](https://docs.docker.com/engine/swarm/stack-deploy/) that deploys three
Control Plane instances to a three-node Docker Swarm cluster, where each
[host](../userguide.md#hosts) is named after the AWS region that the host runs
in.

> [!IMPORTANT]
> This configuration uses the `host` network to attach each Control Plane
> container to the host's network interface. This automatically publishes all
> ports used by the container, and it's required for the Control Plane to
> function.

> [!IMPORTANT]
> The path to the data volume **must be the same** inside the
> container as it is outside the container. The Control Plane provides this path
> to Docker when it runs database containers, so it needs to be accessible on
> the host and inside the container.

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

## Configuration

See the [configuration reference](./configuration.md) for descriptions of all
configuration settings.
