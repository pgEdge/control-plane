# Integrating the RAG Server into the Control Plane

This document describes how the `rag` service type was added to the pgEdge control plane. Use it as a concrete worked example alongside the generic [supported-services guide](supported-services.md).

---

## Overview

The RAG service runs the `pgedge/rag-server` container co-located with a Postgres instance. It performs hybrid vector + keyword search against a pgvector table and returns LLM-synthesised answers. It differs from MCP in two important ways:

1. It requires a **config file** (YAML) delivered via Docker Swarm config rather than environment variables alone.
2. It requires **schema setup** — pgvector extension, table creation, HNSW index, and SELECT grants — before the container can start.

These two requirements add two extra resources to the standard service lifecycle.

---

## Resource lifecycle

Standard services use 5 resources. RAG uses 7:

```
Phase 1:  NetworkResource              (swarm.network)
          ServiceUserRole              (swarm.service_user_role)
          ServiceConfigResource        (swarm.service_config)        ← RAG only

Phase 2:  RAGSchemaResource            (swarm.rag_schema)            ← RAG only
          ServiceInstanceSpecResource  (swarm.service_instance_spec)

Phase 3:  ServiceInstanceResource      (swarm.service_instance)

Phase 4:  ServiceInstanceMonitor       (monitor.service_instance)
```

`ServiceInstanceSpecResource` depends on both `ServiceConfigResource` and `RAGSchemaResource` before it can build the container spec. This ordering ensures the Swarm config ID is available to mount into the container and the schema exists before the container boots.

---

## Files changed

| File | What changed |
|------|-------------|
| `api/apiv1/validate.go` | Added `"rag"` to service type allowlist; added `validateRAGServiceConfig()` |
| `orchestrator/swarm/service_images.go` | Registered `ghcr.io/pgedge/rag-server:main` under `"rag"` |
| `orchestrator/swarm/rag_config.go` | New file — renders `pgedge-rag-server.yaml` from `ServiceSpec.Config` |
| `orchestrator/swarm/rag_schema.go` | New file — `RAGSchemaResource` lifecycle resource |
| `orchestrator/swarm/service_spec.go` | Added `serviceHealthCheckTest()`, `serviceConfigMountPath()` branching for RAG; changed port publish mode to Ingress |
| `orchestrator/swarm/service_instance_spec.go` | Added `SwarmConfigID` population; added `ServiceConfigResource` and `RAGSchemaResource` as dependencies |
| `orchestrator/swarm/orchestrator.go` | Added RAG branch in `GenerateServiceInstanceResources()` to create `ServiceConfigResource` and `RAGSchemaResource` |
| `orchestrator/swarm/service_user_role.go` | Fixed `connectToPrimary()` to use co-located Patroni node; added existence check in `Refresh()` |

---

## Step 1 — API validation

**File:** `api/apiv1/validate.go`

Add `"rag"` to the service type allowlist in `validateServiceSpec()`:

```go
validServiceTypes := map[string]bool{
    "mcp": true,
    "rag": true,
}
```

Add a dispatcher in `validateServiceSpec()`:

```go
switch spec.ServiceType {
case "rag":
    errs = append(errs, validateRAGServiceConfig(spec.Config, append(path, "config"))...)
case "mcp":
    errs = append(errs, validateMCPServiceConfig(spec.Config, append(path, "config"))...)
}
```

Implement `validateRAGServiceConfig()`. Required fields:

| Field | Rule |
|-------|------|
| `embedding_provider` | one of `"openai"`, `"voyage"`, `"ollama"` |
| `embedding_model` | non-empty string |
| `llm_provider` | one of `"anthropic"`, `"openai"`, `"ollama"` |
| `llm_model` | non-empty string |
| `tables` | array, at least 1 entry; each entry needs `table`, `text_column`, `vector_column` |

Conditionally required based on provider:

| Condition | Required field |
|-----------|---------------|
| `embedding_provider == "openai"` or `llm_provider == "openai"` | `openai_api_key` |
| `llm_provider == "anthropic"` | `anthropic_api_key` |
| `embedding_provider == "voyage"` | `voyage_api_key` |
| `embedding_provider == "ollama"` or `llm_provider == "ollama"` | `ollama_url` |

API keys are validated as present-and-non-empty strings. They are **never** returned in API responses — the sensitive key filter in the API layer strips any key containing `api_key`, `secret`, `token`, `password`, `credential`, `private_key`, or `access_key`.

