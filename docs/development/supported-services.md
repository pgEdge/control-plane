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

```text
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
| `cpus` | `String` (optional) | CPU limit; accepts SI suffix `m` (e.g., `"500m"`, `"1"`) |
| `memory` | `String` (optional) | Memory limit in SI or IEC notation (e.g., `"512M"`, `"1GiB"`) |

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
workflow task and sets the database to `"failed"` state. Note that this is
distinct from post-provision health-check failures detected by the service
instance monitor, which transition individual `ServiceInstance` records to
`"failed"` but do **not** change the parent database's state.

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

```text
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

`GenerateDatabaseNetworkID` returns the **resource identifier** used to look up
the overlay network in the resource registry. The actual Docker Swarm network
name is `"{databaseID}-database"` (set in the `Network.Name` field in
`orchestrator.go`).

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

> [!NOTE]
> The service-specific test stubs (`makeServiceResources()` and its companion
> types) are being added as part of PLAT-412. Once merged, `makeServiceResources()`
> will construct a complete set of stub resources for a single service instance,
> serialize them to `[]*resource.ResourceData`, create the real
> `monitor.ServiceInstanceMonitorResource`, and return the
> `operations.ServiceResources` wrapper.

**Five standard test cases** will cover the full lifecycle:

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

## API Usage & Verification

This section shows how services look in the API request and response payloads.
Use these examples to verify your integration or to hand-test with `curl`.

### Creating a Database with a Service

`POST /v1/databases`

```json
{
  "id": "my-app",
  "spec": {
    "database_name": "storefront",
    "port": 5432,
    "database_users": [
      {
        "username": "admin",
        "password": "secret",
        "db_owner": true,
        "attributes": ["LOGIN", "SUPERUSER"]
      }
    ],
    "nodes": [
      {
        "name": "n1",
        "host_ids": ["host-1"]
      }
    ],
    "services": [
      {
        "service_id": "mcp-server",
        "service_type": "mcp",
        "version": "latest",
        "host_ids": ["host-1"],
        "port": 8080,
        "config": {
          "llm_provider": "anthropic",
          "llm_model": "claude-sonnet-4-5",
          "anthropic_api_key": "sk-ant-..."
        }
      }
    ]
  }
}
```

A successful response returns **HTTP 200** with a `task` object you can poll
for completion.

### Validation Error Response

If the request payload fails validation, the API returns **HTTP 400** with an
`APIError`. All service validation errors use the `invalid_input` error name.

Example — missing a required MCP config field:

```json
{
  "name": "invalid_input",
  "message": "services[0].config: missing required field 'llm_provider'"
}
```

Other common validation errors:

| Condition | Example `message` |
|-----------|-------------------|
| Duplicate `service_id` | `services[1]: service IDs must be unique within a database` |
| Unsupported `service_type` | `services[0].service_type: unsupported service type 'foo' (only 'mcp' is currently supported)` |
| Bad `version` format | `services[0].version: version must be in semver format (e.g., '1.0.0') or 'latest'` |
| Missing provider API key | `services[0].config: missing required field 'anthropic_api_key' for anthropic provider` |
| Unsupported `llm_provider` | `services[0].config[llm_provider]: unsupported llm_provider 'foo' (must be one of: anthropic, openai, ollama)` |

### Reading a Database with Service Instances

`GET /v1/databases/my-app` returns the full database, including runtime
`service_instances`:

