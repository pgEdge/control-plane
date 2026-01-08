# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The pgEdge Control Plane is a distributed system for managing and orchestrating Postgres databases. It uses Docker Swarm to deploy databases as Docker containers, however it is architected to support other orchestrators in the future. It uses an embedded Etcd server for distributed state management, and provides declarative APIs for database lifecycle management. The system supports multi-active deployments with Spock replication, read replicas via Patroni, and backup/restore operations through pgBackRest.

## Common Development Commands

### Local Development
```bash
# Start local Docker compose-based development environment in foreground with hot reload
make dev-watch

# Rebuild and restart after changes
make dev-build

# Stop development environment
make dev-down

# Complete teardown (removes databases, networks, data)
make dev-teardown
```

### Testing
```bash
# Run unit tests
make test

# Run unit tests with specific package
go test ./server/internal/database/...

# Run etcd lifecycle tests
make test-etcd-lifecycle

# Run workflow backend tests
make test-workflows-backend

# Deploy the Lima-based E2E test fixture
make deploy-lima-fixture

# Update the Control Plane on an existing Lima-based test fixture
make update-lima-fixture

# Run E2E tests against the Lima-based test fixture
make test-e2e E2E_FIXTURE=lima

# Run specific E2E test
make test-e2e E2E_FIXTURE=lima E2E_RUN=TestCancelDatabaseTask

# Run E2E tests with debug output for failing tests and skip cleanup
make test-e2e E2E_DEBUG=1

# Run cluster integration tests
make test-cluster

# Run specific cluster test
make test-cluster CLUSTER_TEST_RUN=TestInitialization
```

### Linting and Code Quality
```bash
# Run linters
make lint

# Update license notices
make licenses
```

### API Code Generation
```bash
# Regenerate API code from Goa design files
make -C api generate

# This generates:
# - Service interfaces and endpoints in api/apiv1/gen/control_plane/
# - HTTP transport layer in api/apiv1/gen/http/
# - OpenAPI 3.0 specifications (JSON and YAML)
```

### Documentation

```bash
# Build and serve documentation locally at http://localhost:8000
make docs
```

## Git Commit Message Style

- Commit message headers should follow the Conventional Commit style, e.g. `feat: an awesome feature`.
- Try to keep commit message headers to 50 characters or less.
- Commit message bodies should be markdown-formatted.
- Wrap each commit body line at 72 characters unless it would be syntactically incorrect, such as a hyperlink or code example.
- Ticket numbers should be included in the commit message footer, e.g. `PLAT-123`.

## Branch Naming Style

Branch names should include a Conventional Commit type, a ticket number (if any), and a brief, lower kebab-cased description, e.g. `feat/PLAT-123/an-awesome-feature` or `feat/an-awesome-feature` if there is no ticket.

## Architecture Overview

### Hexagonal Architecture

The codebase follows hexagonal/ports & adapters architecture with clear separation between:
- **Domain logic**: Core business logic in `server/internal/`
- **Infrastructure**: Orchestrator implementations, storage backends
- **API layer**: Goa-generated HTTP/MQTT interfaces

### Key Architectural Patterns

**Dependency Injection**: Uses `samber/do` injector. Each package has a `Provide()` function that registers dependencies with the injector.

**Resource Lifecycle**: Resources follow a standard lifecycle:
1. **Refresh**: Sync current state from infrastructure
2. **Plan**: Compute diff between desired and current state
3. **Apply**: Execute Create/Update/Delete operations
4. Resources declare their Executor (where they run), Dependencies, and lifecycle methods

**Workflow Orchestration**: Built on `cschleiden/go-workflows`:
- Workflows represent long-running operations (database creation, updates, backups)
- Activities are atomic units of work that execute on specific hosts
- Workflows persist state to etcd for durability and resumability
- Task tracking provides visibility into workflow execution progress

**State Management**:
- etcd stores all cluster state with versioned values and watch support
- Storage layer provides transaction support and optimistic locking
- Declarative desired state reconciliation pattern

### Directory Structure

