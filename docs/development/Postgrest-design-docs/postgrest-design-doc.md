# PostgREST Service: Design Document

**Status:** Proposed | **Depends on:** PR #280 (MCP YAML config) | **Ticket:** PLAT-458

## What

Add PostgREST as the second supported service type in the Control Plane. PostgREST
turns a Postgres schema into a REST API automatically — no application code. It reads
the database schema at startup and generates HTTP endpoints for every table, view, and
function in the configured schema.

## Why

Customers need a way to query structured data over HTTP without writing a custom API.
PostgREST fills this gap using the same deployment model we already have for MCP:
declared in `DatabaseSpec.Services`, provisioned as a Docker Swarm service on the
overlay network, with CP-managed credentials and health monitoring.

## How It Differs from MCP

| | MCP (post-PR-280) | PostgREST |
|---|---|---|
| Config delivery | YAML files bind-mounted at `/app/data/` | Env vars (`PGRST_*` + libpq `PG*`) |
| Image | Custom (`postgres-mcp`) | Upstream (`postgrest/postgrest`) — static binary, no shell utils |
| DB role | `LOGIN`, public-schema read + `pg_read_all_settings` | `LOGIN NOINHERIT` + `GRANT web_anon TO svc_*` |
| Docker health check | `curl -f http://localhost:8080/health` | Disabled (`NONE`) — image has no curl/wget |
| Admin port | N/A | `PGRST_ADMIN_SERVER_PORT=3001` for `/health`, `/ready`, `/live` |
| Extra resources | DirResource → MCPConfigResource | None — uses the 4 base resources only |
| External access | LLM provider APIs (outbound internet) | None |

## Config

```json
{
  "service_type": "postgrest",
  "config": {
    "db_schemas": "api",
    "db_anon_role": "web_anon",
    "db_pool": 10,
    "max_rows": 1000
  }
}
```

| Field | Required | Type | Validation |
|-------|----------|------|------------|
| `db_schemas` | yes | string | Non-empty |
| `db_anon_role` | yes | string | Non-empty |
| `db_pool` | no | int | 1–30, default 10 |
| `max_rows` | no | int | 1–10000, default 1000 |

The CP translates these into env vars and adds hardcoded defaults (`PGRST_DB_URI=postgresql://`, `PGSSLMODE=prefer`, `PGRST_SERVER_PORT=8080`, `PGRST_LOG_LEVEL=warn`, `PGRST_DB_CHANNEL_ENABLED=true`, `PGRST_DB_POOL_ACQUISITION_TIMEOUT=10`, `PGTARGETSESSIONATTRS=read-write`).

## Code Changes

11 files, ~550 lines. No new resource types. No new packages.

| File | Change |
|------|--------|
| `api/apiv1/design/database.go` | Add `"postgrest"` to `service_type` enum. Run `make -C api generate`. |
| `api/apiv1/validate.go` | Add to allowlist. Add `validatePostgRESTServiceConfig()` (required field + range checks). Wire dispatch via `switch`. |
| `swarm/service_images.go` | Register `postgrest/postgrest:v14.5` (with PG >= 15 constraint) and `latest` in `NewServiceVersions()`. Note: `serviceImageTag()` will prepend the configured registry host — images must be mirrored to the private registry, or bypass the helper and use Docker Hub refs directly. |
| `swarm/service_spec.go` | Add `buildPostgRESTEnvVars()`. Branch in `ServiceContainerSpec()`: PostgREST gets env vars + no mounts; MCP keeps YAML + bind mount. |
| `swarm/service_user_role.go` | Add `ServiceType` and `AnonRole` fields. In `Create()`, PostgREST branch runs `ALTER ROLE ... NOINHERIT` then `GRANT web_anon TO svc_*`. |
| `swarm/orchestrator.go` | Add PostgREST branch in `GenerateServiceInstanceResources()` — skips DirResource/MCPConfigResource, uses 4 base resources. |
| `swarm/service_instance_spec.go` | Make `DataDirID` and MCPConfigResource dependency conditional (empty for PostgREST). |
| `validate_test.go` | PostgREST config validation tests. |
| `service_spec_test.go` | PostgREST container spec + env var assertions. |
| `service_images_test.go` | PostgREST image resolution tests. |
| `e2e/service_provisioning_test.go` | Provision, bad version, add/remove, stability, config update tests. |

**Files that do NOT change:** `spec.go`, `service_instance.go`, `plan_update.go`, `operations/`, `resources.go`, `network.go`, `mcp_config*.go`.

## Postgres Prerequisites (Manual)

Before deploying PostgREST, the DBA must run:

```sql
CREATE ROLE web_anon NOLOGIN;
CREATE SCHEMA api;
GRANT USAGE ON SCHEMA api TO web_anon;
GRANT SELECT ON ALL TABLES IN SCHEMA api TO web_anon;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA api TO web_anon;
```

The CP handles: service user creation, `NOINHERIT`, `GRANT web_anon TO svc_*`, credential injection.

## Open Items

| Item | Severity | Decision |
|------|----------|----------|
| **No primary-awareness** — `PGHOST` is a single hostname baked at provision time. Breaks failover for PostgREST and MCP `allow_writes`. | High | Solve separately (multi-host PGHOST or Patroni routing). Not a blocker for initial PostgREST deploy — read-only workloads work on any node. |
| **Schema setup validation** — if `api` schema or `web_anon` role doesn't exist, PostgREST starts but returns 404 for everything. | Low | Document prerequisites. Consider preflight SQL check in a follow-up. |
| **PostgREST version** — v14.5 is the latest stable release (Feb 2026). v14 dropped support for PostgreSQL 12 (EOL). The implementation guide's Postgres >= 15 constraint is stricter than what PostgREST requires — decide whether to enforce >= 13 (PostgREST minimum) or >= 15 (pgEdge preference). | Low | Register v14.5 with the agreed Postgres constraint. Add newer versions as they ship. |

## Verification

```bash
docker service ls --filter name=postgrest
curl http://localhost:3100/health                                           # 200
curl http://localhost:3100/ -H "Accept-Profile: api"                        # OpenAPI spec
curl "http://localhost:3100/gold_summaries?limit=1" -H "Accept-Profile: api"  # JSON rows
curl -X DELETE "http://localhost:3100/gold_summaries?id=eq.1" -H "Content-Profile: api"  # 405 (blocked)
```