```json
{
  "id": "my-app",
  "state": "available",
  "created_at": "2025-06-01T12:00:00Z",
  "updated_at": "2025-06-01T12:05:00Z",
  "spec": {
    "services": [
      {
        "service_id": "mcp-server",
        "service_type": "mcp",
        "version": "latest",
        "host_ids": ["host-1"],
        "port": 8080,
        "config": {
          "llm_provider": "anthropic",
          "llm_model": "claude-sonnet-4-5"
        }
      }
    ]
  },
  "service_instances": [
    {
      "service_instance_id": "my-app-mcp-server-host-1",
      "service_id": "mcp-server",
      "database_id": "my-app",
      "host_id": "host-1",
      "state": "running",
      "status": {
        "container_id": "a1b2c3d4e5f6",
        "image_version": "latest",
        "addresses": [
            "10.0.1.5",
            "mcp-server-host-1.internal"
        ],
        "ports": [
          {
            "name": "http",
            "container_port": 8080,
            "host_port": 8080
          }
        ],
        "health_check": {
          "status": "healthy",
          "message": "Service responding normally",
          "checked_at": "2025-06-01T12:04:50Z"
        },
        "last_health_at": "2025-06-01T12:04:50Z",
        "service_ready": true
      },
      "created_at": "2025-06-01T12:00:30Z",
      "updated_at": "2025-06-01T12:01:00Z"
    }
  ]
}
```

> [!NOTE]
> Sensitive config keys (`anthropic_api_key`, `openai_api_key`, and any key
> matching patterns like `password`, `secret`, `token`, `credential`,
> `private_key`, `access_key`) are **stripped from all API responses**. The
> `config` object in `spec.services` will only contain non-sensitive keys.

### Updating Services on an Existing Database

Services are managed through the `spec.services` array in
`PUT /v1/databases/{id}`. The control plane diffs the desired state against
the current state and creates, updates, or deletes service instances
accordingly.

**Add a service** — include it in the `services` array:

```json
{
  "spec": {
    "services": [
      {
        "service_id": "mcp-server",
        "service_type": "mcp",
        "version": "latest",
        "host_ids": ["host-1"],
        "config": {
          "llm_provider": "anthropic",
          "llm_model": "claude-sonnet-4-5",
          "anthropic_api_key": "sk-ant-..."
        }
      },
      {
        "service_id": "mcp-analytics",
        "service_type": "mcp",
        "version": "1.0.0",
        "host_ids": ["host-2"],
        "config": {
          "llm_provider": "openai",
          "llm_model": "gpt-4",
          "openai_api_key": "sk-..."
        }
      }
    ]
  }
}
```

**Remove a service** — omit it from the `services` array. The control plane
deletes the corresponding service instances:

```json
{
  "spec": {
    "services": []
  }
}
```

**Update a service** — change fields (e.g., `version`, `config`, `host_ids`)
in the existing entry. The control plane will update or recreate service
instances as needed.

### Service Health & Failure in API State

Service instance health is tracked by the **service instance monitor**, which
periodically polls the Docker Swarm orchestrator for container status. The
results are reflected in the `service_instances` array of the database
response.

**State lifecycle:**

| State | Meaning |
|-------|---------|
| `creating` | Instance provisioned; waiting for container to become healthy (up to 5 min timeout) |
| `running` | Container is healthy and accepting requests |
| `failed` | Container health check failed, creation timed out, or container disappeared |
| `deleting` | Instance is being removed |

**How failures surface:**

- When a `running` instance's health check fails, the monitor transitions it
  to `failed` and populates the `error` field with a diagnostic message (e.g.,
  `"container is no longer healthy"`).
- When a `creating` instance exceeds the 5-minute creation timeout, it
  transitions to `failed` with an error like
  `"creation timeout after 5m0s - container not healthy"`.
- If the container disappears entirely (nil status from orchestrator), a
  `running` instance transitions to `failed` after a 30-second grace period
  with `"container status not available"`.

Example of a failed service instance in the API response:

```json
{
  "service_instance_id": "my-app-mcp-server-host-1",
  "service_id": "mcp-server",
  "database_id": "my-app",
  "host_id": "host-1",
  "state": "failed",
  "status": null,
  "error": "creation timeout after 5m0s - no status available",
  "created_at": "2025-06-01T12:00:30Z",
  "updated_at": "2025-06-01T12:05:30Z"
}
```

> [!NOTE]
> Service instance failures detected by the monitor do **not** automatically
> change the parent database's `state`. A database can be `"available"` while
> one or more of its service instances are `"failed"`. This is distinct from
> provisioning/workflow failures (e.g., an unregistered image version), which
> do set the database to `"failed"` because the workflow itself fails. Monitor
> both `database.state` and `service_instances[].state` for a complete health
> picture.

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

