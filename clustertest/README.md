# Clustertest - Testcontainers-based Testing Framework for Control Plane

Clustertest is a Go testing framework for the pgEdge Control Plane that provides isolated, programmatic cluster creation using testcontainers. It enables writing reliable, parallel-safe integration tests with full control over cluster lifecycle.

## Features

- **Isolated test clusters**: Each test gets its own cluster, preventing interference
- **Testcontainers-based**: Self-contained tests with no external dependencies
- **Flexible configuration**: Programmatic cluster setup with sensible defaults
- **Full lifecycle control**: Start, stop, pause, and restart individual hosts
- **Automatic cleanup**: Resources cleaned up automatically after tests
- **Debugging support**: Capture logs and preserve containers on failure
- **Direct etcd access**: Advanced testing via direct etcd client
- **Parallel execution**: Run tests in parallel without conflicts

## Quick Start

```go
package mypackage_test

import (
    "context"
    "testing"

    "github.com/pgEdge/control-plane/clustertest"
    "github.com/stretchr/testify/require"
)

func TestMyFeature(t *testing.T) {
    ctx := context.Background()

    // Create a 3-host cluster with auto-initialization
    cluster, err := clustertest.NewCluster(ctx, t,
        clustertest.WithHosts(3),
        clustertest.WithAutoInit(true),
    )
    require.NoError(t, err)

    // Use the cluster client to interact with the API
    client := cluster.Client()
    hosts := cluster.Hosts()

    // Create a database
    db, err := client.CreateDatabase(ctx, &client.Database{
        Name: "testdb",
        Nodes: []client.Node{
            {ID: "n1", HostID: hosts[0].ID()},
        },
        Users: []client.User{
            {Name: "admin", Password: "secret"},
        },
    })
    require.NoError(t, err)

    // Wait for database creation
    err = client.WaitForTask(ctx, db.TaskID)
    require.NoError(t, err)

    // Test your feature...
}
```

## Configuration Options

### WithHosts(count int)

Creates a cluster with the specified number of hosts using sensible defaults for etcd topology:

```go
// 1 host: single etcd server
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(1),
)

// 2-3 hosts: all etcd servers
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
)

// 4+ hosts: first 3 as etcd servers, rest as clients
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(5), // 3 servers, 2 clients
)
```

### WithHost(config HostConfig)

Add individual hosts with custom configuration:

```go
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHost(clustertest.HostConfig{
        ID:       "host-1",
        EtcdMode: clustertest.EtcdModeServer,
        ExtraEnv: map[string]string{
            "PGEDGE_LOGGING__LEVEL": "debug",
        },
    }),
    clustertest.WithHost(clustertest.HostConfig{
        ID:       "host-2",
        EtcdMode: clustertest.EtcdModeClient,
    }),
)
```

### WithAutoInit(enable bool)

Enable or disable automatic cluster initialization:

```go
// Auto-initialize (default: false)
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithAutoInit(true),
)

// Manual initialization
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithAutoInit(false),
)
err := cluster.InitializeCluster(ctx)
```

### WithKeepOnFailure(enable bool)

Preserve containers when tests fail for debugging:

```go
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithKeepOnFailure(true), // Keep on failure
)
// Container IDs will be logged if test fails
```

### WithLogCapture(enable bool)

Capture and display container logs:

```go
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithLogCapture(true), // Capture logs
)
// Logs automatically printed on cleanup
```

### WithImage(image string)

Specify the control plane Docker image:

```go
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithImage("ghcr.io/pgedge/control-plane:v1.0.0"),
)
```

## Common Patterns

### Pattern 1: Basic Database Test

```go
func TestDatabaseCreation(t *testing.T) {
    ctx := context.Background()

    cluster, err := clustertest.NewCluster(ctx, t,
        clustertest.WithHosts(3),
        clustertest.WithAutoInit(true),
    )
    require.NoError(t, err)

    cli := cluster.Client()
    hosts := cluster.Hosts()

    db, err := cli.CreateDatabase(ctx, &client.Database{
        Name: "mydb",
        Nodes: []client.Node{
            {ID: "n1", HostID: hosts[0].ID()},
        },
        Users: []client.User{
            {Name: "admin", Password: "secret"},
        },
    })
    require.NoError(t, err)
    err = cli.WaitForTask(ctx, db.TaskID)
    require.NoError(t, err)

    // Verify database
    dbInfo, err := cli.GetDatabase(ctx, "mydb")
    require.NoError(t, err)
    assert.Equal(t, client.DatabaseStateAvailable, dbInfo.State)
}
```

