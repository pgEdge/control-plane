# Control Plane Version Support

pgEdge Control Plane supports deploying Postgres 16, 17, and 18 with support for managed extensions, including: 

* Spock 
* LOLOR
* Snowflake 
* pgAudit 
* PostGIS 
* pgVector

## Supported Operating Systems

Currently, the pgEdge Control Plane supports deploying databases to virtual machines and bare metal hosts using the Docker Swarm orchestrator, with both the Control Plane and Postgres instances running in containers.

With this model, the pgEdge Control Plane can be deployed to hosts running any Linux distribution supported by [Docker Engine for Linux](https://docs.docker.com/engine/install/), on either x86_64 or arm64 architectures.

We plan to support additional orchestration approaches in the near future, including direct deployment to hosts without containerization.