## Appendix: MCP Reference Implementation for AI-Assisted Development

> [!NOTE]
> This section is designed for consumption by Claude Code (or similar AI
> assistants). When a developer asks Claude to help add a new service type, point
> it at this document. The code below provides complete, copy-editable reference
> implementations from MCP at each touch point, so the assistant can work from
> concrete examples rather than having to read every source file independently.

### A.1 API Enum (`api/apiv1/design/database.go`)

The `ServiceSpec` Goa type. To add a new service type, add it to the `g.Enum()`
call on `service_type`:

```go
// lines 125–191
var ServiceSpec = g.Type("ServiceSpec", func() {
    g.Attribute("service_id", Identifier, func() {
        g.Description("The unique identifier for this service.")
        g.Example("mcp-server")
        g.Example("analytics-service")
        g.Meta("struct:tag:json", "service_id")
    })
    g.Attribute("service_type", g.String, func() {
        g.Description("The type of service to run.")
        g.Enum("mcp") // ← add new type here, e.g. g.Enum("mcp", "my-service")
        g.Example("mcp")
        g.Meta("struct:tag:json", "service_type")
    })
    g.Attribute("version", g.String, func() {
        g.Description("The version of the service in semver format (e.g., '1.0.0') or the literal 'latest'.")
        g.Pattern(serviceVersionPattern) // `^\d+\.\d+\.\d+|latest$`
        g.Example("1.0.0")
        g.Example("latest")
        g.Meta("struct:tag:json", "version")
    })
    g.Attribute("host_ids", HostIDs, func() {
        g.Description("The IDs of the hosts that should run this service.")
        g.MinLength(1)
        g.Meta("struct:tag:json", "host_ids")
    })
    g.Attribute("port", g.Int, func() {
        g.Description("The port to publish the service on the host.")
        g.Minimum(0)
        g.Maximum(65535)
        g.Meta("struct:tag:json", "port,omitempty")
    })
    g.Attribute("config", g.MapOf(g.String, g.Any), func() {
        g.Description("Service-specific configuration.")
        g.Meta("struct:tag:json", "config")
    })
    g.Attribute("cpus", g.String, func() {
        g.Description("CPU limit. Accepts SI suffix 'm', e.g. '500m'.")
        g.Pattern(cpuPattern)
        g.Meta("struct:tag:json", "cpus,omitempty")
    })
    g.Attribute("memory", g.String, func() {
        g.Description("Memory limit in SI or IEC notation, e.g. '512M', '1GiB'.")
        g.MaxLength(16)
        g.Meta("struct:tag:json", "memory,omitempty")
    })

    g.Required("service_id", "service_type", "version", "host_ids", "config")
})
```

After editing, run `make -C api generate`.

### A.2 Validation (`server/internal/api/apiv1/validate.go`)

**`validateServiceSpec()`** — the dispatcher. Add your type to the allowlist and
config dispatch:

```go
// lines 229–280
func validateServiceSpec(svc *api.ServiceSpec, path []string) []error {
    var errs []error

    serviceIDPath := appendPath(path, "service_id")
    errs = append(errs, validateIdentifier(string(svc.ServiceID), serviceIDPath))

    // ← add your type to this check
    if svc.ServiceType != "mcp" {
        err := fmt.Errorf("unsupported service type '%s' (only 'mcp' is currently supported)", svc.ServiceType)
        errs = append(errs, newValidationError(err, appendPath(path, "service_type")))
    }

    if svc.Version != "latest" && !semverPattern.MatchString(svc.Version) {
        err := errors.New("version must be in semver format (e.g., '1.0.0') or 'latest'")
        errs = append(errs, newValidationError(err, appendPath(path, "version")))
    }

    seenHostIDs := make(ds.Set[string], len(svc.HostIds))
    for i, hostID := range svc.HostIds {
        hostIDStr := string(hostID)
        hostIDPath := appendPath(path, "host_ids", arrayIndexPath(i))
        errs = append(errs, validateIdentifier(hostIDStr, hostIDPath))
        if seenHostIDs.Has(hostIDStr) {
            err := errors.New("host IDs must be unique within a service")
            errs = append(errs, newValidationError(err, hostIDPath))
        }
        seenHostIDs.Add(hostIDStr)
    }

    // ← add dispatch for your type here
    if svc.ServiceType == "mcp" {
        errs = append(errs, validateMCPServiceConfig(svc.Config, appendPath(path, "config"))...)
    }

    if svc.Cpus != nil {
        errs = append(errs, validateCPUs(svc.Cpus, appendPath(path, "cpus"))...)
    }
    if svc.Memory != nil {
        errs = append(errs, validateMemory(svc.Memory, appendPath(path, "memory"))...)
    }

    return errs
}
```