### Pattern 2: Host Failure Simulation

```go
func TestHostFailure(t *testing.T) {
    ctx := context.Background()

    cluster, err := clustertest.NewCluster(ctx, t,
        clustertest.WithHosts(3),
        clustertest.WithAutoInit(true),
    )
    require.NoError(t, err)

    // Create database...

    // Simulate host failure
    host := cluster.Host("host-1")
    err = host.Stop(ctx)
    require.NoError(t, err)

    // Verify cluster detects failure
    eventually(t, 30*time.Second, func() bool {
        info, _ := cluster.Client().GetHost(ctx, "host-1")
        return info.State == client.HostStateUnreachable
    })

    // Test failover behavior...

    // Restore host
    err = host.Start(ctx)
    require.NoError(t, err)
}
```

### Pattern 3: Network Partition

```go
func TestNetworkPartition(t *testing.T) {
    ctx := context.Background()

    cluster, err := clustertest.NewCluster(ctx, t,
        clustertest.WithHosts(5),
        clustertest.WithAutoInit(true),
    )
    require.NoError(t, err)

    // Partition a minority of hosts
    host := cluster.Host("host-5")
    err = host.Pause(ctx) // Freeze container
    require.NoError(t, err)

    // Majority should continue operating
    _, err = cluster.Client().GetCluster(ctx)
    require.NoError(t, err)

    // Restore partition
    err = host.Unpause(ctx)
    require.NoError(t, err)
}
```

### Pattern 4: Direct Etcd Access

```go
func TestEtcdConsistency(t *testing.T) {
    ctx := context.Background()

    cluster, err := clustertest.NewCluster(ctx, t,
        clustertest.WithHosts(3),
        clustertest.WithAutoInit(true),
    )
    require.NoError(t, err)

    // Get direct etcd client
    host := cluster.Hosts()[0]
    etcdClient, err := host.EtcdClient(ctx)
    require.NoError(t, err)

    // Query etcd directly
    resp, err := etcdClient.MemberList(ctx)
    require.NoError(t, err)
    assert.Equal(t, 3, len(resp.Members))

    // Check stored keys
    getResp, err := etcdClient.Get(ctx, "/", clientv3.WithPrefix())
    require.NoError(t, err)
    // Analyze etcd state...
}
```

## API Reference

### Cluster Type

```go
type Cluster struct {
    // Methods
    Client() client.Client
    Host(id string) *Host
    Hosts() []*Host
    Initialized() bool
    InitializeCluster(ctx context.Context) error
    Cleanup(ctx context.Context) error
    Logs(ctx context.Context) (map[string]string, error)
}
```

### Host Type

```go
type Host struct {
    // Lifecycle
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Restart(ctx context.Context) error
    Pause(ctx context.Context) error
    Unpause(ctx context.Context) error
    Terminate(ctx context.Context) error

    // Introspection
    ID() string
    APIURL() string
    APIPort() int
    Logs(ctx context.Context) (string, error)
    Exec(ctx context.Context, cmd []string) (string, error)
    EtcdClient(ctx context.Context) (*clientv3.Client, error)
}
```

## Best Practices

### 1. Test Isolation

Always create a new cluster per test to ensure isolation:

```go
// Good
func TestFeatureA(t *testing.T) {
    cluster, _ := clustertest.NewCluster(ctx, t, ...)
    // Test using this cluster
}

func TestFeatureB(t *testing.T) {
    cluster, _ := clustertest.NewCluster(ctx, t, ...)
    // Test using a separate cluster
}

// Bad - sharing clusters between tests
var sharedCluster *clustertest.Cluster
```

### 2. Use Auto-Init for Most Tests

Unless testing cluster initialization itself, use auto-init:

```go
// Preferred for most tests
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithAutoInit(true),
)

// Only skip auto-init when testing initialization
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithAutoInit(false),
)
// Test init behavior
err := cluster.InitializeCluster(ctx)
```

### 3. Enable Debugging for Flaky Tests

Use log capture and keep-on-failure for debugging:

```go
cluster, _ := clustertest.NewCluster(ctx, t,
    clustertest.WithHosts(3),
    clustertest.WithAutoInit(true),
    clustertest.WithLogCapture(true),
    clustertest.WithKeepOnFailure(true),
)
```

### 4. Use Eventually Pattern for Async Operations

```go
func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        if condition() {
            return
        }
        if time.Now().After(deadline) {
            t.Fatal("condition not met within timeout")
        }
        <-ticker.C
    }
}

// Usage
eventually(t, 30*time.Second, func() bool {
    info, _ := client.GetHost(ctx, "host-1")
    return info.State == client.HostStateHealthy
})
```

### 5. Skip Long Tests in Short Mode

```go
func TestComplexScenario(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    // Test implementation...
}

// Run all tests: go test
// Skip long tests: go test -short
```

## Common Gotchas

### 1. Host IDs Are Predictable

When using `WithHosts(n)`, host IDs are `host-1`, `host-2`, etc.:

```go
cluster, _ := clustertest.NewCluster(ctx, t, WithHosts(3))
hosts := cluster.Hosts()
// hosts[0].ID() == "host-1"
// hosts[1].ID() == "host-2"
// hosts[2].ID() == "host-3"
```

### 2. Cleanup Is Automatic

Cluster cleanup is registered with `t.Cleanup()`, so you don't need to manually defer it:

```go
// This works, but is redundant
cluster, _ := clustertest.NewCluster(ctx, t, ...)
defer cluster.Cleanup(ctx) // Not necessary

// Cleanup happens automatically when test ends
```

### 3. Context Deadlines

Use appropriate context timeouts for operations:

```go
// Too short - might timeout during slow operations
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// Better - reasonable timeout for cluster operations
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()
```

### 4. Wait for Tasks

Always wait for async tasks to complete:

```go
// Bad - task might still be running
db, err := client.CreateDatabase(ctx, spec)
require.NoError(t, err)
// Immediately try to use database - might fail

// Good - wait for task
db, err := client.CreateDatabase(ctx, spec)
require.NoError(t, err)
err = client.WaitForTask(ctx, db.TaskID)
require.NoError(t, err)
// Now database is ready
```

### 5. Etcd Topology Defaults

Understand the default etcd topology:

```go
WithHosts(1)  // 1 server, 0 clients
WithHosts(3)  // 3 servers, 0 clients
WithHosts(5)  // 3 servers, 2 clients
WithHosts(10) // 3 servers, 7 clients
```

## Troubleshooting

### Tests Hang or Timeout

- Check Docker is running and responsive
- Increase context timeouts
- Check for resource constraints (CPU, memory, disk)
- Enable log capture to see what's happening

### "Port Already in Use" Errors

- Testcontainers uses random host ports, so this shouldn't happen
- If it does, check for leftover containers: `docker ps -a`
- Clean up: `docker rm -f $(docker ps -aq)`

### Containers Not Cleaned Up

- If `WithKeepOnFailure(true)` is set, containers are preserved on failure
- Manually clean up: `docker rm -f $(docker ps -aq --filter name=cluster-test)`
- Check network: `docker network ls` and `docker network rm <name>`

### Etcd Errors

- Ensure at least one etcd server in cluster
- Check etcd logs via `host.Logs(ctx)`
- Use `host.EtcdClient(ctx)` to query etcd directly

## Performance Tips

1. **Reuse test binaries**: Build once, run multiple tests
2. **Parallel test execution**: Tests are isolated, run with `go test -parallel N`
3. **Skip in CI when needed**: Use `testing.Short()` for expensive tests
4. **Resource limits**: Configure Docker daemon with adequate resources

## Examples

See [examples_test.go](examples_test.go) for comprehensive usage examples.

## License

Same as pgEdge Control Plane project.