---

## Step 2 — Image registration

**File:** `orchestrator/swarm/service_images.go`

```go
versions.addServiceImage("rag", "latest", &ServiceImage{
    Tag: serviceImageTag(cfg, "ghcr.io/pgedge/rag-server:main"),
})
```

`serviceImageTag()` allows the image repository to be overridden via the orchestrator config, which is how dev/staging environments point to a private registry.

Version constraints (`PostgresConstraint`, `SpockConstraint`) can be added here when the RAG server has minimum Postgres version requirements.

---

## Step 3 — Config file rendering

**File:** `orchestrator/swarm/rag_config.go`

The RAG server reads its full configuration from a YAML file at `/etc/pgedge/pgedge-rag-server.yaml`. API keys are **not** written to this file — they are injected as environment variables so they never land on disk in a Swarm config object.

`generateRAGConfig(opts *ragConfigOptions) (string, error)` renders the YAML from `ServiceSpec.Config`. The output shape:

```yaml
server:
  listen_address: "0.0.0.0"
  port: 8080

defaults:
  token_budget: 4000
  top_n: 10

pipelines:
  - name: "default"
    database:
      host: "postgres-<instanceID>"
      port: 5432
      database: "storefront"
      username: "svc_rag_host_1"
      password: "<generated>"
      ssl_mode: "prefer"
    tables:
      - table: "documents_content_chunks"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "openai"
      model: "text-embedding-3-small"
    rag_llm:
      provider: "anthropic"
      model: "claude-sonnet-4-5"
```

Helper functions:

- `stringConfigField(cfg, key, default)` — returns `cfg[key]` as string or `default`
- `intConfigField(cfg, key, default)` — returns `cfg[key]` as int (handles both `int` and `float64` since JSON numbers deserialise as `float64`)
- `buildRAGTablesYAML(cfg)` — iterates `cfg["tables"].([]any)` and renders the YAML tables block

---

## Step 4 — ServiceConfigResource

**File:** `orchestrator/swarm/orchestrator.go`

`ServiceConfigResource` stores the rendered YAML in a Docker Swarm config object and tracks the resulting config ID. It is created in Phase 1 so that `ServiceInstanceSpecResource` can reference its ID when building the container spec.

In `GenerateServiceInstanceResources()`:

```go
var serviceConfig *ServiceConfigResource
var ragSchema *RAGSchemaResource

if spec.ServiceSpec.ServiceType == "rag" {
    serviceConfig = &ServiceConfigResource{
        ServiceInstanceID: spec.ServiceInstanceID,
        DatabaseID:        spec.DatabaseID,
        ServiceSpec:       spec.ServiceSpec,
        DatabaseHost:      spec.DatabaseHost,
        DatabasePort:      spec.DatabasePort,
        DatabaseName:      spec.DatabaseName,
        // credentials populated after ServiceUserRole runs
    }
    ragSchema = &RAGSchemaResource{
        ServiceInstanceID: spec.ServiceInstanceID,
        DatabaseID:        spec.DatabaseID,
        ServiceSpec:       spec.ServiceSpec,
        HostID:            spec.HostID,
        PostgresHostID:    spec.PostgresHostID,
    }
}
```

The `ServiceConfigResource`:
- **Create**: calls `generateRAGConfig()` to render the YAML, then calls `docker config create` via the Docker SDK with label `pgedge.component=service-config`
- **Refresh**: checks whether the Swarm config object still exists by ID
- **Delete**: removes the Swarm config object
- Stores the Swarm config ID in its own state so `ServiceInstanceSpecResource` can read it

---

## Step 5 — RAGSchemaResource

**File:** `orchestrator/swarm/rag_schema.go`

This resource ensures the Postgres schema is ready before the container starts. It runs in Phase 2, after `ServiceUserRole` has created the service user.

### What `Create()` / `setup()` does

1. Connects to the co-located Patroni **primary** using the `admin` database user
2. Creates the `pgvector` extension: `CREATE EXTENSION IF NOT EXISTS vector`
3. For each table in `ServiceSpec.Config["tables"]`:
   - Determines vector dimensions from the embedding model name (see `embeddingDimensions()`)
   - `CREATE TABLE IF NOT EXISTS <table> (id BIGSERIAL PRIMARY KEY, <text_col> TEXT, <vector_col> vector(<dims>))`
   - `CREATE INDEX IF NOT EXISTS ... USING hnsw (<vector_col> vector_cosine_ops)`
   - `GRANT SELECT ON <table> TO <service_user>`
