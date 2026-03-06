# PostgREST Service: Technical Q&A

A structured deep-dive into PostgREST as a Control Plane service type — what it is, how it runs, what it needs, and where the current model has gaps.

---

## 1. What Is This Service and Why Does It Live Next to the Database?

PostgREST is a standalone web server (written in Haskell) that reads a PostgreSQL schema and automatically generates a RESTful HTTP API from it. Tables become endpoints, columns become fields, foreign keys become nested resources, and PostgreSQL's own role/grant system becomes the authorization layer. There is no application code — the database schema *is* the API contract.

**Why it lives next to the database:**

- **Zero network hops to Postgres.** PostgREST connects over the Docker overlay network to the Postgres instance on the same host. Latency is sub-millisecond. Deploying it remotely adds a network round-trip to every request.
- **Credential isolation.** The Control Plane generates a dedicated `svc_postgrest_{host_id}` user per instance and injects it as environment variables. No credentials leave the overlay network.
- **Failover tracking (planned).** Once multi-host `PGHOST` is implemented (see Gap 2), the container's environment will have all node hostnames. Combined with `PGTARGETSESSIONATTRS=read-write`, libpq will automatically reconnect to the new primary after a Patroni switchover. **Today**, the Control Plane sets a single `PGHOST` value, so failover requires a manual restart or redeploy.
- **Lifecycle parity with other services.** PostgREST follows the same resource model as MCP: `Network → ServiceUserRole → ServiceInstanceSpec → ServiceInstance`. The orchestrator, workflow engine, and monitor treat it identically. No new framework is needed.

**Comparison with MCP:**

| Dimension | MCP | PostgREST |
|-----------|-----|-----------|
| Purpose | AI/LLM-powered queries | Schema-derived REST API |
| Database access | Read-only (public-schema `SELECT` + `pg_read_all_settings`, post-PR-280) | Configurable. Read-only by default (via `web_anon` grants); write possible with RLS + authenticated roles |
| External dependencies | LLM provider API keys | None |
| Config complexity | LLM provider, model, API key | Schema name, anonymous role, pool size, row limit |
| Port | 8080 internal, user-chosen published | 8080 internal, user-chosen published |

---

## 2. What Does Its Config Look Like?

PostgREST config lives in the `config` map of `ServiceSpec`. The Control Plane translates these into `PGRST_*` and `PG*` environment variables at container creation time.

### Required Fields

| Field | Type | Validation | Maps To |
|-------|------|------------|---------|
| `db_schemas` | `string` | Non-empty. Comma-separated list of PostgreSQL schema names. Recommended: `"api"` only. | `PGRST_DB_SCHEMAS` |
| `db_anon_role` | `string` | Non-empty. Must be a valid PostgreSQL role name (no spaces, no special chars). | `PGRST_DB_ANON_ROLE` |

### Optional Fields

| Field | Type | Default | Validation | Maps To |
|-------|------|---------|------------|---------|
| `db_pool` | `number` | 10 | Integer, range 1–30. Values above 30 risk exhausting `max_connections`. | `PGRST_DB_POOL` |
| `max_rows` | `number` | 1000 | Integer, range 1–10000. Caps rows returned per response. Does not prevent full table scans in Postgres — use `statement_timeout` for that. | `PGRST_DB_MAX_ROWS` |

### Hardcoded (Not User-Configurable)

These are set by the Control Plane unconditionally:

| Variable | Value | Reason |
|----------|-------|--------|
| `PGRST_DB_URI` | `postgresql://` | Minimal base URI. Actual connection details come from `PGHOST`, `PGPORT`, etc. Keeps credentials out of the connection string. |
| `PGSSLMODE` | `prefer` | Use TLS if available, fall back to unencrypted. Matches the Control Plane's default for all service types. |
| `PGTARGETSESSIONATTRS` | `read-write` | Ensures libpq always connects to the current primary. Only effective once multi-host `PGHOST` is implemented (Gap 2). |
| `PGRST_DB_POOL_ACQUISITION_TIMEOUT` | `10` | Seconds to wait for a pool connection. Requests during failover get HTTP 503 rather than hanging. |
| `PGRST_SERVER_PORT` | `8080` | Must match the Control Plane health check target port. |
| `PGRST_LOG_LEVEL` | `warn` | Avoids log noise. Change to `info` or `debug` only for troubleshooting. |
| `PGRST_DB_CHANNEL_ENABLED` | `true` | Allows `NOTIFY pgrst, 'reload schema'` to refresh the schema cache without restarting. |