**`validateMCPServiceConfig()`** — the per-type config validator to use as a
template:

```go
// lines 283–330
func validateMCPServiceConfig(config map[string]any, path []string) []error {
    var errs []error

    requiredFields := []string{"llm_provider", "llm_model"}
    for _, field := range requiredFields {
        if _, ok := config[field]; !ok {
            err := fmt.Errorf("missing required field '%s'", field)
            errs = append(errs, newValidationError(err, path))
        }
    }

    if val, exists := config["llm_provider"]; exists {
        provider, ok := val.(string)
        if !ok {
            err := errors.New("llm_provider must be a string")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("llm_provider"))))
        } else {
            validProviders := []string{"anthropic", "openai", "ollama"}
            if !slices.Contains(validProviders, provider) {
                err := fmt.Errorf("unsupported llm_provider '%s' (must be one of: %s)",
                    provider, strings.Join(validProviders, ", "))
                errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("llm_provider"))))
            }

            switch provider {
            case "anthropic":
                if _, ok := config["anthropic_api_key"]; !ok {
                    err := errors.New("missing required field 'anthropic_api_key' for anthropic provider")
                    errs = append(errs, newValidationError(err, path))
                }
            case "openai":
                if _, ok := config["openai_api_key"]; !ok {
                    err := errors.New("missing required field 'openai_api_key' for openai provider")
                    errs = append(errs, newValidationError(err, path))
                }
            case "ollama":
                if _, ok := config["ollama_url"]; !ok {
                    err := errors.New("missing required field 'ollama_url' for ollama provider")
                    errs = append(errs, newValidationError(err, path))
                }
            }
        }
    }

    return errs
}
```

### A.3 Image Registry (`server/internal/orchestrator/swarm/service_images.go`)

Register the image in `NewServiceVersions()`:

```go
// lines 39–68
func NewServiceVersions(cfg config.Config) *ServiceVersions {
    versions := &ServiceVersions{
        cfg:    cfg,
        images: make(map[string]map[string]*ServiceImage),
    }

    // MCP service versions
    versions.addServiceImage("mcp", "latest", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgres-mcp:latest"),
    })

    // ← add your service here:
    // versions.addServiceImage("my-service", "1.0.0", &ServiceImage{
    //     Tag: serviceImageTag(cfg, "my-service:1.0.0"),
    // })

    return versions
}
```

Supporting types:

```go
type ServiceImage struct {
    Tag                string                  `json:"tag"`
    PostgresConstraint *host.VersionConstraint `json:"postgres_constraint,omitempty"`
    SpockConstraint    *host.VersionConstraint `json:"spock_constraint,omitempty"`
}

// GetServiceImage resolves (serviceType, version) → *ServiceImage.
// Returns an error if the type or version is unregistered.
func (sv *ServiceVersions) GetServiceImage(serviceType string, version string) (*ServiceImage, error) {
    versionMap, ok := sv.images[serviceType]
    if !ok {
        return nil, fmt.Errorf("unsupported service type %q", serviceType)
    }
    image, ok := versionMap[version]
    if !ok {
        return nil, fmt.Errorf("unsupported version %q for service type %q", version, serviceType)
    }
    return image, nil
}

// serviceImageTag prepends the configured registry host unless the image
// reference already contains a registry prefix.
func serviceImageTag(cfg config.Config, imageRef string) string {
    if strings.Contains(imageRef, "/") {
        parts := strings.Split(imageRef, "/")
        firstPart := parts[0]
        if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") || firstPart == "localhost" {
            return imageRef
        }
    }
    if cfg.DockerSwarm.ImageRepositoryHost == "" {
        return imageRef
    }
    return fmt.Sprintf("%s/%s", cfg.DockerSwarm.ImageRepositoryHost, imageRef)
}
```

