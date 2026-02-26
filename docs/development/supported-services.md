# Adding a Supported Service

This guide explains how to add a new supported service type to the control plane.
MCP is the reference implementation throughout. All touch points are summarized in
the [checklist](#checklist-adding-a-new-service-type) at the end.

> [!CAUTION]
> This document is subject to change. The MCP service is not yet fully
> implemented, and several areas are still in active design:
>
> - **Configuration delivery**: How config reaches service containers (env vars,
>   config files, mounts, etc.) is still being designed and may end up being
>   service-specific.
> - **Workflow refactoring**: Some of the workflow code (e.g., service
>   provisioning) needs to be refactored based on PR feedback. This work is
>   being coordinated with the SystemD migration tasks.
>
> Treat this as a snapshot of the current architecture, not a stable contract.

## Overview

A supported service is a containerized application deployed alongside a pgEdge
database. Services are declared in `DatabaseSpec.Services`, provisioned after
the database is available, and deleted when the database is deleted.

One `ServiceSpec` produces N `ServiceInstance` records — one per `host_id` in
the spec. Each instance runs as its own Docker Swarm service on the database's
overlay network, with its own dedicated Postgres credentials.

## Design Constraint: Services Depend on the Database Only

The current architecture constrains every service to have exactly one
dependency: the parent database. There is no concept of service provisioning
ordering or dependencies between services. All services declared in a database
spec are provisioned independently and in parallel once the database is
available.

This means:

- A service cannot declare a dependency on another service
- There is no ordering guarantee between services (service A may start before
  or after service B)
- A service cannot discover another service's hostname or connection info at
  provisioning time

This constraint keeps the model simple and parallelizable, but it will likely
need to be relaxed in the future. Scaling out the AI workbench will require
multi-container service types where one component depends on another (e.g., a
web client that proxies to an API server). Supporting this will require
introducing service-to-service dependencies, provisioning ordering, and
health-gated startup — effectively a dependency graph within the service layer.

The data flow from API request to running container looks like this:

```
API Request (spec.services)
  → Validation (API layer)
  → Store spec in etcd
  → UpdateDatabase workflow
    → PlanUpdate sub-workflow
      → For each (service, host_id):
          → Resolve service image from registry
          → Validate Postgres/Spock version compatibility
          → Generate resource objects (network, user role, spec, instance, monitor)
      → Merge service resources into EndState
      → Compute plan (diff current state vs. desired state)
    → Apply plan (execute resource Create/Update/Delete)
  → Service container running
```

## API Layer

The `ServiceSpec` Goa type is defined in `api/apiv1/design/database.go`. It
lives inside the `DatabaseSpec` as an array:

```go
g.Attribute("services", g.ArrayOf(ServiceSpec), func() { ... })
```

The `ServiceSpec` type has these attributes:

| Attribute | Type | Description |
|-----------|------|-------------|
| `service_id` | `Identifier` | Unique ID for this service within the database |
| `service_type` | `String` (enum) | The type of service (e.g., `"mcp"`) |
| `version` | `String` | Semver (e.g., `"1.0.0"`) or `"latest"` |
| `host_ids` | `[]Identifier` | Which hosts should run this service (one instance per host) |
| `port` | `Int` (optional) | Host port to publish; `0` = random; omitted = not published |
| `config` | `MapOf(String, Any)` | Service-specific configuration |
| `cpus` | `Float64` (optional) | CPU limit |
| `memory` | `UInt64` (optional) | Memory limit in bytes |

To add a new service type, add its string value to the enum on `service_type`:

```go
g.Attribute("service_type", g.String, func() {
    g.Enum("mcp", "my-new-service")
})
```

The `config` attribute is intentionally `MapOf(String, Any)` so the API schema
doesn't change when new service types are added. Config structure is validated
at the application layer (see [Validation](#validation)).

After editing the design file, regenerate the API code:

```sh
make -C api generate
```

## Validation

There are two validation layers that catch different classes of errors.

### API validation (HTTP 400)

`validateServiceSpec()` in `server/internal/api/apiv1/validate.go` runs on
every API request that includes services. It performs fast, syntactic checks:

1. **service_id**: must be a valid identifier (the shared `validateIdentifier`
   function)
2. **service_type**: must be in the allowlist. Currently this is a direct check:
   ```go
   if svc.ServiceType != "mcp" {
       // error: unsupported service type
   }
   ```
   Add your new type here.
3. **version**: must match semver format or be the literal `"latest"`
4. **host_ids**: must be unique within the service (duplicate host IDs are
   rejected)
5. **config**: dispatches to a per-type config validator:
   ```go
   if svc.ServiceType == "mcp" {
       errs = append(errs, validateMCPServiceConfig(svc.Config, ...)...)
   }
   ```

The per-type config validator is where you enforce required fields, type
correctness, and service-specific constraints. For example,
`validateMCPServiceConfig()` in the same file:

- Requires `llm_provider` and `llm_model`
- Validates `llm_provider` is one of `"anthropic"`, `"openai"`, `"ollama"`
- Requires the provider-specific API key (`anthropic_api_key`, `openai_api_key`,
  or `ollama_url`) based on the chosen provider

Write a parallel `validateMyServiceConfig()` function for your service type and
add a dispatch branch in `validateServiceSpec()`.

### Workflow-time validation

`GetServiceImage()` in `server/internal/orchestrator/swarm/service_images.go` is
called during workflow execution. If the `service_type`/`version` combination is
not registered in the image registry, it returns an error that fails the
workflow task and sets the database to `"failed"` state.

This catches cases where the API validation passes (valid semver, known type)
but the specific version hasn't been registered. The E2E test
`TestProvisionMCPServiceUnsupportedVersion` in `e2e/service_provisioning_test.go`
demonstrates this: version `"99.99.99"` passes API validation but fails at
workflow time.

## Service Image Registry

The image registry maps `(serviceType, version)` pairs to container image
references. It lives in `server/internal/orchestrator/swarm/service_images.go`.

### Data structures

```go
type ServiceImage struct {
    Tag                string                  // Full image:tag reference
    PostgresConstraint *host.VersionConstraint // Optional: restrict PG versions
    SpockConstraint    *host.VersionConstraint // Optional: restrict Spock versions
}

type ServiceVersions struct {
    images map[string]map[string]*ServiceImage // serviceType → version → image
}
```

### Registering a new service type

Add your image in `NewServiceVersions()`:

```go
func NewServiceVersions(cfg config.Config) *ServiceVersions {
    versions := &ServiceVersions{...}

    // Existing MCP registration
    versions.addServiceImage("mcp", "latest", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgres-mcp:latest"),
    })

    // Your new service
    versions.addServiceImage("my-service", "1.0.0", &ServiceImage{
        Tag: serviceImageTag(cfg, "my-service:1.0.0"),
    })

    return versions
}
```

`serviceImageTag()` prepends the configured registry host
(`cfg.DockerSwarm.ImageRepositoryHost`) unless the image reference already
contains a registry prefix (detected by checking if the first path component
contains a `.`, `:`, or is `localhost`).

### Version constraints

If your service is only compatible with specific Postgres or Spock versions, set
the constraint fields:

```go
versions.addServiceImage("my-service", "1.0.0", &ServiceImage{
    Tag: serviceImageTag(cfg, "my-service:1.0.0"),
    PostgresConstraint: &host.VersionConstraint{
        Min: host.MustParseVersion("15"),
        Max: host.MustParseVersion("17"),
    },
    SpockConstraint: &host.VersionConstraint{
        Min: host.MustParseVersion("4.0.0"),
    },
})
```

`ValidateCompatibility()` checks these constraints against the database's
running versions during `GenerateServiceInstanceResources()` in the workflow.
Constraint failures produce errors like `"postgres version 14 does not satisfy
constraint >=15"`.

## Resource Lifecycle

Every service instance is represented by five resources that participate in the
standard plan/apply reconciliation cycle. These resource types are generic —
adding a new service type does **not** require new resource types.

### Dependency chain

```
Phase 1: Network (swarm.network)              — no dependencies
         ServiceUserRole (swarm.service_user_role) — no dependencies
Phase 2: ServiceInstanceSpec (swarm.service_instance_spec) — depends on Network + ServiceUserRole
Phase 3: ServiceInstance (swarm.service_instance) — depends on ServiceUserRole + ServiceInstanceSpec
Phase 4: ServiceInstanceMonitor (monitor.service_instance) — depends on ServiceInstance
```

On deletion, the order reverses: monitor first, then instance, spec, and
finally the network and user role.

### What each resource does

**Network** (`server/internal/orchestrator/swarm/network.go`): Creates a Docker
Swarm overlay network for the database. The network is shared between Postgres
instances and service instances of the same database, so it deduplicates
naturally via identifier matching (both generate the same
`"{databaseID}-database"` name). Uses the IPAM service to allocate subnets.
Runs on `ManagerExecutor`.

**ServiceUserRole** (`server/internal/orchestrator/swarm/service_user_role.go`):
Manages the Postgres user lifecycle for a service instance. On `Create`, it
generates a deterministic username via `database.GenerateServiceUsername()` (format:
`svc_{serviceID}_{hostID}`), generates a random 32-byte password, and creates a
Postgres role with `pgedge_application_read_only` privileges. On `Delete`, it
drops the role. Runs on `HostExecutor(postgresHostID)` because it needs Docker
access to the Postgres container for direct database connectivity. See
`docs/development/service-credentials.md` for full details on credential
generation.

**ServiceInstanceSpec** (`server/internal/orchestrator/swarm/service_instance_spec.go`):
A virtual resource that generates the Docker Swarm `ServiceSpec`. Its
`Refresh()`, `Create()`, and `Update()` methods all call `ServiceContainerSpec()`
to compute the spec from the current inputs. The computed spec is stored in the
`Spec` field and consumed by the `ServiceInstance` resource during deployment.
`Delete()` is a no-op. Runs on `HostExecutor(hostID)`.

**ServiceInstance** (`server/internal/orchestrator/swarm/service_instance.go`):
The resource that actually deploys the Docker Swarm service. On `Create`, it
stores an initial etcd record with `state="creating"`, then calls
`client.ServiceDeploy()` with the spec from `ServiceInstanceSpec`, waits up to
5 minutes for the service to start, and transitions the state to `"running"`. On
`Delete`, it scales the service to 0 replicas (waiting for containers to stop),
removes the Docker service, and deletes the etcd record. Runs on
`ManagerExecutor`.

**ServiceInstanceMonitor** (`server/internal/monitor/service_instance_monitor_resource.go`):
Registers (or deregisters) a health monitor for the service instance. The
monitor periodically checks the service's `/health` endpoint and updates status
in etcd. Runs on `HostExecutor(hostID)`.

### How resources are generated

`GenerateServiceInstanceResources()` in
`server/internal/orchestrator/swarm/orchestrator.go` is the entry point. It:

1. Resolves the `ServiceImage` via `GetServiceImage(serviceType, version)`
2. Validates Postgres/Spock version compatibility if constraints exist
3. Constructs the four orchestrator resources (`Network`, `ServiceUserRole`,
   `ServiceInstanceSpec`, `ServiceInstance`)
4. Serializes them to `[]*resource.ResourceData` via `resource.ToResourceData()`
5. Returns a `*database.ServiceInstanceResources` wrapper

The monitor resource is added separately in the workflow layer (see
[Workflow Integration](#workflow-integration)).

All resource types are registered in
`server/internal/orchestrator/swarm/resources.go` via
`resource.RegisterResourceType[*T](registry, ResourceTypeConstant)`, which
enables the resource framework to deserialize stored state back into typed
structs.

## Container Spec

`ServiceContainerSpec()` in `server/internal/orchestrator/swarm/service_spec.go`
builds the Docker Swarm `ServiceSpec` from a `ServiceContainerSpecOptions`
struct. The options contain everything needed to build the spec: the
`ServiceSpec`, credentials, image, network IDs, database connection info, and
placement constraints.

The generated spec configures:

- **Placement**: pins the container to a specific Swarm node via
  `node.id==<cohortMemberID>`
- **Networks**: attaches to both the default bridge network (for control plane
  access and external connectivity) and the database overlay network (for
  Postgres connectivity)
- **Port publication**: `buildServicePortConfig()` publishes port 8080 in host
  mode. If the `port` field in the spec is nil, no port is published. If it's
  0, Docker assigns a random port. If it's a specific value, that port is used.
- **Health check**: currently configured to `curl -f http://localhost:8080/health`
  with a 30s start period, 10s interval, 5s timeout, and 3 retries
- **Resource limits**: CPU and memory limits from the spec, if provided

> [!NOTE]
> How configuration reaches the service container (environment variables, config
> files, mounts, etc.) is still evolving and will vary per service type. See
> `buildServiceEnvVars()` in `service_spec.go` for the current MCP
> implementation, but expect this area to change as new service types are added.

For a new service type, you may need to:

- Extend `ServiceContainerSpecOptions` with service-specific fields
- Add branches in `ServiceContainerSpec()` or its helper functions to handle
  the new service type's requirements (different health check endpoint, different
  target port, mount points, entrypoint, etc.)

## Workflow Integration

The workflow layer is generic. No per-service-type changes are needed here.

### PlanUpdate

`PlanUpdate` in `server/internal/workflows/plan_update.go` is the sub-workflow
that computes the reconciliation plan. It:

1. Computes `NodeInstances` from the spec
2. Generates node resources (same as before services existed)
3. Determines a `postgresHostID` for `ServiceUserRole` executor routing —
   `ServiceUserRole.Create()` needs local Docker access to the Postgres
   container, so it picks the first available Postgres host
4. Iterates `spec.Services` and for each `(service, hostID)` pair, calls
   `getServiceResources()`
5. Passes both node and service resources to `operations.UpdateDatabase()`

### getServiceResources

`getServiceResources()` in the same file builds an `operations.ServiceResources`
for a single service instance:

1. Generates the `ServiceInstanceID` via
   `database.GenerateServiceInstanceID(databaseID, serviceID, hostID)`
2. Resolves the Postgres hostname via `findPostgresInstance()`, which prefers a
   co-located instance (same host as the service) for lower latency but falls
   back to any instance in the database
3. Constructs a `database.ServiceInstanceSpec` with all the inputs
4. Fires the `GenerateServiceInstanceResources` activity (executes on the
   manager queue)
5. Wraps the result in `operations.ServiceResources`, adding the
   `ServiceInstanceMonitorResource`

### EndState

`EndState()` in `server/internal/database/operations/end.go` merges service
resources into the desired end state:

```go
for _, svc := range services {
    state, err := svc.State()
    end.Merge(state)
}
```

Service resources always land in the final plan, after all node operations. This
is because intermediate states (from `UpdateNodes`, `AddNodes`,
`PopulateNodes`) only contain node diffs. `PlanAll` produces one plan per state
transition, so services end up in the last plan (the diff from the last
intermediate state to EndState).

Resources that exist in the current state but are absent from the end state are
automatically marked `PendingDeletion` by the plan engine, which generates
delete events in reverse dependency order.

## Domain Model

### ServiceSpec

`server/internal/database/spec.go`:

```go
type ServiceSpec struct {
    ServiceID   string         `json:"service_id"`
    ServiceType string         `json:"service_type"`
    Version     string         `json:"version"`
    HostIDs     []string       `json:"host_ids"`
    Config      map[string]any `json:"config"`
    Port        *int           `json:"port,omitempty"`
    CPUs        *float64       `json:"cpus,omitempty"`
    MemoryBytes *uint64        `json:"memory,omitempty"`
}
```

This is the spec-level declaration that lives inside `Spec.Services`. It's
service-type-agnostic — no fields are MCP-specific. The `Config` map holds all
service-specific settings.

### ServiceInstance

`server/internal/database/service_instance.go`:

The runtime artifact that tracks an individual container's state. Key fields:
`ServiceInstanceID`, `ServiceID`, `DatabaseID`, `HostID`, `State`
(`creating`/`running`/`failed`/`deleting`), `Status` (container ID, hostname,
IP, port mappings, health), and `Credentials` (`ServiceUser` with username and
password).

### ID generation

| Function | Format | Example |
|----------|--------|---------|
| `GenerateServiceInstanceID(dbID, svcID, hostID)` | `"{dbID}-{svcID}-{hostID}"` | `"mydb-mcp-host1"` |
| `GenerateServiceUsername(svcID, hostID)` | `"svc_{svcID}_{hostID}"` | `"svc_mcp_host1"` |
| `GenerateDatabaseNetworkID(dbID)` | `"{dbID}"` | `"mydb"` |

Usernames longer than 63 characters are truncated with a deterministic hash
suffix. See `docs/development/service-credentials.md` for details.

### ServiceResources

`server/internal/database/operations/common.go`:

```go
type ServiceResources struct {
    ServiceInstanceID string
    Resources         []*resource.ResourceData
    MonitorResource   resource.Resource
}
```

The operations-layer wrapper that bridges the orchestrator output and the
planning system. `Resources` holds the serialized orchestrator resources (from
`GenerateServiceInstanceResources`). `MonitorResource` is the
`ServiceInstanceMonitorResource`. The `State()` method merges both into a
`resource.State` for use in `EndState()`.

## Testing

### Unit tests

**Container spec tests** in
`server/internal/orchestrator/swarm/service_spec_test.go`:

Table-driven tests for `ServiceContainerSpec()` and its helpers. The test
pattern uses per-case check functions:

```go
{
    name: "basic MCP service",
    opts: &ServiceContainerSpecOptions{...},
    checks: []checkFunc{
        checkLabels(expectedLabels),
        checkNetworks("bridge", "my-db-database"),
        checkEnv("PGHOST=...", "PGPORT=5432", ...),
        checkPlacement("node.id==swarm-node-1"),
        checkHealthcheck("/health", 8080),
        checkPorts(8080, 5434),
    },
}
```

Add test cases for your new service type here, particularly if it has different
health check endpoints, ports, or other spec differences.

**Image registry tests** in
`server/internal/orchestrator/swarm/service_images_test.go`:

Tests `GetServiceImage()`, `SupportedServiceVersions()`, and
`ValidateCompatibility()`. Covers both happy path (valid type + version) and
error cases (unsupported type, unregistered version, constraint violations). Add
test cases for your new service type's image registration and any version
constraints.

### Golden plan tests

The golden plan tests in
`server/internal/database/operations/update_database_test.go` validate that
service resources are correctly integrated into the plan/apply reconciliation
cycle.

**How they work:**

1. Build a start `*resource.State` representing the current state of the world
2. Build `[]*operations.NodeResources` and `[]*operations.ServiceResources`
   representing the desired state
3. Call `operations.UpdateDatabase()` to compute the plan
4. Summarize the plans via `resource.SummarizePlans()`
5. Compare against a committed JSON golden file via `testutils.GoldenTest[T]`

**Test helpers** in `server/internal/database/operations/helpers_test.go` define
stub resource types that mirror the real swarm types' `Identifier()`,
`Dependencies()`, `DiffIgnore()`, and `Executor()` without importing the swarm
package. This avoids pulling in the Docker SDK and keeps tests self-contained.
The stubs use the `orchestratorResource` embedding pattern already established
in the file.

`makeServiceResources()` constructs a complete set of service resources for a
single service instance:

```go
func makeServiceResources(t testing.TB, databaseID, serviceID, hostID string, nodeNames []string) *operations.ServiceResources
```

It builds all four stub resources, serializes them to `[]*resource.ResourceData`,
creates the real `monitor.ServiceInstanceMonitorResource`, and returns the
`operations.ServiceResources` wrapper.

**Five standard test cases** cover the full lifecycle:

| Test case | Start state | Services | Verifies |
|-----------|-------------|----------|----------|
| `single node with service from empty` | empty | 1 service | Service resources created in correct phase order alongside database resources |
| `single node with service no-op` | node + service | same service | Unchanged services produce an empty plan (the core regression test) |
| `add service to existing database` | node only | 1 new service | Only service create events, no database changes |
| `remove service from existing database` | node + service | nil | Service delete events in reverse dependency order, database unchanged |
| `update database node with unchanged service` | node + service | same service | Only database update events, service resources untouched |

These test cases are generic and apply regardless of service type. To regenerate
golden files after changes:

```sh
go test ./server/internal/database/operations/... -run TestUpdateDatabase -update
```

Always review the generated JSON files in
`golden_test/TestUpdateDatabase/` before committing.

### E2E tests

E2E tests in `e2e/service_provisioning_test.go` validate service provisioning
against a real control plane cluster.

Build tag: `//go:build e2e_test`

The tests use `fixture.NewDatabaseFixture()` for auto-cleanup and poll the API
for state transitions. Key patterns to replicate for a new service type:

| Pattern | Example test | What it validates |
|---------|-------------|-------------------|
| Single-host provision | `TestProvisionMCPService` | Service reaches `"running"` state |
| Multi-host provision | `TestProvisionMultiHostMCPService` | One instance per host, all reach `"running"` |
| Add to existing DB | `TestUpdateDatabaseAddService` | Service added without affecting database |
| Remove from DB | `TestUpdateDatabaseRemoveService` | Empty `Services` array removes the service |
| Stability | `TestUpdateDatabaseServiceStable` | Unrelated DB update doesn't recreate service (checks `created_at` and `container_id` unchanged) |
| Bad version | `TestProvisionMCPServiceUnsupportedVersion` | Unregistered version fails task, DB goes to `"failed"` |
| Recovery | `TestProvisionMCPServiceRecovery` | Failed DB recovered by updating with valid version |

Run service E2E tests:

```sh
make test-e2e E2E_RUN=TestProvisionMCPService
```

## Checklist: Adding a New Service Type

| Step | File | Change |
|------|------|--------|
| 1. API enum | `api/apiv1/design/database.go` | Add to `g.Enum(...)` on `service_type` |
| 2. Regenerate | — | `make -C api generate` |
| 3. Validation | `server/internal/api/apiv1/validate.go` | Add type to allowlist in `validateServiceSpec()`; add `validateMyServiceConfig()` function |
| 4. Image registry | `server/internal/orchestrator/swarm/service_images.go` | Call `versions.addServiceImage()` in `NewServiceVersions()` |
| 5. Container spec | `server/internal/orchestrator/swarm/service_spec.go` | Service-specific configuration delivery, health check, mounts, entrypoint |
| 6. Unit tests | `swarm/service_spec_test.go`, `swarm/service_images_test.go` | Add cases for new type |
| 7. Golden plan tests | `operations/update_database_test.go` | Already covered generically; regenerate with `-update` if resource shape changes |
| 8. E2E tests | `e2e/service_provisioning_test.go` | Add provision, lifecycle, stability, and failure/recovery tests |

## What Doesn't Change

The following are service-type-agnostic and require no modification:

- `ServiceSpec` struct — `server/internal/database/spec.go`
- `ServiceInstance` domain model — `server/internal/database/service_instance.go`
- Workflow code — `server/internal/workflows/plan_update.go`
- Resource types — `server/internal/orchestrator/swarm/` (all five resource
  types are generic)
- `GenerateServiceInstanceResources()` —
  `server/internal/orchestrator/swarm/orchestrator.go`
- Operations layer — `server/internal/database/operations/` (`UpdateDatabase`,
  `EndState`)
- Store/etcd layer

## Future Work

- **Read/write service user accounts**: Service users are currently provisioned
  with the `pgedge_application_read_only` role. Some service types will require
  write access (`INSERT`, `UPDATE`, `DELETE`, DDL). This will require a
  mechanism for the service spec to declare the required access level and for
  `ServiceUserRole` to provision the appropriate role accordingly.
- **Primary-aware database connection routing**: Services currently connect to a
  Postgres instance resolved at provisioning time by `findPostgresInstance()`,
  which prefers a co-located instance but does not distinguish between primary
  and replica. Services that require read/write access will need their database
  connection routed to the current primary, and that routing will need to
  survive failovers and switchovers.
- **Persistent bind mounts**: Service containers currently have no persistent
  storage (`Mounts: []mount.Mount{}`). Some services need configuration or
  application state that survives container restarts (e.g., token stores, local
  databases, generated config files). This will require adding bind mount
  support to `ServiceContainerSpec()`, along with corresponding filesystem
  directory resources to manage the host-side paths — following the same pattern
  used by Patroni and pgBackRest config resources.