### Validation Rules

```
validatePostgRESTServiceConfig(config, path):
  1. config["db_schemas"] must exist and be a non-empty string
  2. config["db_anon_role"] must exist and be a non-empty string
  3. If config["db_pool"] exists:
       - must be a number (float64 from JSON)
       - must be an integer in range [1, 30]
  4. If config["max_rows"] exists:
       - must be a number (float64 from JSON)
       - must be an integer in range [1, 10000]
  5. Unknown keys are silently ignored (forward-compatible)
```

### Example Spec

```json
{
  "service_id": "postgrest",
  "service_type": "postgrest",
  "version": "14.5",
  "host_ids": ["host-abc"],
  "port": 3100,
  "config": {
    "db_schemas": "api",
    "db_anon_role": "web_anon",
    "db_pool": 10,
    "max_rows": 1000
  }
}
```

---

## 3. How Does the User Utilize It After It's Running?

### REST API (Automatic)

Once deployed, PostgREST exposes every table, view, and function in the configured schema as HTTP endpoints:

| Operation | HTTP | Example |
|-----------|------|---------|
| List rows | `GET /table` | `GET /gold_summaries?limit=10` |
| Filter rows | `GET /table?col=op.val` | `GET /gold_summaries?year=eq.2025&order=date.desc` |
| Get single row | `GET /table?id=eq.1` + `Accept: application/vnd.pgrst.object+json` | Single JSON object |
| Insert rows | `POST /table` | `POST /gold_summaries` with JSON body |
| Update rows | `PATCH /table?filter` | `PATCH /gold_summaries?id=eq.1` |
| Upsert | `PUT /table` | With `Prefer: resolution=merge-duplicates` |
| Delete rows | `DELETE /table?filter` | `DELETE /gold_summaries?id=eq.1` |
| Call function | `POST /rpc/fn_name` | `POST /rpc/search_gold` with JSON args |
| OpenAPI spec | `GET /` | Full schema introspection |
| Health check | `GET /health` | Returns 200 if connected to Postgres |
| Readiness | `GET /ready` | Returns 200 if schema cache is loaded |

### Filtering Operators

`eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `like`, `ilike`, `in`, `is`, `cs` (contains), `cd` (contained by), `ov` (overlaps), `fts` (full-text search).

Boolean logic: `?and=(col1.eq.a,col2.gt.5)`, `?or=(...)`.

### Embedding (Relationships)

PostgREST auto-detects foreign keys and allows nested resource fetching:

```
GET /posts?select=id,title,comments(id,text)
```

### Pagination

```
GET /table?limit=25&offset=50
```

Response headers include `Content-Range` for total count when `Prefer: count=exact` is set.

### Runtime Settings That Can Change Without Redeploying

| Setting | How to Change | Effect |
|---------|---------------|--------|
| Schema cache | `NOTIFY pgrst, 'reload schema'` (SQL) | PostgREST re-introspects the database. Use after DDL changes (new tables, columns, functions). |
| Row-level security policies | `ALTER POLICY` / `CREATE POLICY` (SQL) | Takes effect immediately on next request. No PostgREST restart needed. |
| Grant/revoke permissions | `GRANT SELECT ON api.new_table TO web_anon` (SQL) | Takes effect after schema cache reload. |
| Statement timeout | `ALTER ROLE svc_postgrest_host1 SET statement_timeout = '30s'` (SQL) | Takes effect on new connections. Kills long-running queries. |
| JWT secret | Requires container restart (env var change) | Must redeploy via Control Plane spec update. |

### What the User Should NOT Do

- Do not expose PostgREST directly to the internet without a reverse proxy (rate limiting, TLS, auth).
- Do not grant `INSERT`/`UPDATE`/`DELETE` to `web_anon` without RLS policies on every affected table.
- Do not add `SECURITY DEFINER` functions to the `api` schema — place them in a `private` schema.

---

## 4. How Does It Run?

### Container Image

| Version | Image Tag | Postgres Constraint |
|---------|-----------|---------------------|
| `14.5` | `postgrest/postgrest:v14.5` | Postgres >= 15 (as defined in the implementation guide) |
| `latest` | `postgrest/postgrest:latest` | None |

PostgREST v14.5 is the latest stable release (Feb 2026). v14 dropped support for PostgreSQL 12 (EOL) but supports PostgreSQL 13+. The Postgres >= 15 constraint in the implementation guide is stricter than PostgREST's own minimum — it reflects a pgEdge platform decision. The `latest` entry has no constraints and is intended for development.

### Health Check

**Docker health check:** Disabled (`NONE`) for PostgREST. The PostgREST Docker image
is a static binary with no shell utilities (no `curl`, no `wget`), so the standard
`curl -f http://localhost:8080/health` command fails.

