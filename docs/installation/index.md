# Installing Control Plane

The Control Plane simplifies deployment of high-availability Postgres clusters in an easy-to-manage environment, running on bare metal, in VMs, or in the cloud.

## Choosing a Deployment Method

The Control Plane supports two orchestration models. Pick the one that fits your infrastructure:

| | [systemd](systemd-installation.md) | [Docker Swarm](swarm-installation.md) |
|---|---|---|
| **How it works** | Control Plane and Postgres run as native Linux services | Control Plane and Postgres run as Docker containers |
| **Best for** | Bare metal or VMs without Docker | Container-based infrastructure |
| **Package format** | RPM or Deb system packages | Docker image |

## Installation Guides

* The [Quickstart guide](quickstart.md) deploys a three-node distributed Postgres database on your local host — the fastest way to try out Control Plane.

* [Installing via System Packages](systemd-installation.md) covers installing the Control Plane as a native Linux service using RPM or Deb packages.

* [Installing via Docker Swarm](swarm-installation.md) covers deploying the Control Plane as Docker containers across a set of hosts. 