### A.4 Container Spec (`server/internal/orchestrator/swarm/service_spec.go`)

**`ServiceContainerSpecOptions`** — the input struct:

```go
// lines 15–32
type ServiceContainerSpecOptions struct {
    ServiceSpec       *database.ServiceSpec
    ServiceInstanceID string
    DatabaseID        string
    DatabaseName      string
    HostID            string
    ServiceName       string
    Hostname          string
    CohortMemberID    string
    ServiceImage      *ServiceImage
    Credentials       *database.ServiceUser
    DatabaseNetworkID string
    DatabaseHost      string
    DatabasePort      int
    Port              *int
}
```

**`ServiceContainerSpec()`** — builds the Docker Swarm `ServiceSpec`:

```go
// lines 35–117
func ServiceContainerSpec(opts *ServiceContainerSpecOptions) (swarm.ServiceSpec, error) {
    labels := map[string]string{
        "pgedge.component":           "service",
        "pgedge.service.instance.id": opts.ServiceInstanceID,
        "pgedge.service.id":          opts.ServiceSpec.ServiceID,
        "pgedge.database.id":         opts.DatabaseID,
        "pgedge.host.id":             opts.HostID,
    }

    networks := []swarm.NetworkAttachmentConfig{
        {Target: "bridge"},
        {Target: opts.DatabaseNetworkID},
    }

    env := buildServiceEnvVars(opts)
    image := opts.ServiceImage.Tag
    ports := buildServicePortConfig(opts.Port)

    var resources *swarm.ResourceRequirements
    if opts.ServiceSpec.CPUs != nil || opts.ServiceSpec.MemoryBytes != nil {
        resources = &swarm.ResourceRequirements{
            Limits: &swarm.Limit{},
        }
        if opts.ServiceSpec.CPUs != nil {
            resources.Limits.NanoCPUs = int64(*opts.ServiceSpec.CPUs * 1e9)
        }
        if opts.ServiceSpec.MemoryBytes != nil {
            resources.Limits.MemoryBytes = int64(*opts.ServiceSpec.MemoryBytes)
        }
    }

    return swarm.ServiceSpec{
        TaskTemplate: swarm.TaskSpec{
            ContainerSpec: &swarm.ContainerSpec{
                Image:    image,
                Labels:   labels,
                Hostname: opts.Hostname,
                Env:      env,
                Healthcheck: &container.HealthConfig{
                    Test:        []string{"CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"},
                    StartPeriod: time.Second * 30,
                    Interval:    time.Second * 10,
                    Timeout:     time.Second * 5,
                    Retries:     3,
                },
                Mounts: []mount.Mount{},
            },
            Networks: networks,
            Placement: &swarm.Placement{
                Constraints: []string{
                    "node.id==" + opts.CohortMemberID,
                },
            },
            Resources: resources,
        },
        EndpointSpec: &swarm.EndpointSpec{
            Mode:  swarm.ResolutionModeVIP,
            Ports: ports,
        },
        Annotations: swarm.Annotations{
            Name:   opts.ServiceName,
            Labels: labels,
        },
    }, nil
}
```

**`buildServiceEnvVars()`** — MCP-specific env var injection (this is expected to
change; see the caution at the top of this document):