**PostgREST admin endpoints:** PostgREST v14 serves `/health`, `/live`, and `/ready`
on a separate admin port (configured via `PGRST_ADMIN_SERVER_PORT=3001`), not the API
port (8080). These endpoints are:
- `/health` — returns 200 if PostgREST has at least one usable DB connection
- `/ready` — returns 200 if the schema cache is loaded
- `/live` — alias for `/health`

**CP health monitoring:** The `ServiceInstanceMonitor` checks health from outside the
container via the bridge network, using the published port. This still works.

**MCP comparison:** MCP uses `curl -f http://localhost:8080/health` because the MCP
image includes curl. PostgREST cannot use this approach.

### Port

- **Internal (container):** 8080 — hardcoded via `PGRST_SERVER_PORT=8080`. Matches the health check and all Control Plane assumptions.
- **Published (host):** User-configurable via `spec.port`. Set to `3100` in the design spec example. `nil` = not published, `0` = Docker assigns random port.
- **Publish mode:** Host mode (`swarm.PortConfigPublishModeHost`) — traffic hits the specific Swarm node, not a VIP.

### Resource Defaults

| Resource | Default | Notes |
|----------|---------|-------|
| CPUs | Container default (no limit) | User can set via `spec.cpus` (e.g., `"0.5"`, `"1"`, `"500m"`) |
| Memory | Container default (no limit) | User can set via `spec.memory` (e.g., `"512M"`, `"1GiB"`) |
| Volumes | None | PostgREST is stateless. No persistent mounts. |
| Replicas | 1 per host_id | One ServiceInstance per host in `host_ids`. |

### Differences from MCP

| Aspect | MCP | PostgREST |
|--------|-----|-----------|
| Image source | `postgres-mcp:latest` (custom) | `postgrest/postgrest:v14.5` (upstream) |
| Version constraints | None | Postgres >= 15 for v14.5 |
| Config env vars | `PGEDGE_LLM_PROVIDER`, `PGEDGE_LLM_MODEL`, provider API keys | `PGRST_DB_SCHEMAS`, `PGRST_DB_ANON_ROLE`, `PGRST_DB_POOL`, `PGRST_DB_MAX_ROWS`, plus hardcoded PGRST defaults |
| Extra PG env vars | None | `PGTARGETSESSIONATTRS=read-write` |
| DB role attributes | Default (INHERIT) | NOINHERIT required |
| External dependencies | LLM provider internet access | None |
| Schema requirements | None specific | `api` schema + `web_anon` role + grants must exist before deploy |

---

## 5. What Does It Need from Postgres?

### Access Level

**Default: read-only.** The `web_anon` anonymous role is granted `SELECT` and `EXECUTE` only. Write access is possible but requires:
1. Creating authenticated roles (e.g., `app_user`) with `INSERT`/`UPDATE`/`DELETE`.
2. Enabling row-level security (RLS) on every writable table.
3. Configuring JWT authentication so PostgREST can `SET ROLE` to the authenticated role.

### Users and Roles

