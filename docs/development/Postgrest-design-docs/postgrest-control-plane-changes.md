# PostgREST Integration: Complete Code Changes

Every file that needs changing, what to change, and the code to write. This targets
the **post-PR-#280 codebase** (MCP YAML config merged). Pre-PR-280 alternatives are
noted where they differ.

> **Prerequisite:** PR #280 (feat: replace MCP env-var config with bind-mounted YAML
> config files) must be merged first. That PR restructures `service_spec.go`,
> `service_user_role.go`, `orchestrator.go`, `resources.go`, `service_instance_spec.go`,
> and `plan_update.go`. All PostgREST changes below are written against the post-merge
> state.
>
> **Current main vs this doc:** As of writing, PR #280 is open and not merged. If you
> open these files on `main` today, you will see the **pre-PR-280** code. Every
> "Current (post-PR-280)" block below shows the state *after* PR #280 merges, not what
> is on `main` right now. Key differences on current `main`:
>
> | File | Current `main` | After PR #280 (this doc) |
> |------|---------------|--------------------------|
> | `validate.go` | `validateMCPServiceConfig(config, path)` — 2 args | `validateMCPServiceConfig(config, path, isUpdate)` — 3 args |
> | `service_spec.go` | `buildServiceEnvVars()` exists, delivers config via env vars | `buildServiceEnvVars()` deleted, MCP uses YAML files |
> | `service_user_role.go` | `Roles: []string{"pgedge_application_read_only"}` | `Attributes: []string{"LOGIN"}` + fine-grained SQL grants |
> | `orchestrator.go` | No MCP-specific resources | Adds `DirResource` + `MCPConfigResource` |
> | `service_instance_spec.go` | No `DataDirID` field | Adds `DataDirID` + MCPConfigResource dependency |
>
> See [Section 14](#14-dependency-on-pr-280) for what to change if PostgREST work
> starts before PR #280 merges.

---

## Table of Contents

1. [API Enum](#1-api-enum)
2. [Validation](#2-validation)
3. [Service Image Registry](#3-service-image-registry)
4. [Container Spec (Env Vars)](#4-container-spec)
5. [Service User Role (NOINHERIT + Grants)](#5-service-user-role)
6. [Orchestrator (Resource Generation)](#6-orchestrator)
7. [Resource Registration](#7-resource-registration)
8. [Service Instance Spec](#8-service-instance-spec)
9. [Workflow (plan_update.go)](#9-workflow)
10. [Config Redaction (API Responses)](#10-config-redaction)
11. [Unit Tests](#11-unit-tests)
12. [E2E Tests](#12-e2e-tests)
13. [Files That Do NOT Change](#13-files-that-do-not-change)
14. [Dependency on PR #280](#14-dependency-on-pr-280)

---

## 1. API Enum

**File:** `api/apiv1/design/database.go`
**Lines:** ~159 (the `g.Enum(...)` call on `service_type`)

### Current (post-PR-280)

```go
g.Attribute("service_type", g.String, func() {
    g.Description("The type of service to run.")
    g.Enum("mcp")
    g.Example("mcp")
    g.Meta("struct:tag:json", "service_type")
})
```

### Change

```go
g.Attribute("service_type", g.String, func() {
    g.Description("The type of service to run.")
    g.Enum("mcp", "postgrest")
    g.Example("mcp")
    g.Example("postgrest")
    g.Meta("struct:tag:json", "service_type")
})
```

### After editing

```sh
make -C api generate
```

This regenerates `api/apiv1/gen/` types. The generated code validates `service_type`
against the enum at the Goa framework level (before reaching application validation).

---

## 2. Validation

**File:** `server/internal/api/apiv1/validate.go`

### 2a. Service type allowlist

**Location:** `validateServiceSpec()` — the `if svc.ServiceType != "mcp"` check.

#### Current (post-PR-280)

```go
if svc.ServiceType != "mcp" {
    err := fmt.Errorf("unsupported service type '%s' (only 'mcp' is currently supported)", svc.ServiceType)
    errs = append(errs, newValidationError(err, appendPath(path, "service_type")))
}
```

#### Change

```go
supportedServiceTypes := []string{"mcp", "postgrest"}
if !slices.Contains(supportedServiceTypes, svc.ServiceType) {
    err := fmt.Errorf("unsupported service type '%s' (supported: %s)",
        svc.ServiceType, strings.Join(supportedServiceTypes, ", "))
    errs = append(errs, newValidationError(err, appendPath(path, "service_type")))
}
```

**Import needed:** `"slices"` (Go 1.21+, already used elsewhere in the codebase).

### 2b. Config validation dispatch

**Location:** `validateServiceSpec()` — the `if svc.ServiceType == "mcp"` dispatch.

> On current `main`, `validateMCPServiceConfig` takes 2 args (config, path).
> Post-PR-280, it gains a third `isUpdate bool` parameter. See Section 14 for
> the pre-PR-280 alternative.

#### Current (post-PR-280)

```go
if svc.ServiceType == "mcp" {
    errs = append(errs, validateMCPServiceConfig(svc.Config, appendPath(path, "config"), isUpdate)...)
}
```

#### Change

```go
switch svc.ServiceType {
case "mcp":
    errs = append(errs, validateMCPServiceConfig(svc.Config, appendPath(path, "config"), isUpdate)...)
case "postgrest":
    errs = append(errs, validatePostgRESTServiceConfig(svc.Config, appendPath(path, "config"))...)
}
```

### 2c. New function: `validatePostgRESTServiceConfig()`

Add to the same file, after `validateMCPServiceConfig()`:

```go
// validatePostgRESTServiceConfig validates the config map for PostgREST services.
func validatePostgRESTServiceConfig(config map[string]any, path []string) []error {
    var errs []error

    // Required fields
    requiredFields := []string{"db_schemas", "db_anon_role"}
    for _, field := range requiredFields {
        if _, ok := config[field]; !ok {
            err := fmt.Errorf("%s is required", field)
            errs = append(errs, newValidationError(err, path))
        }
    }

    // db_schemas: must be a non-empty string
    if val, exists := config["db_schemas"]; exists {
        s, ok := val.(string)
        if !ok {
            err := errors.New("db_schemas must be a string")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_schemas"))))
        } else if strings.TrimSpace(s) == "" {
            err := errors.New("db_schemas must not be empty")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_schemas"))))
        }
    }

    // db_anon_role: must be a non-empty string
    if val, exists := config["db_anon_role"]; exists {
        s, ok := val.(string)
        if !ok {
            err := errors.New("db_anon_role must be a string")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_anon_role"))))
        } else if strings.TrimSpace(s) == "" {
            err := errors.New("db_anon_role must not be empty")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_anon_role"))))
        }
    }

    // db_pool: optional, integer in range [1, 30]
    if val, exists := config["db_pool"]; exists {
        switch v := val.(type) {
        case float64:
            if v != float64(int(v)) || v < 1 || v > 30 {
                err := errors.New("db_pool must be an integer between 1 and 30")
                errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_pool"))))
            }
        case json.Number:
            n, nErr := v.Int64()
            if nErr != nil || n < 1 || n > 30 {
                err := errors.New("db_pool must be an integer between 1 and 30")
                errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_pool"))))
            }
        default:
            err := errors.New("db_pool must be a number")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("db_pool"))))
        }
    }

    // max_rows: optional, integer in range [1, 10000]
    if val, exists := config["max_rows"]; exists {
        switch v := val.(type) {
        case float64:
            if v != float64(int(v)) || v < 1 || v > 10000 {
                err := errors.New("max_rows must be an integer between 1 and 10000")
                errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("max_rows"))))
            }
        case json.Number:
            n, nErr := v.Int64()
            if nErr != nil || n < 1 || n > 10000 {
                err := errors.New("max_rows must be an integer between 1 and 10000")
                errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("max_rows"))))
            }
        default:
            err := errors.New("max_rows must be a number")
            errs = append(errs, newValidationError(err, appendPath(path, mapKeyPath("max_rows"))))
        }
    }

    return errs
}
```

**Import needed:** `"encoding/json"` (for `json.Number` handling, matches PR #280 pattern).

### Validation test file

**File:** `server/internal/api/apiv1/validate_test.go`

Add test cases for `validatePostgRESTServiceConfig()`:

```go
func TestValidatePostgRESTServiceConfig(t *testing.T) {
    tests := []struct {
        name      string
        config    map[string]any
        wantErrs  int
        errSubstr string
    }{
        {
            name:     "valid minimal config",
            config:   map[string]any{"db_schemas": "api", "db_anon_role": "web_anon"},
            wantErrs: 0,
        },
        {
            name:     "valid full config",
            config:   map[string]any{"db_schemas": "api", "db_anon_role": "web_anon", "db_pool": float64(15), "max_rows": float64(500)},
            wantErrs: 0,
        },
        {
            name:      "missing db_schemas",
            config:    map[string]any{"db_anon_role": "web_anon"},
            wantErrs:  1,
            errSubstr: "db_schemas is required",
        },
        {
            name:      "missing db_anon_role",
            config:    map[string]any{"db_schemas": "api"},
            wantErrs:  1,
            errSubstr: "db_anon_role is required",
        },
        {
            name:      "missing both required fields",
            config:    map[string]any{},
            wantErrs:  2,
        },
        {
            name:      "empty db_schemas",
            config:    map[string]any{"db_schemas": "", "db_anon_role": "web_anon"},
            wantErrs:  1,
            errSubstr: "must not be empty",
        },
        {
            name:      "db_pool too high",
            config:    map[string]any{"db_schemas": "api", "db_anon_role": "web_anon", "db_pool": float64(50)},
            wantErrs:  1,
            errSubstr: "between 1 and 30",
        },
        {
            name:      "db_pool zero",
            config:    map[string]any{"db_schemas": "api", "db_anon_role": "web_anon", "db_pool": float64(0)},
            wantErrs:  1,
            errSubstr: "between 1 and 30",
        },
        {
            name:      "max_rows too high",
            config:    map[string]any{"db_schemas": "api", "db_anon_role": "web_anon", "max_rows": float64(99999)},
            wantErrs:  1,
            errSubstr: "between 1 and 10000",
        },
        {
            name:     "unknown keys are ignored",
            config:   map[string]any{"db_schemas": "api", "db_anon_role": "web_anon", "unknown_key": "value"},
            wantErrs: 0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            errs := validatePostgRESTServiceConfig(tt.config, []string{"config"})
            assert.Len(t, errs, tt.wantErrs)
            if tt.errSubstr != "" && len(errs) > 0 {
                assert.Contains(t, errs[0].Error(), tt.errSubstr)
            }
        })
    }
}
```

---

## 3. Service Image Registry

**File:** `server/internal/orchestrator/swarm/service_images.go`
**Location:** `NewServiceVersions()` function

### Current (post-PR-280)

```go
func NewServiceVersions(cfg config.Config) *ServiceVersions {
    versions := &ServiceVersions{
        cfg:    cfg,
        images: make(map[string]map[string]*ServiceImage),
    }

    // MCP service versions
    versions.addServiceImage("mcp", "latest", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgres-mcp:latest"),
    })

    return versions
}
```

### Change

```go
func NewServiceVersions(cfg config.Config) *ServiceVersions {
    versions := &ServiceVersions{
        cfg:    cfg,
        images: make(map[string]map[string]*ServiceImage),
    }

    // MCP service versions
    versions.addServiceImage("mcp", "latest", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgres-mcp:latest"),
    })

    // PostgREST service versions
    versions.addServiceImage("postgrest", "14.5", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgrest/postgrest:v14.5"),
        PostgresConstraint: &host.VersionConstraint{
            Min: host.MustParseVersion("15"),
        },
    })
    versions.addServiceImage("postgrest", "latest", &ServiceImage{
        Tag: serviceImageTag(cfg, "postgrest/postgrest:latest"),
    })

    return versions
}
```

**Note on version:** PostgREST v14.5 is the latest stable release (Feb 2026). v14
dropped support for PostgreSQL 12 (EOL). The Postgres >= 15 constraint in the
implementation guide is stricter than PostgREST's own minimum — it reflects a pgEdge
platform decision, not an upstream limitation.

**Note on image registry:** `serviceImageTag()` will prepend
`cfg.DockerSwarm.ImageRepositoryHost` to `postgrest/postgrest:v14.5` if a registry host
is configured. The function only bypasses the prefix when the first path component
contains `.`, `:`, or equals `localhost` — `"postgrest"` matches none of these. This
means PostgREST images must be mirrored to the private registry if one is configured.
If the intent is to always pull from Docker Hub, bypass `serviceImageTag()` and use the
image reference directly:

```go
// Option A: Go through configured registry (image must be mirrored)
Tag: serviceImageTag(cfg, "postgrest/postgrest:v14.5"),

// Option B: Always pull from Docker Hub (bypass registry)
Tag: "postgrest/postgrest:v14.5",
```

Choose based on whether the deployment environment mirrors images to a private registry.

### Image registry tests

**File:** `server/internal/orchestrator/swarm/service_images_test.go`

Add test cases:

```go
{
    name:        "postgrest latest resolves",
    serviceType: "postgrest",
    version:     "latest",
    wantTag:     "postgrest/postgrest:latest",
},
{
    name:        "postgrest 14.5 resolves",
    serviceType: "postgrest",
    version:     "14.5",
    wantTag:     "postgrest/postgrest:v14.5",
},
{
    name:        "postgrest unsupported version",
    serviceType: "postgrest",
    version:     "99.99.99",
    wantErr:     true,
},
```

---

## 4. Container Spec

**File:** `server/internal/orchestrator/swarm/service_spec.go`

PostgREST uses environment variables for configuration (unlike MCP post-PR-280 which
uses bind-mounted YAML files). PostgREST's official Docker image reads `PGRST_*` env
vars and libpq `PG*` env vars — no config file needed.

### 4a. Config delivery: env vars vs YAML files

PR #280 removed `buildServiceEnvVars()` entirely and replaced it with YAML config
files for MCP. PostgREST needs env vars back, but only for PostgREST containers.
On current `main`, `buildServiceEnvVars()` still exists — see
[Section 14](#14-dependency-on-pr-280) for how to add PostgREST there instead.

#### Post-PR-280 state of `ServiceContainerSpec()`

After PR #280, the container spec has:
- No `Env` field (env vars removed for MCP)
- `Command` and `Args` set for MCP
- `User: "1001"` for MCP
- Bind mount for `/app/data`

#### Change: Add service-type branching

The `ServiceContainerSpec()` function needs to branch on service type for:
- **Env vars**: PostgREST needs them, MCP doesn't (post-PR-280)
- **Command/Args**: MCP sets custom command, PostgREST uses the image default
- **User**: MCP runs as UID 1001, PostgREST uses the image default
- **Mounts**: MCP has bind mount, PostgREST has none

```go
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

    containerSpec := &swarm.ContainerSpec{
        Image:    opts.ServiceImage.Tag,
        Labels:   labels,
        Hostname: opts.Hostname,
        Healthcheck: &container.HealthConfig{
            Test:        buildServiceHealthCheckCmd(opts),
            StartPeriod: time.Second * 30,
            Interval:    time.Second * 10,
            Timeout:     time.Second * 5,
            Retries:     3,
        },
    }

    // Service-type-specific container configuration
    switch opts.ServiceSpec.ServiceType {
    case "mcp":
        // MCP: bind-mounted YAML config, custom command, runs as UID 1001
        containerSpec.User = fmt.Sprintf("%d", mcpContainerUID)
        containerSpec.Command = []string{"/app/pgedge-postgres-mcp"}
        containerSpec.Args = []string{"-config", "/app/data/config.yaml"}
        containerSpec.Mounts = []mount.Mount{
            docker.BuildMount(opts.DataPath, "/app/data", false),
        }

    case "postgrest":
        // PostgREST: env-var-based config, default image entrypoint, no mounts
        containerSpec.Env = buildPostgRESTEnvVars(opts)
        containerSpec.Mounts = []mount.Mount{}
    }

    return swarm.ServiceSpec{
        TaskTemplate: swarm.TaskSpec{
            ContainerSpec: containerSpec,
            Networks:      networks,
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

### 4b. New function: `buildPostgRESTEnvVars()`

Add to `service_spec.go`:

```go
// buildPostgRESTEnvVars constructs the environment variables for a PostgREST container.
// PostgREST reads PGRST_* variables for its own config and PG* variables (via libpq)
// for the database connection.
func buildPostgRESTEnvVars(opts *ServiceContainerSpecOptions) []string {
    env := []string{
        // libpq connection — PGRST_DB_URI is set to a bare "postgresql://" so that
        // libpq fills in host/port/dbname/user/password from the PG* env vars.
        // This keeps credentials out of the connection string.
        "PGRST_DB_URI=postgresql://",
        fmt.Sprintf("PGHOST=%s", opts.DatabaseHost),
        fmt.Sprintf("PGPORT=%d", opts.DatabasePort),
        fmt.Sprintf("PGDATABASE=%s", opts.DatabaseName),
        "PGSSLMODE=prefer",
        "PGTARGETSESSIONATTRS=read-write",
    }

    // Credentials (injected by ServiceUserRole)
    if opts.Credentials != nil {
        env = append(env,
            fmt.Sprintf("PGUSER=%s", opts.Credentials.Username),
            fmt.Sprintf("PGPASSWORD=%s", opts.Credentials.Password),
        )
    }

    // PostgREST-specific config from ServiceSpec.Config
    if schemas, ok := opts.ServiceSpec.Config["db_schemas"].(string); ok {
        env = append(env, fmt.Sprintf("PGRST_DB_SCHEMAS=%s", schemas))
    }
    if role, ok := opts.ServiceSpec.Config["db_anon_role"].(string); ok {
        env = append(env, fmt.Sprintf("PGRST_DB_ANON_ROLE=%s", role))
    }
    if pool, ok := opts.ServiceSpec.Config["db_pool"].(float64); ok {
        env = append(env, fmt.Sprintf("PGRST_DB_POOL=%d", int(pool)))
    }
    if maxRows, ok := opts.ServiceSpec.Config["max_rows"].(float64); ok {
        env = append(env, fmt.Sprintf("PGRST_DB_MAX_ROWS=%d", int(maxRows)))
    }

    // Hardcoded PostgREST defaults
    env = append(env,
        "PGRST_DB_POOL_ACQUISITION_TIMEOUT=10",
        "PGRST_SERVER_PORT=8080",
        "PGRST_ADMIN_SERVER_PORT=3001",
        "PGRST_LOG_LEVEL=warn",
        "PGRST_DB_CHANNEL_ENABLED=true",
    )

    return env
}
```

---

## 5. Service User Role

**File:** `server/internal/orchestrator/swarm/service_user_role.go`

PostgREST requires two changes to the service user that MCP does not:

1. **NOINHERIT** — PostgREST's authenticator pattern requires `SET ROLE`, which only
   works correctly when the service user does not automatically inherit granted role
   privileges.
2. **GRANT web_anon TO service_user** — PostgREST needs the anonymous role granted to
   the service user so it can `SET ROLE web_anon` on each request.

### 5a. Current state

**On current `main`:** `Create()` generates username/password, then calls
`postgres.CreateUserRole()` with `Roles: []string{"pgedge_application_read_only"}`.
No per-service-type branching.

**After PR #280:** `Create()` generates username/password, creates the role with
`Attributes: []string{"LOGIN"}` (no inherited roles), then runs fine-grained SQL grants
for public schema access (`GRANT CONNECT`, `GRANT USAGE ON SCHEMA public`, etc.).

### 5b. New field on ServiceUserRole

Add `ServiceType` to the struct so `Create()` can branch:

```go
type ServiceUserRole struct {
    ServiceInstanceID string `json:"service_instance_id"`
    DatabaseID        string `json:"database_id"`
    DatabaseName      string `json:"database_name"`
    Username          string `json:"username"`
    HostID            string `json:"host_id"`
    PostgresHostID    string `json:"postgres_host_id"`
    ServiceID         string `json:"service_id"`
    ServiceType       string `json:"service_type"`   // NEW: "mcp" or "postgrest"
    AnonRole          string `json:"anon_role"`       // NEW: PostgREST only, from config["db_anon_role"]
    Password          string `json:"password"`
}
```

**DiffIgnore** must include the new fields that are set at creation time:
```go
func (r *ServiceUserRole) DiffIgnore() []string {
    return []string{"/postgres_host_id", "/username", "/password", "/anon_role"}
}
```

### 5c. Changes to `Create()`

After the existing role creation and grant logic, add PostgREST-specific handling:

```go
func (r *ServiceUserRole) Create(ctx context.Context, rc resource.ResourceContext) error {
    // ... existing: generate username, password, connect to primary ...

    // Create the Postgres role (same for all service types post-PR-280)
    err = postgres.CreateUserRole(ctx, conn, postgres.CreateUserRoleOptions{
        Name:       r.Username,
        Password:   r.Password,
        DBName:     r.DatabaseName,
        DBOwner:    false,
        Attributes: []string{"LOGIN"},
    })
    if err != nil {
        return fmt.Errorf("creating user role: %w", err)
    }

    // Service-type-specific grants.
    // Default to "mcp" for backward compatibility: existing ServiceUserRole resources
    // in etcd were created before the ServiceType field existed and will deserialize
    // with ServiceType == "". Treating "" as "mcp" ensures re-creation of existing
    // MCP service users still runs the correct grants.
    serviceType := r.ServiceType
    if serviceType == "" {
        serviceType = "mcp"
    }

    switch serviceType {
    case "mcp":
        // Post-PR-280 MCP grants: public schema read-only + pg_read_all_settings
        grants := []string{
            fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(r.Username)),
            fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", sanitizeIdentifier(r.Username)),
            fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s", sanitizeIdentifier(r.Username)),
            fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %s", sanitizeIdentifier(r.Username)),
            fmt.Sprintf("GRANT pg_read_all_settings TO %s", sanitizeIdentifier(r.Username)),
        }
        for _, grant := range grants {
            if _, err := conn.Exec(ctx, grant); err != nil {
                return fmt.Errorf("granting MCP privileges: %w", err)
            }
        }

    case "postgrest":
        // PostgREST: NOINHERIT + grant anonymous role
        //
        // NOINHERIT is required because PostgREST uses SET ROLE to switch to the
        // anonymous role on each request. Without NOINHERIT, the service user would
        // automatically inherit web_anon privileges, defeating role isolation.
        _, err = conn.Exec(ctx, fmt.Sprintf(
            "ALTER ROLE %s NOINHERIT", sanitizeIdentifier(r.Username)))
        if err != nil {
            return fmt.Errorf("setting NOINHERIT on postgrest role: %w", err)
        }

        // Grant the anonymous role so SET ROLE works.
        // The role name comes from config["db_anon_role"] (e.g., "web_anon").
        if r.AnonRole != "" {
            _, err = conn.Exec(ctx, fmt.Sprintf(
                "GRANT %s TO %s", sanitizeIdentifier(r.AnonRole), sanitizeIdentifier(r.Username)))
            if err != nil {
                return fmt.Errorf("granting anonymous role %q to postgrest user: %w", r.AnonRole, err)
            }
        }

        // Grant CONNECT so the service user can connect to the database.
        _, err = conn.Exec(ctx, fmt.Sprintf(
            "GRANT CONNECT ON DATABASE %s TO %s", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(r.Username)))
        if err != nil {
            return fmt.Errorf("granting CONNECT to postgrest user: %w", err)
        }
    }

    return nil
}
```

### 5d. Update `populateCredentials()` in `service_instance_spec.go`

Post-PR-280, `populateCredentials()` sets `Role: "..."` on the `ServiceUser`. For
PostgREST, the role field is not used (PostgREST's role switching is handled by
`SET ROLE` at the Postgres level, not by the Control Plane). Set it to a descriptive
value:

```go
func (s *ServiceInstanceSpecResource) populateCredentials(rc resource.ResourceContext) error {
    userRole, err := resource.GetFromState[*ServiceUserRole](rc, ServiceUserRoleIdentifier(s.ServiceInstanceID))
    if err != nil {
        return fmt.Errorf("getting service user role: %w", err)
    }

    role := "public_read_only" // default for MCP post-PR-280
    if s.ServiceSpec.ServiceType == "postgrest" {
        role = "postgrest_authenticator"
    }

    s.Credentials = &database.ServiceUser{
        Username: userRole.Username,
        Password: userRole.Password,
        Role:     role,
    }
    return nil
}
```

---

## 6. Orchestrator

**File:** `server/internal/orchestrator/swarm/orchestrator.go`
**Location:** `GenerateServiceInstanceResources()`

### Current (post-PR-280)

After PR #280, this function:
1. Resolves service image
2. Validates Postgres/Spock compatibility
3. **For MCP:** Parses config via `database.ParseMCPServiceConfig()`, creates
   `DirResource`, creates `MCPConfigResource`
4. Creates Network, ServiceUserRole, ServiceInstanceSpec, ServiceInstance
5. Returns resource chain

### Change: Add PostgREST branch

The PostgREST branch is simpler than MCP — no config files, no DirResource, no
MCPConfigResource. PostgREST only needs the four base resources.

```go
func (o *Orchestrator) GenerateServiceInstanceResources(spec *database.ServiceInstanceSpec) (
    *database.ServiceInstanceResources, error) {

    // 1. Resolve service image (unchanged)
    serviceImage, err := o.serviceVersions.GetServiceImage(
        spec.ServiceSpec.ServiceType, spec.ServiceSpec.Version)
    if err != nil {
        return nil, err
    }

    // 2. Validate compatibility (unchanged)
    if err := serviceImage.ValidateCompatibility(spec.PgEdgeVersion); err != nil {
        return nil, err
    }

    // 3. Create base resources (shared across all service types)
    networkResource := &Network{
        // ... unchanged ...
    }

    serviceUserRole := &ServiceUserRole{
        ServiceInstanceID: spec.ServiceInstanceID,
        DatabaseID:        spec.DatabaseID,
        DatabaseName:      spec.DatabaseName,
        HostID:            spec.HostID,
        PostgresHostID:    spec.PostgresHostID,
        ServiceID:         spec.ServiceSpec.ServiceID,
        ServiceType:       spec.ServiceSpec.ServiceType, // NEW
        Password:          "", // Generated on Create
    }

    // PostgREST: set AnonRole from config
    if spec.ServiceSpec.ServiceType == "postgrest" {
        if anonRole, ok := spec.ServiceSpec.Config["db_anon_role"].(string); ok {
            serviceUserRole.AnonRole = anonRole
        }
    }

    // Copy persisted credentials if they exist (backward compatibility, unchanged)
    if spec.Credentials != nil {
        serviceUserRole.Username = spec.Credentials.Username
        serviceUserRole.Password = spec.Credentials.Password
    }

    // 4. Service-type-specific resource chain
    var resources []resource.Resource

    switch spec.ServiceSpec.ServiceType {
    case "mcp":
        // MCP: DirResource → MCPConfigResource → ServiceInstanceSpec → ServiceInstance
        // (post-PR-280 logic, unchanged)
        mcpConfig, err := database.ParseMCPServiceConfig(spec.ServiceSpec.Config, false)
        if err != nil {
            return nil, fmt.Errorf("parsing MCP config: %w", err)
        }

        dirResource := &filesystem.DirResource{/* ... unchanged ... */}
        mcpConfigResource := &MCPConfigResource{/* ... unchanged ... */}

        serviceInstanceSpec := &ServiceInstanceSpecResource{
            // ... standard fields ...
            DataDirID: dirResource.Identifier().ID,
        }

        serviceInstance := &ServiceInstanceResource{/* ... unchanged ... */}

        resources = []resource.Resource{
            networkResource, serviceUserRole,
            dirResource, mcpConfigResource,
            serviceInstanceSpec, serviceInstance,
        }

    case "postgrest":
        // PostgREST: Network → ServiceUserRole → ServiceInstanceSpec → ServiceInstance
        // No config files, no DirResource. Config delivered via env vars.
        serviceInstanceSpec := &ServiceInstanceSpecResource{
            ServiceInstanceID: spec.ServiceInstanceID,
            ServiceSpec:       spec.ServiceSpec,
            DatabaseID:        spec.DatabaseID,
            DatabaseName:      spec.DatabaseName,
            HostID:            spec.HostID,
            ServiceName:       ServiceInstanceName(
                spec.ServiceSpec.ServiceType, spec.DatabaseID,
                spec.ServiceSpec.ServiceID, spec.HostID),
            Hostname:          ServiceInstanceHostname(spec.ServiceSpec.ServiceID, spec.HostID),
            CohortMemberID:    spec.CohortMemberID,
            ServiceImage:      serviceImage,
            DatabaseNetworkID: database.GenerateDatabaseNetworkID(spec.DatabaseID),
            DatabaseHost:      spec.DatabaseHost,
            DatabasePort:      spec.DatabasePort,
            Port:              spec.Port,
            // No DataDirID — PostgREST has no bind mounts
        }

        serviceInstance := &ServiceInstanceResource{
            ServiceInstanceID: spec.ServiceInstanceID,
            DatabaseID:        spec.DatabaseID,
            HostID:            spec.HostID,
        }

        resources = []resource.Resource{
            networkResource, serviceUserRole,
            serviceInstanceSpec, serviceInstance,
        }
    }

    // 5. Convert to ResourceData (unchanged)
    data, err := resource.ToResourceData(resources...)
    if err != nil {
        return nil, err
    }

    return &database.ServiceInstanceResources{
        ServiceInstance: &database.ServiceInstance{
            ServiceInstanceID: spec.ServiceInstanceID,
            ServiceID:         spec.ServiceSpec.ServiceID,
            DatabaseID:        spec.DatabaseID,
            HostID:            spec.HostID,
            State:             database.ServiceInstanceStateCreating,
        },
        Resources: data,
    }, nil
}
```

---

## 7. Resource Registration

**File:** `server/internal/orchestrator/swarm/resources.go`

### No change needed for PostgREST

PostgREST uses the same four resource types as the base service model:
- `Network` (already registered)
- `ServiceUserRole` (already registered)
- `ServiceInstanceSpecResource` (already registered)
- `ServiceInstanceResource` (already registered)

MCP's `MCPConfigResource` (registered by PR #280) is MCP-specific and not used by
PostgREST.

---

## 8. Service Instance Spec

**File:** `server/internal/orchestrator/swarm/service_instance_spec.go`

### Change: Handle missing DataDirID for PostgREST

Post-PR-280, `Refresh()` resolves `DataDirID` to get the host-side data path for the
bind mount. PostgREST doesn't have a `DataDirID` (no bind mount), so this must be
conditional:

```go
func (s *ServiceInstanceSpecResource) Refresh(ctx context.Context, rc resource.ResourceContext) error {
    // Validate network exists (unchanged)
    _, err := resource.GetFromState[*Network](rc, NetworkResourceIdentifier(s.DatabaseNetworkID))
    if err != nil {
        return fmt.Errorf("database network not found: %w", err)
    }

    // Populate credentials from ServiceUserRole (unchanged)
    if err := s.populateCredentials(rc); err != nil {
        return err
    }

    // Resolve data path (MCP only — PostgREST has no bind mount)
    var dataPath string
    if s.DataDirID != "" {
        dataPath, err = filesystem.DirResourceFullPath(rc, s.DataDirID)
        if err != nil {
            return fmt.Errorf("resolving data directory: %w", err)
        }
    }

    // Generate Docker service spec
    spec, err := ServiceContainerSpec(&ServiceContainerSpecOptions{
        ServiceSpec:       s.ServiceSpec,
        ServiceInstanceID: s.ServiceInstanceID,
        DatabaseID:        s.DatabaseID,
        DatabaseName:      s.DatabaseName,
        HostID:            s.HostID,
        ServiceName:       s.ServiceName,
        Hostname:          s.Hostname,
        CohortMemberID:    s.CohortMemberID,
        ServiceImage:      s.ServiceImage,
        Credentials:       s.Credentials,
        DatabaseNetworkID: s.DatabaseNetworkID,
        DatabaseHost:      s.DatabaseHost,
        DatabasePort:      s.DatabasePort,
        Port:              s.Port,
        DataPath:          dataPath, // empty string for PostgREST
    })
    if err != nil {
        return err
    }

    s.Spec = spec
    return nil
}
```

### Change: Dependencies

Post-PR-280, `Dependencies()` includes `MCPConfigResourceIdentifier`. For PostgREST,
this dependency doesn't apply:

```go
func (s *ServiceInstanceSpecResource) Dependencies() []resource.ResourceIdentifier {
    deps := []resource.ResourceIdentifier{
        NetworkResourceIdentifier(s.DatabaseNetworkID),
        ServiceUserRoleIdentifier(s.ServiceInstanceID),
    }
    // MCP has additional config resource dependency
    if s.DataDirID != "" {
        deps = append(deps, MCPConfigResourceIdentifier(s.ServiceInstanceID))
    }
    return deps
}
```

---

## 9. Workflow

**File:** `server/internal/workflows/plan_update.go`
**Location:** `getServiceResources()`

### No PostgREST-specific changes needed

Post-PR-280, `getServiceResources()` is service-type-agnostic. It:
1. Generates the service instance ID
2. Finds a Postgres instance (co-located or fallback)
3. Builds a `ServiceInstanceSpec`
4. Fires the `GenerateServiceInstanceResources` activity

The PostgREST-specific logic lives in `GenerateServiceInstanceResources()` (section 6)
and `ServiceContainerSpec()` (section 4), not here.

### Port fix already done by PR #280

PR #280 changed `findPostgresInstance()` to always return internal port 5432 (not the
host-published port). This is correct for PostgREST, which connects over the overlay
network.

---

## 10. Config Redaction

**File:** `server/internal/api/apiv1/convert.go`

PR #280 adds config key redaction for MCP (hides `anthropic_api_key`, `openai_api_key`,
`init_users` from API responses). PostgREST has no sensitive config keys — `db_schemas`,
`db_anon_role`, `db_pool`, and `max_rows` are all safe to return.

### No change needed

---

## 11. Unit Tests

### 11a. Container spec tests

**File:** `server/internal/orchestrator/swarm/service_spec_test.go`

Add test cases for PostgREST container spec:

```go
{
    name: "postgrest service basic",
    opts: &ServiceContainerSpecOptions{
        ServiceSpec: &database.ServiceSpec{
            ServiceID:   "postgrest",
            ServiceType: "postgrest",
            Version:     "14.5",
            Config: map[string]any{
                "db_schemas":   "api",
                "db_anon_role": "web_anon",
                "db_pool":      float64(10),
                "max_rows":     float64(1000),
            },
        },
        ServiceInstanceID: "mydb-postgrest-host1",
        DatabaseID:        "mydb",
        DatabaseName:      "storefront",
        HostID:            "host1",
        ServiceName:       "postgrest-mydb-postgrest-host1",
        Hostname:          "postgrest-host1",
        CohortMemberID:    "swarm-node-1",
        ServiceImage:      &ServiceImage{Tag: "postgrest/postgrest:v14.5"},
        Credentials:       &database.ServiceUser{Username: "svc_postgrest_host1", Password: "secret"},
        DatabaseNetworkID: "mydb-database",
        DatabaseHost:      "postgres-mydb-n1",
        DatabasePort:      5432,
        Port:              intPtr(3100),
    },
    checks: []checkFunc{
        checkLabels(map[string]string{
            "pgedge.component":           "service",
            "pgedge.service.instance.id": "mydb-postgrest-host1",
            "pgedge.service.id":          "postgrest",
            "pgedge.database.id":         "mydb",
            "pgedge.host.id":             "host1",
        }),
        checkNetworks("bridge", "mydb-database"),
        checkPlacement("node.id==swarm-node-1"),
        checkHealthcheck("/health", 8080),
        checkPorts(8080, 3100),
        checkEnv(
            "PGRST_DB_URI=postgresql://",
            "PGHOST=postgres-mydb-n1",
            "PGPORT=5432",
            "PGDATABASE=storefront",
            "PGSSLMODE=prefer",
            "PGTARGETSESSIONATTRS=read-write",
            "PGUSER=svc_postgrest_host1",
            "PGPASSWORD=secret",
            "PGRST_DB_SCHEMAS=api",
            "PGRST_DB_ANON_ROLE=web_anon",
            "PGRST_DB_POOL=10",
            "PGRST_DB_MAX_ROWS=1000",
            "PGRST_DB_POOL_ACQUISITION_TIMEOUT=10",
            "PGRST_SERVER_PORT=8080",
            "PGRST_ADMIN_SERVER_PORT=3001",
            "PGRST_LOG_LEVEL=warn",
            "PGRST_DB_CHANNEL_ENABLED=true",
        ),
        // PostgREST: no mounts, no custom command, no custom user, health check disabled
        checkNoMounts(),
        checkNoCommand(),
    },
},
{
    name: "postgrest service optional fields omitted",
    opts: &ServiceContainerSpecOptions{
        ServiceSpec: &database.ServiceSpec{
            ServiceID:   "postgrest",
            ServiceType: "postgrest",
            Version:     "latest",
            Config: map[string]any{
                "db_schemas":   "api",
                "db_anon_role": "web_anon",
                // db_pool and max_rows omitted
            },
        },
        ServiceInstanceID: "mydb-postgrest-host1",
        DatabaseID:        "mydb",
        DatabaseName:      "storefront",
        HostID:            "host1",
        ServiceName:       "postgrest-mydb-postgrest-host1",
        Hostname:          "postgrest-host1",
        CohortMemberID:    "swarm-node-1",
        ServiceImage:      &ServiceImage{Tag: "postgrest/postgrest:latest"},
        Credentials:       &database.ServiceUser{Username: "svc_postgrest_host1", Password: "secret"},
        DatabaseNetworkID: "mydb-database",
        DatabaseHost:      "postgres-mydb-n1",
        DatabasePort:      5432,
    },
    checks: []checkFunc{
        checkEnvAbsent("PGRST_DB_POOL", "PGRST_DB_MAX_ROWS"),
        checkEnv(
            "PGRST_DB_POOL_ACQUISITION_TIMEOUT=10",
            "PGRST_SERVER_PORT=8080",
        ),
    },
},
```

**New helper function for health check (in `service_spec.go`):**

```go
// buildServiceHealthCheckCmd returns the health check command for the service.
// PostgREST's Docker image is a static binary with no shell utilities (no curl, no wget).
// We disable the Docker health check for PostgREST and rely on the CP's ServiceInstanceMonitor.
func buildServiceHealthCheckCmd(opts *ServiceContainerSpecOptions) []string {
    if opts.ServiceSpec.ServiceType == "postgrest" {
        return []string{"NONE"}
    }
    return []string{"CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"}
}
```

**New check functions needed (in `service_spec_test.go`):**

```go
func checkNoMounts() checkFunc {
    return func(t *testing.T, spec swarm.ServiceSpec) {
        t.Helper()
        assert.Empty(t, spec.TaskTemplate.ContainerSpec.Mounts)
    }
}

func checkNoCommand() checkFunc {
    return func(t *testing.T, spec swarm.ServiceSpec) {
        t.Helper()
        assert.Empty(t, spec.TaskTemplate.ContainerSpec.Command)
        assert.Empty(t, spec.TaskTemplate.ContainerSpec.Args)
    }
}

func checkEnvAbsent(keys ...string) checkFunc {
    return func(t *testing.T, spec swarm.ServiceSpec) {
        t.Helper()
        env := spec.TaskTemplate.ContainerSpec.Env
        for _, key := range keys {
            for _, e := range env {
                assert.False(t, strings.HasPrefix(e, key+"="),
                    "env var %s should not be present", key)
            }
        }
    }
}
```

---

## 12. E2E Tests

**File:** `e2e/service_provisioning_test.go`

### 12a. Basic provisioning test

```go
func TestProvisionPostgRESTService(t *testing.T) {
    t.Parallel()

    host1 := fixture.HostIDs()[0]

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    t.Log("Creating database with PostgREST service")

    db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
        Spec: &controlplane.DatabaseSpec{
            DatabaseName: "test_postgrest_service",
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
                    ServiceID:   "postgrest",
                    ServiceType: "postgrest",
                    Version:     "latest",
                    HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
                    Config: map[string]any{
                        "db_schemas":   "api",
                        "db_anon_role": "web_anon",
                    },
                },
            },
        },
    })

    t.Log("Database created, verifying service instances")

    require.NotNil(t, db.ServiceInstances)
    require.Len(t, db.ServiceInstances, 1)

    serviceInstance := db.ServiceInstances[0]
    assert.Equal(t, "postgrest", serviceInstance.ServiceID)
    assert.Equal(t, string(host1), serviceInstance.HostID)

    // Poll until running
    if serviceInstance.State != "running" {
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

    // Verify HTTP port is configured
    serviceInstance = db.ServiceInstances[0]
    if serviceInstance.Status != nil {
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

### 12b. Additional E2E tests to add

| Test | What it validates |
|------|-------------------|
| `TestProvisionPostgRESTServiceUnsupportedVersion` | Version `"99.99.99"` fails workflow, DB goes to `"failed"` |
| `TestUpdateDatabaseAddPostgRESTService` | Adding PostgREST to existing DB works without affecting DB |
| `TestUpdateDatabaseRemovePostgRESTService` | Empty `Services` array removes PostgREST cleanly |
| `TestPostgRESTServiceStable` | Unrelated DB update doesn't recreate PostgREST (checks `container_id` unchanged) |
| `TestProvisionPostgRESTServiceConfigUpdate` | Changing `max_rows` triggers redeploy, service reaches `"running"` |

---

## 13. Files That Do NOT Change

| File | Why |
|------|-----|
| `server/internal/database/spec.go` | `ServiceSpec` is service-type-agnostic |
| `server/internal/database/service_instance.go` | `ServiceInstance` is service-type-agnostic |
| `server/internal/database/operations/` | Plan/apply/EndState are generic |
| `server/internal/orchestrator/swarm/resources.go` | No new resource types needed |
| `server/internal/orchestrator/swarm/network.go` | Network is shared, unchanged |
| `server/internal/orchestrator/swarm/service_instance.go` | Docker service deploy is generic |
| `server/internal/monitor/service_instance_monitor_resource.go` | Health monitor is generic |
| `server/internal/workflows/plan_update.go` | Service resource generation is generic |
| `server/internal/database/mcp_service_config.go` | MCP-specific, not touched |
| `server/internal/orchestrator/swarm/mcp_config*.go` | MCP-specific, not touched |

---

## 14. Dependency on PR #280

This implementation doc assumes PR #280 is merged. The following table shows what
changes if PostgREST work starts before the merge:

| Touch point | Post-PR-280 (this doc) | Pre-PR-280 |
|-------------|----------------------|------------|
| `buildServiceEnvVars()` | Deleted. Add `buildPostgRESTEnvVars()` separately. | Still exists. Add PostgREST branch inside existing function. |
| `ServiceContainerSpec()` | Has MCP-specific `Command`, `Args`, `User`, `Mounts`. Add `case "postgrest"` to the switch. | Has generic `Env: buildServiceEnvVars(opts)`. Add PostgREST env vars to the existing function. |
| `ServiceUserRole.Create()` | Has fine-grained grants (public schema, `pg_read_all_settings`). Add PostgREST branch. | Has `Roles: []string{"pgedge_application_read_only"}`. Add PostgREST-specific `ALTER ROLE NOINHERIT` and `GRANT web_anon` after role creation. |
| `service_instance_spec.go` | Has `DataDirID`, `MCPConfigResource` dependency. Make conditional for PostgREST. | No `DataDirID`. Dependencies are only Network + ServiceUserRole. PostgREST needs no changes here. |
| `orchestrator.go` | Has MCP config parsing, DirResource, MCPConfigResource. Add PostgREST branch that skips these. | No MCP-specific resources. PostgREST uses the four base resources directly (no changes needed). |

---

## Change Summary

| # | File | Change | Lines (est.) |
|---|------|--------|-------------|
| 1 | `api/apiv1/design/database.go` | Add `"postgrest"` to enum | 2 |
| 2 | `server/internal/api/apiv1/validate.go` | Add allowlist entry + dispatch + `validatePostgRESTServiceConfig()` | 70 |
| 3 | `server/internal/api/apiv1/validate_test.go` | Add PostgREST validation tests | 80 |
| 4 | `server/internal/orchestrator/swarm/service_images.go` | Register PostgREST images | 8 |
| 5 | `server/internal/orchestrator/swarm/service_images_test.go` | Add PostgREST image tests | 20 |
| 6 | `server/internal/orchestrator/swarm/service_spec.go` | Add `buildPostgRESTEnvVars()` + service-type branching in `ServiceContainerSpec()` | 60 |
| 7 | `server/internal/orchestrator/swarm/service_spec_test.go` | Add PostgREST container spec tests | 80 |
| 8 | `server/internal/orchestrator/swarm/service_user_role.go` | Add `ServiceType`/`AnonRole` fields + PostgREST grants in `Create()` | 30 |
| 9 | `server/internal/orchestrator/swarm/orchestrator.go` | Add PostgREST branch in `GenerateServiceInstanceResources()` | 40 |
| 10 | `server/internal/orchestrator/swarm/service_instance_spec.go` | Conditional `DataDirID`/dependency handling | 10 |
| 11 | `e2e/service_provisioning_test.go` | Add PostgREST E2E tests | 150 |
| | **Total** | | **~550** |