```go
// lines 120–168
func buildServiceEnvVars(opts *ServiceContainerSpecOptions) []string {
    env := []string{
        fmt.Sprintf("PGHOST=%s", opts.DatabaseHost),
        fmt.Sprintf("PGPORT=%d", opts.DatabasePort),
        fmt.Sprintf("PGDATABASE=%s", opts.DatabaseName),
        "PGSSLMODE=prefer",
        fmt.Sprintf("PGEDGE_SERVICE_ID=%s", opts.ServiceSpec.ServiceID),
        fmt.Sprintf("PGEDGE_DATABASE_ID=%s", opts.DatabaseID),
    }

    if opts.Credentials != nil {
        env = append(env,
            fmt.Sprintf("PGUSER=%s", opts.Credentials.Username),
            fmt.Sprintf("PGPASSWORD=%s", opts.Credentials.Password),
        )
    }

    // MCP-specific config → env var mapping
    if provider, ok := opts.ServiceSpec.Config["llm_provider"].(string); ok {
        env = append(env, fmt.Sprintf("PGEDGE_LLM_PROVIDER=%s", provider))
    }
    if model, ok := opts.ServiceSpec.Config["llm_model"].(string); ok {
        env = append(env, fmt.Sprintf("PGEDGE_LLM_MODEL=%s", model))
    }

    if provider, ok := opts.ServiceSpec.Config["llm_provider"].(string); ok {
        switch provider {
        case "anthropic":
            if key, ok := opts.ServiceSpec.Config["anthropic_api_key"].(string); ok {
                env = append(env, fmt.Sprintf("PGEDGE_ANTHROPIC_API_KEY=%s", key))
            }
        case "openai":
            if key, ok := opts.ServiceSpec.Config["openai_api_key"].(string); ok {
                env = append(env, fmt.Sprintf("PGEDGE_OPENAI_API_KEY=%s", key))
            }
        case "ollama":
            if url, ok := opts.ServiceSpec.Config["ollama_url"].(string); ok {
                env = append(env, fmt.Sprintf("PGEDGE_OLLAMA_URL=%s", url))
            }
        }
    }

    return env
}
```

**`buildServicePortConfig()`** — port publication:

```go
// lines 175–196
func buildServicePortConfig(port *int) []swarm.PortConfig {
    if port == nil {
        return nil
    }
    config := swarm.PortConfig{
        PublishMode: swarm.PortConfigPublishModeHost,
        TargetPort:  8080,
        Name:        "http",
        Protocol:    swarm.PortConfigProtocolTCP,
    }
    if *port > 0 {
        config.PublishedPort = uint32(*port)
    } else if *port == 0 {
        config.PublishedPort = 0
    }
    return []swarm.PortConfig{config}
}
```

### A.5 Domain Model (`server/internal/database/spec.go`)