| Role | Created By | Attributes | Purpose |
|------|-----------|------------|---------|
| `svc_postgrest_{host_id}` | Control Plane (ServiceUserRole) | `LOGIN` today; needs `NOINHERIT` (Gap 1) | Authenticator role. PostgREST connects as this user, then `SET ROLE` to the request role. |
| `web_anon` | DBA (manual) | `NOLOGIN` | Anonymous request role. Used when no JWT is provided. Must be granted to the service user (Gap 4 — not yet automated). |
| `app_user` (optional) | DBA (manual) | `NOLOGIN` | Authenticated request role. Used when a valid JWT with `"role": "app_user"` is provided. |

**Why NOINHERIT matters:** PostgREST uses the PostgreSQL `SET ROLE` mechanism. The authenticator role must NOT automatically inherit the anonymous role's privileges — otherwise it would have ambient access to everything `web_anon` can do, defeating role-based isolation. `NOINHERIT` ensures `SET ROLE` is the only way to gain permissions.

> **Current state:** Today `ServiceUserRole.Create()` creates users with `LOGIN` and default `INHERIT`. Both `NOINHERIT` and `GRANT web_anon TO svc_postgrest_{host_id}` must be added for PostgREST to function (Gaps 1 and 4).

### Schema Requirements

| Schema | Purpose | Created By |
|--------|---------|------------|
| `api` | Exposed to HTTP clients. Contains views and functions only. | DBA (manual, before deploy) |
| `private` (recommended) | Business logic, helper functions. Not exposed. | DBA (manual) |
| `internal` (recommended) | Raw tables, internal data. Not exposed. | DBA (manual) |

### Required Grants (Before Deploy)

```sql
GRANT USAGE ON SCHEMA api TO web_anon;
GRANT SELECT ON ALL TABLES IN SCHEMA api TO web_anon;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA api TO web_anon;
ALTER DEFAULT PRIVILEGES IN SCHEMA api GRANT SELECT ON TABLES TO web_anon;
ALTER DEFAULT PRIVILEGES IN SCHEMA api GRANT EXECUTE ON FUNCTIONS TO web_anon;

-- After Control Plane creates the service user:
GRANT web_anon TO svc_postgrest_host1;
```

### Extensions

No extensions are strictly required. Common companions:

| Extension | Purpose |
|-----------|---------|
| `pgvector` | Similarity search via `api` functions |
| `pg_stat_statements` | Query performance monitoring |
| `pg_trgm` | Fuzzy text search support |

### Row-Level Security

RLS must be enabled on every table backing an `api` view if write access is granted:

```sql
ALTER TABLE internal.my_table ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal.my_table FORCE ROW LEVEL SECURITY;
```

Without `FORCE`, table owners bypass policies silently.

---

## 6. What Are Its Networking Needs?

### Network Attachments

PostgREST attaches to two Docker networks (same as MCP):

1. **Bridge network** — Local to the host.
   - Control Plane reaches the container on port 8080 for health checks.
   - End-users reach the published port (e.g., 3100) from outside Docker.

2. **Database overlay network** (`{database_id}-database`) — Spans the Swarm cluster.
   - PostgREST connects to Postgres instances via overlay DNS: `{hostname}.{database_id}-database`.
   - Isolated per database. Services for database A cannot reach Postgres for database B.

### Port Allocation

| Port | Scope | Purpose |
|------|-------|---------|
| 8080 | Container-internal | HTTP API. Used by health checks and inter-service calls. |
| User-configured (e.g., 3100) | Host-published | External access. Published in host mode, not VIP mode. |

**Conflict risk:** If two services on the same host both request port 3100, the second deployment fails. Docker Swarm rejects duplicate host-port bindings at deployment time. The Control Plane has a `validatePortAvailable` check in some node-update flows, but it is not confirmed to run for every new service deploy — port conflicts may surface as Swarm errors rather than pre-validated API errors. Users must coordinate published ports across services on the same host.

### Service Discovery

- **Intra-overlay:** Other containers on the same database overlay can reach PostgREST at `{service_name}:{8080}` via Docker DNS.
- **From host:** Access via `localhost:{published_port}` or `{host_ip}:{published_port}`.
- **Cross-database:** Not possible by default. Overlay networks are per-database. A service in database A cannot reach PostgREST in database B without explicit network configuration.

