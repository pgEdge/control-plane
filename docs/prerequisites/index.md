# Prerequisites

Before installing the Control Plane, you must:

- Create a set of Linux hosts where you want to deploy Postgres instances; if you are deploying Control Plane on your localhost, see the [Quickstart Guide](../installation/quickstart.md):
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