```go
// lines 116–125
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

This struct is service-type-agnostic. No changes needed when adding a new type.

### A.6 E2E Test Pattern (`e2e/service_provisioning_test.go`)

Complete example of provisioning a service and verifying it reaches `"running"`
state:

```go
// lines 18–147
func TestProvisionMCPService(t *testing.T) {
    t.Parallel()

    host1 := fixture.HostIDs()[0]

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    t.Log("Creating database with MCP service")

    db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
        Spec: &controlplane.DatabaseSpec{
            DatabaseName: "test_mcp_service",
            DatabaseUsers: []*controlplane.DatabaseUserSpec{
                {
                    Username:   "admin",
                    Password:   pointerTo("testpassword"),
                    DbOwner:    pointerTo(true),
                    Attributes: []string{"LOGIN", "SUPERUSER"},
                },
            },
            Port: pointerTo(0),
            Nodes: []*controlplane.DatabaseNodeSpec{
                {
                    Name:    "n1",
                    HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
                },
            },
            Services: []*controlplane.ServiceSpec{
                {
                    ServiceID:   "mcp-server",
                    ServiceType: "mcp",
                    Version:     "latest",
                    HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
                    Config: map[string]any{
                        "llm_provider":      "anthropic",
                        "llm_model":         "claude-sonnet-4-5",
                        "anthropic_api_key": "sk-ant-test-key-12345",
                    },
                },
            },
        },
    })

    t.Log("Database created, verifying service instances")

    require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
    require.Len(t, db.ServiceInstances, 1, "Expected 1 service instance")

    serviceInstance := db.ServiceInstances[0]
    assert.Equal(t, "mcp-server", serviceInstance.ServiceID)
    assert.Equal(t, string(host1), serviceInstance.HostID)
    assert.NotEmpty(t, serviceInstance.ServiceInstanceID)

    validStates := []string{"creating", "running"}
    assert.Contains(t, validStates, serviceInstance.State)

    // Poll until running
    if serviceInstance.State == "creating" {
        t.Log("Service is still creating, waiting for running...")
        maxWait := 5 * time.Minute
        pollInterval := 5 * time.Second
        deadline := time.Now().Add(maxWait)

        for time.Now().Before(deadline) {
            err := db.Refresh(ctx)
            require.NoError(t, err)
            if len(db.ServiceInstances) > 0 && db.ServiceInstances[0].State == "running" {
                break
            }
            time.Sleep(pollInterval)
        }

        require.Len(t, db.ServiceInstances, 1)
        assert.Equal(t, "running", db.ServiceInstances[0].State)
    }

    // Verify status/connection info
    serviceInstance = db.ServiceInstances[0]
    if serviceInstance.Status != nil {
        assert.NotNil(t, serviceInstance.Status.Hostname)
        assert.NotNil(t, serviceInstance.Status.Ipv4Address)

        foundHTTPPort := false
        for _, port := range serviceInstance.Status.Ports {
            if port.Name == "http" && port.ContainerPort != nil && *port.ContainerPort == 8080 {
                foundHTTPPort = true
                break
            }
        }
        assert.True(t, foundHTTPPort, "HTTP port (8080) should be configured")
    }
}
```

### A.7 Step-by-Step Instructions for Adding a New Service

When a developer asks you to add a new service type (e.g., `"my-service"`),
follow these steps in order:

1. **`api/apiv1/design/database.go`**: Add `"my-service"` to the `g.Enum()`
   call on `service_type` (see [A.1](#a1-api-enum-apiapiv1designdatabasego)).
   Run `make -C api generate`.

2. **`server/internal/api/apiv1/validate.go`**: Change the allowlist check in
   `validateServiceSpec()` to accept `"my-service"`. Write a
   `validateMyServiceConfig()` function following the pattern in
   `validateMCPServiceConfig()` (see [A.2](#a2-validation-serverinternalapiapiv1validatego)).
   Add a dispatch branch in `validateServiceSpec()`.

3. **`server/internal/orchestrator/swarm/service_images.go`**: Add a
   `versions.addServiceImage("my-service", ...)` call in `NewServiceVersions()`
   (see [A.3](#a3-image-registry-serverinternalorchestratorswarmservice_imagesgo)).

4. **`server/internal/orchestrator/swarm/service_spec.go`**: If your service
   needs different environment variables, a different health check endpoint,
   different port, or bind mounts, modify `ServiceContainerSpec()` and/or its
   helpers to branch on `ServiceType` (see
   [A.4](#a4-container-spec-serverinternalorchestratorswarmservice_specgo)).

5. **`server/internal/orchestrator/swarm/service_spec_test.go`**: Add table-driven
   test cases for the new service type's container spec, env vars, and port config.

6. **`server/internal/orchestrator/swarm/service_images_test.go`**: Add test
   cases for `GetServiceImage()` with the new type and version(s).

7. **`e2e/service_provisioning_test.go`**: Add E2E tests following the patterns
   in [A.6](#a6-e2e-test-pattern-e2eservice_provisioning_testgo): single-host
   provision, multi-host, add/remove from existing database, stability (unrelated
   update doesn't recreate), bad version failure + recovery.

**Files that do NOT need changes**: `spec.go`, `service_instance.go`,
`plan_update.go`, `orchestrator.go`, `resources.go`, `end.go`, any store/etcd
code.