### Interaction with Other Services

- PostgREST does not conflict with MCP or other services on the overlay network — they share the network but listen on different container names.
- The only collision vector is **published host ports**. Two services requesting the same host port will fail.
- PostgREST does not need outbound internet access (unlike MCP, which calls LLM provider APIs).

---

## 7. What Doesn't Fit?

### Gap 1: NOINHERIT on Service User Role

**Problem:** `ServiceUserRole.Create()` currently creates all service users with default PostgreSQL attributes (which means `INHERIT`). PostgREST requires `NOINHERIT`. MCP does not.

**Current workaround in the implementation guide:**
```go
if opts.ServiceType == "postgrest" {
    _, err = conn.Exec(ctx, fmt.Sprintf(
        "ALTER ROLE %s NOINHERIT", sanitizeIdentifier(r.Username)))
}
```

**Concern:** This is a service-type-specific `if` statement in a generic resource. As more service types are added, this becomes a growing switch statement.

**Options:**
1. **Per-service-type flag:** Add a `NoInherit bool` field to `ServiceUserRole` or `ServiceSpec`, set by the orchestrator based on service type.
2. **Role options struct:** Pass `postgres.UserRoleOptions` with an `Inherit` field, letting each service type declare its needs declaratively.
3. **Accept the if-statement for now.** Two service types don't warrant abstraction. Revisit at three.

### Gap 2: No Primary-Awareness in Service Provisioning

**Problem:** The platform has no mechanism for a service container to discover or follow the current Postgres primary. This is not PostgREST-specific — it affects every service type.

**Root cause:** The provisioning logic in `server/internal/workflows/provision_services.go` (lines ~159–200) picks a single Postgres instance at provisioning time — preferring same-host, falling back to first found — and bakes it into the container's `PGHOST` env var. There is no primary/replica awareness.

**Impact on PostgREST:** After a Patroni switchover, PostgREST keeps connecting to the old primary (now a replica). Read requests may still work, but the schema cache reload channel (`NOTIFY`) only works on the primary. If PostgREST was intended to serve writes via authenticated roles, those fail immediately.

**Impact on MCP:** MCP's `allow_writes: true` mode needs write access to Postgres. With the current provisioning logic, an MCP container could be connected to a replica that can't accept writes, or could lose write access after failover. `allow_writes` cannot work reliably until this is resolved.

**Options under consideration:**

**A) Patroni routing endpoint.** Set `PGHOST` to a Patroni-aware proxy (HAProxy or pgBouncer) that health-checks each node's `/primary` endpoint and routes to the current primary.

| Pro | Con |
|-----|-----|
| `PGHOST` stays a single string — no `DatabaseHosts` refactor | Adds a proxy component to deploy, monitor, and fail over |
| Solves failover for all services uniformly | Extra network hop on every query (not just during failover) |
| If pgBouncer is planned for connection pooling, failover comes free | Single point of failure if the proxy goes down |

**B) Multi-host PGHOST.** Set `PGHOST=host1,host2,host3` with `PGTARGETSESSIONATTRS=read-write`. libpq tries each host natively and connects to the one accepting writes.

| Pro | Con |
|-----|-----|
| No extra infrastructure — uses libpq's built-in multi-host support | Requires `DatabaseHost string` → `DatabaseHosts []string` across the domain model |
| No additional latency — direct connection | Provisioning workflow must populate all node hostnames from the database spec |
| No new failure modes — libpq handles retries internally | Each service reconnects independently (no shared routing knowledge) |

**C) Validate at provisioning time only.** If `allow_writes: true` (MCP) or service type is `postgrest`, reject unless the preferred instance is currently the primary.

| Pro | Con |
|-----|-----|
| No code change to PGHOST handling | Fragile — breaks on every failover |
| Simple to implement | Requires re-provisioning after every switchover |
| | Does not actually solve the problem, only defers it |

**Recommendation:** Option B (multi-host PGHOST) is the right default — it's the simplest path with no new failure modes or infrastructure. If pgBouncer is on the roadmap for connection pooling reasons, Option A becomes attractive because routing comes as a side effect of the pooler. Option C is insufficient on its own.

