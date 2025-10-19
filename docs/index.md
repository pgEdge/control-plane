# pgEdge Control Plane

The **pgEdge Control Plane** is a distributed application that creates and manages
PostgreSQL databases with pgEdge's multi-active replication technology. In its
default configuration, it uses an embedded Etcd server to store configuration
and coordinate database operations with other instances. You can interact with
the Control Plane via an HTTP API. Once you've initialized a Control Plane
cluster, you can submit your requests to any Control Plane instance in the
cluster.

Most Control Plane API operations, such as database modifications, are
idempotent. If an operation fails, you can safely retry the operation after
resolving the underlying issue.

Currently, the Control Plane can deploy databases to Docker Swarm. We plan to
support other orchestrators, like Kubernetes, and bare metal/VMs in the future.