4. Uses `execDDLIdempotent()` which ignores `42P07` (duplicate table) and `42710` (duplicate object) errors to make the operation safe to re-run

### Embedding dimension lookup

```go
func embeddingDimensions(model string) int {
    switch model {
    case "text-embedding-3-large":
        return 3072
    case "text-embedding-3-small", "text-embedding-ada-002":
        return 1536
    // voyage, etc.
    default:
        return 1536
    }
}
```

If a new embedding model is introduced, add it here. There is currently no runtime validation that the vector column dimensions match the model — mismatches produce silent failures at query time.

### Co-located instance routing

`connectToDatabase()` must connect to the **primary** of the Postgres cluster co-located with the service host, not just any primary in the database.

```go
// 1. Find the co-located seed instance by HostID
targetHostID := r.PostgresHostID
if targetHostID == "" {
    targetHostID = r.HostID
}
var seedInstance *database.Instance
for i := range db.Instances {
    if db.Instances[i].HostID == targetHostID {
        seedInstance = db.Instances[i]
        break
    }
}
if seedInstance == nil {
    seedInstance = db.Instances[0]  // fallback with warning
}

// 2. Ask that instance's Patroni for the current primary
var primaryInstanceID string
{
    connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.DatabaseID, seedInstance.InstanceID)
    if err == nil {
        patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
        primaryID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 10*time.Second)
        if err == nil && primaryID != "" {
            primaryInstanceID = primaryID
        }
    }
}
if primaryInstanceID == "" {
    primaryInstanceID = seedInstance.InstanceID  // fallback with warning
}
```

This same pattern is used in `service_user_role.go`. **Do not use a global primary search** — in a multi-node Spock setup each independent Patroni cluster has its own primary, and `CREATE ROLE` / DDL must land on the correct one.

---

## Step 6 — Container spec changes

**File:** `orchestrator/swarm/service_spec.go`

### Health check

The RAG server image does not ship `curl` or `wget`, so the standard health check pattern fails. Instead, use a bash built-in TCP check:

```go
func serviceHealthCheckTest(serviceType string) []string {
    if serviceType == "rag" {
        return []string{"CMD-SHELL", "exec 3<>/dev/tcp/127.0.0.1/8080"}
    }
    return []string{"CMD-SHELL", fmt.Sprintf("curl -f http://localhost:8080%s || exit 1",
        serviceHealthCheckPath(serviceType))}
}
```

### Config file mount

```go
func serviceConfigMountPath(serviceType string) string {
    if serviceType == "rag" {
        return "/etc/pgedge/pgedge-rag-server.yaml"
    }
    return ""
}
```

When `opts.SwarmConfigID != ""`, the container spec adds a `swarm.ConfigReference` entry pointing to the Swarm config object, mounted at the path above with mode `0o444`.

### Environment variables

`buildRAGEnvVars()` injects only API keys as env vars (the rest of the config lives in the YAML file):

```go
func buildRAGEnvVars(config map[string]any) []string {
    var env []string
    if key, ok := config["openai_api_key"].(string); ok && key != "" {
        env = append(env, fmt.Sprintf("OPENAI_API_KEY=%s", key))
    }
    if key, ok := config["anthropic_api_key"].(string); ok && key != "" {
        env = append(env, fmt.Sprintf("ANTHROPIC_API_KEY=%s", key))
    }
    if key, ok := config["voyage_api_key"].(string); ok && key != "" {
        env = append(env, fmt.Sprintf("VOYAGE_API_KEY=%s", key))
    }
    return env
}
```

Standard libpq env vars (`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`) are also set by `buildServiceEnvVars()` for all service types.

### Port publish mode

Use `swarm.PortConfigPublishModeIngress` (not `Host`). With `Host` mode, only one service can bind a given port per Swarm node — which blocks deployment in single-node dev environments where multiple services share the same node.

---

## Step 7 — ServiceInstanceSpecResource dependencies

**File:** `orchestrator/swarm/service_instance_spec.go`

`Dependencies()` must declare `ServiceConfigResource` and `RAGSchemaResource` so the resource executor waits for them before building the container spec:

```go
func (r *ServiceInstanceSpecResource) Dependencies() []resource.Identifier {
    deps := []resource.Identifier{
        NetworkResourceIdentifier(r.DatabaseNetworkID),
        ServiceUserRoleIdentifier(r.ServiceInstanceID),
    }
    if r.ServiceSpec.ServiceType == "rag" {
        deps = append(deps,
            ServiceConfigResourceIdentifier(r.ServiceInstanceID),
            RAGSchemaResourceIdentifier(r.ServiceInstanceID),
        )
    }
    return deps
}
```

In `Refresh()`, populate `SwarmConfigID` from the resolved `ServiceConfigResource`:

```go
if r.ServiceSpec.ServiceType == "rag" {
    configRes, err := resource.Resolve[*ServiceConfigResource](rc, ServiceConfigResourceIdentifier(r.ServiceInstanceID))
    if err == nil && configRes != nil {
        r.SwarmConfigID = configRes.SwarmConfigID
    }
}
```

---

## Step 8 — ServiceUserRole co-located routing fix

**File:** `orchestrator/swarm/service_user_role.go`

The same co-located primary lookup described in Step 5 applies here. The `connectToPrimary()` function must find the Patroni primary of the cluster co-located with the service host, not the first primary found across all clusters.

Additionally, `Refresh()` must verify the role actually exists on that node — not just check whether `Username` is non-empty in local state. After a failed provisioning attempt, stale state can record credentials that were created on the wrong Postgres node:

```go
func (r *ServiceUserRole) Refresh(ctx context.Context, rc *resource.Context) error {
    if r.Username == "" || r.Password == "" {
        return resource.ErrNotFound
    }
    conn, err := r.connectToPrimary(ctx, rc, logger)
    if err != nil {
        return fmt.Errorf("failed to reach postgres to verify service user: %w", err)
    }
    defer conn.Close(ctx)
    var exists bool
    if err := conn.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", r.Username,
    ).Scan(&exists); err != nil {
        return fmt.Errorf("failed to check for service user %q: %w", r.Username, err)
    }
    if !exists {
        return resource.ErrNotFound  // triggers Create() with corrected routing
    }
    return nil
}
```

---

## Multi-node Spock considerations

| Behaviour | Detail |
|-----------|--------|
| `CREATE ROLE` is **not** replicated | Each independent Patroni cluster (n1, n2, n3) needs the service user created on its own primary. The co-located routing in Steps 5 and 8 handles this. |
| Schema DDL (`CREATE TABLE`, `CREATE EXTENSION`) **is** replicated by Spock | Running `RAGSchemaResource.Create()` on n1's primary is sufficient — the table and index propagate to n2 and n3. |
| `GRANT SELECT` **is** replicated | Grants on the table replicate, so the service user on n2/n3 can read the table after it is granted on n1. |

---

## Cleanup for re-testing

```bash
# Remove all RAG services and their Swarm configs
docker service ls --filter name=rag- --format '{{.Name}}' | xargs -r docker service rm
docker config ls --filter label=pgedge.component=service-config --format '{{.Name}}' | xargs -r docker config rm
```

---

## Adding a new config field to RAG

1. **`validate.go`** — add the field to `validateRAGServiceConfig()` (required or optional, with any range/enum constraints)
2. **`rag_config.go`** — add the field to `generateRAGConfig()` in the YAML output (or to `buildRAGEnvVars()` if it is a secret)
3. If it changes the rendered YAML, `ServiceConfigResource.Update()` must re-create the Swarm config and return a new `SwarmConfigID` — this triggers `ServiceInstanceSpecResource` to rebuild, which triggers a service update

---

## Adding a new embedding model

Add the model name and its vector dimension to `embeddingDimensions()` in `rag_schema.go`:

```go
case "text-embedding-new-model":
    return 768
```

Also add the model to the allowlist in `validateRAGServiceConfig()` if the validator enforces a model enum. Currently it accepts any non-empty string.

---

## Open questions / known gaps

| Item | Detail |
|------|--------|
| **No ingestion API** | Documents must be inserted into Postgres with pre-computed embeddings by the caller. The control plane does not provide any ingestion endpoint or helper. |
| **No dimension validation at provisioning time** | If `embedding_model` and the existing table's vector column dimension don't match, the service starts and returns errors only at query time. |
| **Swarm config update requires new config object** | Docker Swarm configs are immutable. Updating the RAG YAML config creates a new config object and removes the old one. The service must be restarted to pick up the new mount. |
| **Single-node dev port access** | Docker Desktop does not forward Swarm ingress ports to Mac localhost. Use a `docker run -p <port>:<port> alpine/socat` proxy container as a workaround. |