**Required changes for Option B:**
- `DatabaseHost string` → `DatabaseHosts []string` in `ServiceContainerSpecOptions`
- `ServiceInstanceSpec.DatabaseHost string` → `DatabaseHosts []string` in the domain model
- Provisioning workflow populates all Postgres node hostnames from the database spec
- `buildServiceEnvVars` joins them: `PGHOST=host1,host2,host3`

### Gap 3: Schema Setup Automation

**Problem:** PostgREST requires three SQL prerequisites before it can start:
1. `CREATE ROLE web_anon NOLOGIN`
2. `CREATE SCHEMA api` + views/functions
3. `GRANT USAGE/SELECT/EXECUTE ON SCHEMA api TO web_anon`

These are currently manual steps. If forgotten, PostgREST starts but returns 404 for all endpoints (no schema to introspect).

**Options:**
1. **Validation at deploy time:** Before creating the service, run a preflight check that connects to Postgres and verifies the `api` schema and `web_anon` role exist. Fail with a clear error if missing.
2. **Init workflow:** Add a database initialization workflow (or hook) that runs the prerequisite SQL when PostgREST is first added to a database spec.
3. **Document and defer.** The DBA is responsible for schema setup. PostgREST's `/ready` endpoint will return 503 until the schema exists, which is self-diagnosing.

### Gap 4: GRANT web_anon TO Authenticator

**Problem:** After the Control Plane creates `svc_postgrest_{host_id}`, someone must run:
```sql
GRANT web_anon TO svc_postgrest_host1;
```

The implementation guide lists this as step 4 of prerequisites and says "The Control Plane handles step 4 credentials automatically" — but `ServiceUserRole.Create()` does not grant the anonymous role to the service user. On current `main` it creates the user with `pgedge_application_read_only`; post-PR-280 it creates with `LOGIN` + fine-grained public-schema grants. Neither version grants the anonymous role.

**Required fix:** After creating the role and setting NOINHERIT, also:
```sql
GRANT web_anon TO svc_postgrest_{host_id};
```

The anonymous role name comes from `config["db_anon_role"]`. This means `ServiceUserRole` needs access to the service config, or the grant must happen in a separate step.

### Gap 5: Postgres Version Constraint Validation

**Problem:** The `postgrest:v14.5` image registers a Postgres >= 15 constraint. The validation runs in `GenerateServiceInstanceResources()` via `serviceImage.ValidateCompatibility()`. If the database is running Postgres 14, the error surfaces during resource generation — which happens inside a workflow, not at API validation time. The user gets an async failure instead of an immediate 400 response.

**Option:** Add an early compatibility check in `validateServiceSpec()` or the API handler, before the workflow starts. This requires access to the database's Postgres version at validation time.

### Gap 6: No Config Update Without Redeploy

**Problem:** Changing `db_pool` or `max_rows` requires a full spec update through the Control Plane, which triggers resource reconciliation and container redeployment. PostgREST v12+ supports `NOTIFY pgrst, 'reload config'` for some settings, but the Control Plane doesn't use NOTIFY — it manages everything through container environment variables.

**This is by design.** The Control Plane's model is declarative: the spec is the source of truth, and the container is replaced when the spec changes. Runtime config patching via NOTIFY would create a second control path outside the spec, leading to drift. Accept the redeploy cost.

### Summary of Open Items

| Gap | Severity | Recommendation |
|-----|----------|----------------|
| NOINHERIT per service type | Medium | Add conditional in `ServiceUserRole.Create()` now; abstract later if a third service needs it |
| No primary-awareness | High | Multi-host PGHOST (Option B) is the default recommendation. Consider Patroni routing (Option A) if pgBouncer is planned. Blocks MCP `allow_writes` and PostgREST failover. |
| Schema setup automation | Low | Document prerequisites. Add preflight validation if time permits. |
| GRANT anon role to authenticator | High | Must be implemented in `ServiceUserRole.Create()` for PostgREST. |
| Early version constraint check | Low | Nice-to-have. Current async error is acceptable for v1. |
| Config update without redeploy | None | By design. No action needed. |