**server/internal/** - Core server implementation:
- **api/** - API server, mounts Goa-generated handlers (HTTP & MQTT)
- **app/** - Application lifecycle, orchestrates startup (pre-init vs post-init states)
- **config/** - Multi-source configuration (JSON, env vars, CLI flags)
- **etcd/** - Embedded and remote etcd client, cluster membership, RBAC
- **storage/** - Generic storage interface abstracting etcd (Get, Put, Delete, Watch, Txn)
- **database/** - Database domain models, specs, instance state management
- **task/** - Task tracking system for workflow visibility and audit logs
- **orchestrator/** - Orchestrator abstraction with provider pattern
- **orchestrator/swarm/** - Docker Swarm implementation (services, networks, configs, secrets)
- **resource/** - Resource abstraction (lifecycle operations), Registry pattern
- **workflows/** - Workflow definitions (UpdateDatabase, DeleteDatabase, PgBackRestBackup, etc.)
- **workflows/activities/** - Reusable workflow activities (ApplyEvent, PlanRefresh, etc.)
- **host/** - Host registration, health checks, version tracking
- **cluster/** - Cluster initialization, join operations, membership
- **monitor/** - Periodic monitors, currently includes Instance and Host health monitoring
- **scheduler/** - Distributed job scheduling with leader election
- **certificates/** - TLS certificate management
- **ipam/** - IP address management for database networks
- **postgres/** - Postgres configuration (GUCs, HBA, roles)
- **patroni/** - Patroni integration for HA management
- **pgbackrest/** - Backup/restore configuration and operations

**api/apiv1/** - API layer:
- **design/** - Goa DSL definitions for endpoints, types, errors
- **gen/** - Auto-generated code from Goa (service interfaces, HTTP transport, OpenAPI specs)
- Implementation of generated service interface in `server/internal/api/apiv1/`

**e2e/** - End-to-end tests:
- Uses build tag `//go:build e2e_test`
- Test fixtures in `e2e/fixtures/`
- Full control plane cluster setup for realistic testing
- Supports debug mode with `-debug` flag

**clustertest/** - Cluster integration tests:
- Uses build tag `//go:build cluster_test`
- Tests multi-host cluster operations with testcontainers
- Tests actual Docker images in multi-container setup

### Docker Swarm Integration

The Control Plane uses Docker Swarm to run Postgres instances:
- **Services**: Each Postgres instance runs as a Docker Swarm service
- **Networks**: Overlay networks provide database isolation, the default bridge network facilitates communication between the Control Plane and Postgres/Patroni in the database containers
- **Bind mounts**: Configuration files, certificates, and the Postgres data directory are made available to the database containers via bind mounts

Container runtime:
- Patroni is PID 1 in the containers, and Patroni manages the Postgres process
- pgBackRest handles backups via mounted volumes
- Health checks are performed against Patroni's HTTP endpoints

### API Design with Goa

The API is defined using Goa's DSL in `api/apiv1/design/`:
- **api.go** - Main service definition with HTTP endpoints
- Domain-specific files (database.go, host.go, cluster.go) define types
- Goa generates service interfaces, HTTP transport, and OpenAPI specs
- Run `make -C api generate` to regenerate after design changes

API lifecycle:
- **Pre-init**: Only cluster init/join endpoints available before etcd initialization
- **Post-init**: Full API access after cluster initialization
- Supports both HTTP and MQTT transports

### Workflow System

Key workflows in `server/internal/workflows/`:
- **UpdateDatabase**: Creates/updates database (handles node additions, configuration updates)
- **DeleteDatabase**: Removes database and cleanup
- **PgBackRestBackup**: Initiates backup operations
- **PgBackRestRestore**: Performs in-place restores
- **RestartInstance, StartInstance, StopInstance**: Instance lifecycle operations
- **Switchover, Failover**: High availability operations via Patroni
- **ValidateSpec**: Validates database specifications before operations
- **RemoveHost**: Removes host from cluster, can remove instances from databases for disaster recovery scenarios

Activities pattern in `server/internal/workflows/activities/`:
- **ApplyEvent**: Executes resource lifecycle operations (Create/Update/Delete)
- **GetCurrentState**: Retrieves current resource state from infrastructure
- **PlanRefresh**: Computes differences between desired and current state
- **PersistState**: Saves state to etcd
- **LogTaskEvent**: Records workflow progress for task tracking
- **Executor**: Routes activity execution to appropriate host based on resource executor type

### Testing Strategy

**Unit Tests**: Standard Go tests throughout codebase
- Use `gotestsum --format-hide-empty-pkg` for better output
- Mock implementations in `server/internal/storage/storagetest/`
- Test utilities in `server/internal/testutils/`

**E2E Tests**: Full integration tests with real Control Plane cluster
- Test fixtures define reusable Control Plane cluster configurations
- Debug mode preserves failed test environments for inspection
- Use `E2E_RUN` to filter specific tests, `E2E_FIXTURE` for specific fixtures

**Cluster Tests**: Multi-host cluster operations
- Uses testcontainers for Docker-based test environments
- Tests actual Docker images in realistic multi-container setups
- Use `CLUSTER_TEST_RUN` to filter specific tests

**CI Tests**: Combined test suite for continuous integration
- `make test-ci` runs all unit tests with JUnit output
- `make test-e2e-ci` and `make test-cluster-ci` for E2E and cluster tests
- Uses `gotestsum` for formatted output

## Development Workflow

### Making API Changes

1. Edit Goa design files in `api/apiv1/design/`
2. Run `make -C api generate` to regenerate code
3. Implement new service methods in `server/internal/api/apiv1/`
4. Test with `make test`

### Adding a New Resource Type

1. Create resource struct implementing `resource.Resource` interface
2. Define Create/Update/Delete/Refresh methods
3. Register resource in the package's `RegisterResourceTypes()` method
4. Add resource to workflow activities as needed

### Creating a New Workflow

1. Define workflow in `server/internal/workflows/`
2. Use activities from `server/internal/workflows/activities/`
3. Register activities in the `Activities.Register` method in `server/internal/workflows/activities/activities.go`
4. Register workflow in the `Workflows.Register` method in `server/internal/workflows/workflows.go`
5. Add a service method for the new workflow in `server/internal/workflows/service.go`
6. Call the service method from the API handlers

### Running Single Tests

```bash
# Unit test
go test -run TestSubnetRange ./server/internal/ipam/...

# E2E test with debug output for failing tests
make test-e2e E2E_DEBUG=1 E2E_RUN=TestS3BackupRestore

# Cluster test
make test-cluster CLUSTER_TEST_RUN=TestInitialization
```

## Important Implementation Details

### Configuration Management

Multi-source configuration with precedence: CLI flags > environment variables > config file
- Config files in JSON format
- Environment variables prefixed with `CONTROL_PLANE_`
- CLI flags defined in `server/cmd/run.go`

### Error Handling

- Domain-specific errors in each package
- API errors mapped to HTTP status codes via Goa
- Structured error logging with zerolog

### Security

- Optional TLS certificates for API communication (inter-cluster and end-user)
- TLS certificates for system Postgres users
- etcd RBAC for access control
- Join tokens for secure cluster joining

### Observability

- Structured JSON logging with zerolog (pretty-printing in dev mode)
- Task logs provide audit trail for all operations
- Optional pprof endpoints for profiling

## Dependencies and Tools

**Core Dependencies**:
- Go 1.24.3
- goa.design/goa/v3@v3.19.1 - API framework
- github.com/cschleiden/go-workflows@v0.19.0 - Workflow engine
- go.etcd.io/etcd/server/v3@v3.6.1 - Distributed state
- github.com/docker/docker@v27.1.1+incompatible - Docker Swarm orchestration
- github.com/samber/do@v1.6.0 - Dependency injection

**Development Tools** (install with `make install-tools`):
- gotestsum - Enhanced test output
- golangci-lint - Code linting
- goa - API code generation
- goreleaser - Release automation
- changie - Changelog management
- yamlfmt - YAML formatting
- go-licenses - License compliance

## Docker Development Environment

The local development environment uses Docker Compose with:
- Multiple Control Plane server instances: three with embedded Etcd servers and three in client-only mode
- Host networking enabled (required for Docker Swarm)
- Auto-rebuild on code changes with `make dev-watch`

Access the API at `http://localhost:3000` (mTLS disabled in dev mode).
