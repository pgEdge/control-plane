# Control Plane Version Support

pgEdge Control Plane supports deploying Postgres 16, 17, and 18 with support for managed extensions, including: 

* Spock 
* LOLOR
* Snowflake 
* pgAudit 
* PostGIS 
* pgVector

## Supported Operating Systems

The pgEdge Control Plane supports two orchestration models:

**Docker Swarm**: deploys the Control Plane and Postgres instances as Docker containers. Supported on any Linux distribution supported by [Docker Engine for Linux](https://docs.docker.com/engine/install/), on either x86_64 or arm64 architectures.

**systemd**: deploys the Control Plane and Postgres instances as native Linux services without Docker. Supported on RPM-based distributions (RHEL, Rocky Linux, AlmaLinux) and Debian-based distributions (Debian, Ubuntu), on either x86_64 or arm64 architectures. See [Installing via System Packages](installation/systemd-installation.md) for details and current limitations.
