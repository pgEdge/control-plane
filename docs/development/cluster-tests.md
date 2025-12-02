The `clustertest` package is a lightweight testing framework for validating cluster operations and host management in the pgEdge Control Plane.

## Framework Overview

The clustertest framework uses [testcontainers-go](https://golang.testcontainers.org/) to dynamically create and manage Docker containers for control-plane hosts. This approach provides several key benefits:

- **No pre-existing infrastructure required**: Tests create their own containerized hosts on-demand
- **Complete isolation**: Each test gets its own ports, data directories, and container instances
- **Parallel execution**: Tests can run concurrently without interfering with each other
- **Automatic cleanup**: Resources are cleaned up after tests complete (configurable for debugging)
- **Realistic testing**: Multi-host clusters are tested in an environment that closely mirrors production

## Container Architecture

### testcontainers-go Integration

The framework uses **testcontainers-go v0.34.0** for container lifecycle management. Each control-plane host runs in its own container with the following configuration:

- **Network mode**: `host` - Containers use the host network stack for direct port access
- **Docker socket access**: Containers mount `/var/run/docker.sock` to enable nested container operations (the control plane spawns database containers)
- **Health checks**: HTTP-based wait strategy polls the `/v1/version` endpoint before considering a container ready
- **Startup timeout**: 10 seconds for container initialization

### Port Allocation

To prevent conflicts during parallel test execution, the framework uses dynamic port allocation:

- **Thread-safe allocation**: A global mutex protects port allocation across concurrent tests
- **OS-assigned ports**: Uses TCP listener on port 0 to let the OS assign available ports
- **Port tracking**: Allocated ports are tracked to prevent reuse within a test run
- **Etcd server mode**: Allocates 3 ports (HTTP API, etcd peer, etcd client)
- **Etcd client mode**: Allocates 1 port (HTTP API only)

### Data Persistence

Each host gets a unique data directory structure:

```
data/YYYYMMDDHHMMSS/{testname}/{hostid}/{randomhex}/
```

- **Bind mounted**: Host data directories are bind-mounted into containers
- **Debugging support**: With `-skip-cleanup`, directories persist for post-test inspection
- **Permission handling**: On Linux, cleanup uses `sudo rm -rf` since containers may change ownership

## Key Components

### Host (`host_test.go`)

The `Host` type represents a single control-plane host running in a container.

**Responsibilities:**
- Creates and configures testcontainers with appropriate environment variables
- Manages container lifecycle (start, stop, cleanup)
- Provides client configuration for API access
- Extracts container logs on test failure

**Configuration options:**
- **ID**: Custom host identifier (defaults to UUID)
- **EtcdMode**: Either `EtcdModeServer` (runs embedded etcd) or `EtcdModeClient` (connects to remote etcd)
- **ExtraEnv**: Additional environment variables for the container

**Key methods:**
- `NewHost(t, config)`: Creates and starts a new host container
- `Start(t)`: Starts a stopped container
- `Stop(t)`: Gracefully stops the container
- `ClientConfig()`: Returns HTTP client configuration for API access

### Cluster (`cluster_test.go`)

The `Cluster` type represents a multi-host cluster and provides a unified interface for cluster operations.

**Responsibilities:**
- Orchestrates multiple Host instances
- Provides a multi-server client that routes requests across all healthy hosts
- Manages cluster initialization and topology changes
- Validates cluster health

**Key methods:**
- `NewCluster(t, config)`: Creates all hosts defined in the cluster configuration
- `Init(t)`: Initializes the cluster (calls InitCluster API)
- `Add(t, hostConfig)`: Dynamically adds a new host to the running cluster
- `Remove(t, hostID)`: Removes a host from the cluster
- `AssertHealthy(t)`: Verifies all hosts are in "healthy" state
- `Client()`: Returns a multi-server client for API operations
- `Host(hostID)`: Gets a reference to a specific host

### Utilities (`utils_test.go`)

Shared helper functions used across tests:

**Port allocation:**
- `allocatePorts(t, n)`: Thread-safe allocation of n available ports

**Polling helpers:**
- `waitForTaskComplete(ctx, client, dbID, taskID, timeout)`: Polls a task until completion, failure, or timeout
- `waitForDatabaseAvailable(ctx, client, dbID, timeout)`: Polls a database until it reaches "available" state
- `verifyDatabaseHealth(ctx, client, dbID, expectedNodes)`: Validates database has expected number of healthy nodes and instances

**Data management:**
- `hostDataDir(t, hostID)`: Creates hierarchical data directory structure with random suffix

**Logging:**
- `tLog(t, args...)`: Test-scoped logging with test name prefix
- `tLogf(t, format, args...)`: Formatted test-scoped logging

## Test Lifecycle

A typical cluster test follows this lifecycle:

```
1. Test creates ClusterConfig with HostConfigs
   └─ Defines topology: number of hosts, etcd modes, custom IDs

2. NewCluster(t, config) launches containers
   └─ For each HostConfig:
       ├─ Allocates ports dynamically
       ├─ Creates data directory
       ├─ Launches testcontainer with environment variables
       ├─ Waits for health check (HTTP /v1/version)
       └─ Registers cleanup handler

3. cluster.Init(t) initializes the etcd cluster
   └─ Calls InitCluster API on first healthy host
   └─ Forms multi-host etcd cluster

4. Test performs operations
   ├─ Create databases
   ├─ Add/remove hosts
   ├─ Trigger failover/switchover
   └─ Validate cluster behavior

5. t.Cleanup() automatically tears down containers
   ├─ Extracts logs if test failed
   ├─ Terminates containers (unless -skip-cleanup)
   └─ Cleans data directories (unless -skip-cleanup)
```

## Container Configuration Details

### Image Building

By default, `TestMain` builds the control-plane image before running tests:

```bash
make -C .. goreleaser-build control-plane-images
```

- **Target**: `127.0.0.1:5000/control-plane:$CONTROL_PLANE_VERSION`
- **Build time**: ~60 seconds
- **Can be skipped**: Use `-skip-image-build` flag or provide custom `-image-tag`

### Environment Variables

Containers are configured with these environment variables:

- `PGEDGE_HOST_ID`: Host UUID or custom ID
- `PGEDGE_DATA_DIR`: Path to data directory (bind-mounted)
- `PGEDGE_HTTP__PORT`: HTTP API port
- `PGEDGE_ETCD_MODE`: Either "server" or "client" (defaults to "server")
- `PGEDGE_ETCD_SERVER__PEER_PORT`: Etcd peer port (server mode only)
- `PGEDGE_ETCD_SERVER__CLIENT_PORT`: Etcd client port (server mode only)

### Volume Mounts

Each container has two bind mounts:

1. **Data directory**: Host-specific data directory mapped to same path in container
2. **Docker socket**: `/var/run/docker.sock` - Enables control plane to spawn database containers

### Cleanup Behavior

When `-skip-cleanup` is set:
- Environment variable `TESTCONTAINERS_RYUK_DISABLED=true` prevents automatic container cleanup
- Data directories are preserved
- Useful for debugging failed tests

## Design Patterns

### Resource Isolation

Each test receives isolated resources:
- Unique ports allocated dynamically
- Separate data directories with test name and random suffix
- Independent container instances

This enables safe parallel execution without conflicts.

### Automatic Cleanup

The framework uses Go's `t.Cleanup()` to register cleanup handlers:
- Cleanup runs even if test panics or fails
- Uses separate context (background) since `t.Context()` is canceled after test completes
- 30-second timeout for graceful termination

### Polling with Timeout

Async operations use a standard polling pattern:
```go
ctx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()

ticker := time.NewTicker(2 * time.Second)
defer ticker.Stop()

for {
    select {
    case <-ctx.Done():
        return fmt.Errorf("timeout")
    case <-ticker.C:
        // Check condition, return if terminal state
    }
}
```

This pattern is used in `waitForTaskComplete`, `waitForDatabaseAvailable`, and custom test logic.

### Multi-Server Client Routing

The `Cluster` type creates a `MultiServerClient` that:
- Routes API requests across all healthy hosts
- Provides automatic failover if one host is unavailable
- Simplifies test code (no need to pick specific host)
- Updates dynamically when hosts are added/removed

## Prerequisites

Before running the tests, make sure you've started the local registry and initialized the `buildx` builder:

```sh
make start-local-registry
make buildx-init
```

## Running Tests

To run all the tests:

```sh
make test-cluster
```

To skip the image build before the tests:

```sh
make test-cluster CLUSTER_TEST_SKIP_IMAGE_BUILD=1
```

To run a specific test:

```sh
make test-cluster CLUSTER_TEST_RUN=TestRemove
```

To limit the parallelism of the tests:

```sh
make test-cluster CLUSTER_TEST_PARALLEL=4
```

To test a different image tag:

```sh
make test-cluster CLUSTER_TEST_IMAGE_TAG=ghcr.io/pgedge/control-plane:v0.5.0
```

To skip cleanup:

```sh
make test-cluster CLUSTER_TEST_SKIP_CLEANUP=1
```

To use an alternate data directory:

```sh
make test-cluster CLUSTER_TEST_DATA_DIR=/tmp
```