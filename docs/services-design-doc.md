# pgEdge Control Plane — Service Types Design Doc

**Status:** Draft for review
**Scope:** RAG and PostgREST service types
**Related PRs:** [#278 — developer guide for adding services](https://github.com/pgEdge/control-plane/pull/278) · [#285 — PostgREST design](https://github.com/pgEdge/control-plane/pull/285)

---

## What is a service and why does it live next to the database?

A **service** is a containerized workload that runs co-located with a pgEdge database and depends on direct, low-latency access to Postgres. Services are not standalone products — they exist to extend the database with an API layer that would otherwise require manual wiring by the application team.

Each service is deployed per-host, one instance per `(service_id, host_id)` pair, constrained to the same Swarm node as its Postgres instance. This placement keeps the network path between the service and Postgres on-node, avoids cross-node overhead, and ensures the service uses the right node's Patroni primary.

Services depend only on the database. There are no inter-service dependencies and no provisioning ordering between services of different types.

**Currently supported types:**

| Type | Purpose |
|------|---------|
| `mcp` | Exposes Postgres as an MCP tool server for LLM agents |
| `rag` | Hybrid vector + keyword search with LLM answer synthesis |
| `postgrest` | Auto-generates a REST API from Postgres schema |

---

## What does the config look like?

All services share the top-level service spec shape:

```json
{
  "service_id": "<unique within this database>",
  "service_type": "rag | postgrest | mcp",
  "version": "latest | <semver>",
  "host_ids": ["host-1"],
  "port": 9200,
  "config": { ... }
}
```

`port` is optional. Omit it to not publish any port. `0` lets Docker assign a random port. A specific integer pins it.

### RAG config

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `embedding_provider` | yes | string | `openai` or `ollama` |
| `embedding_model` | yes | string | e.g. `text-embedding-3-small` |
| `llm_provider` | yes | string | `anthropic`, `openai`, or `ollama` |
| `llm_model` | yes | string | e.g. `claude-sonnet-4-5` |
| `tables` | yes | array | See below |
| `openai_api_key` | conditional | string | Required when provider is `openai` |
| `anthropic_api_key` | conditional | string | Required when provider is `anthropic` |
| `voyage_api_key` | no | string | Optional reranking |
| `ollama_url` | conditional | string | Required when provider is `ollama` |
| `pipeline_name` | no | string | Defaults to `default` |
| `pipeline_description` | no | string | |
| `token_budget` | no | int | Defaults to `4000` |
| `top_n` | no | int | Defaults to `10` |

`tables` entry shape:

```json
{
  "table": "documents_content_chunks",
  "text_column": "content",
  "vector_column": "embedding",
  "id_column": "id"        // optional
}
```

API keys are **not** written to Swarm config files — they are injected as environment variables only. All other config is rendered into a YAML file and delivered via Docker Swarm config.

### PostgREST config

| Field | Required | Type | Validation |
|-------|----------|------|-----------|
| `db_schemas` | yes | string | non-empty |
| `db_anon_role` | yes | string | non-empty |
| `db_pool` | no | int | 1–30, default `10` |
| `max_rows` | no | int | 1–10000, default `1000` |

All config is delivered as `PGRST_*` environment variables. No config file is created.

---

## How does the user utilize it after it's running?

### RAG

The RAG server exposes a read-only HTTP API. Documents must be pre-loaded into Postgres with pre-computed embeddings — there is no ingestion endpoint.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/health` | GET | Health check |
| `/v1/pipelines` | GET | List configured pipelines |
| `/v1/pipelines/{name}` | POST | Submit a query, receive an LLM answer |

Query shape:
```json
{ "query": "What is pgEdge?" }
```

Response includes the LLM answer and the source chunks used.

**To populate data**, insert rows directly into the configured table with pre-computed vector embeddings matching the configured `embedding_model` and dimensions (e.g. `text-embedding-3-small` → 1536 dims).

### PostgREST

PostgREST auto-generates a REST API from the database schema. No additional setup is required after provisioning — the API reflects the schema at query time.

| Pattern | Method | Description |
|---------|--------|-------------|
| `/<table>` | GET | List rows (filtering, ordering, pagination via query params) |
| `/<table>` | POST | Insert row |
| `/<table>?<filter>` | PATCH | Update matching rows |
| `/<table>?<filter>` | DELETE | Delete matching rows |
| `/rpc/<function>` | POST | Call a Postgres function |

Schema and permission changes to the anonymous role take effect immediately with no service restart.

---

## How does it run?

|  | RAG | PostgREST | MCP |
|--|-----|-----------|-----|
| **Image** | `pgedge/pgedge-rag-server` | `postgrest/postgrest:v14.5` | `pgedge/postgres-mcp` |
| **Registry** | pgEdge private | Docker Hub (upstream) | pgEdge private |
| **API port** | 8080 | 8080 | 8080 |
| **Admin port** | — | 3001 (`/ready`, `/live`) | — |
| **Health check** | `/dev/tcp` bash built-in (no curl in image) | HTTP poll on admin port | `curl /health` |
| **Health start period** | 30s | 30s | 30s |
| **Config delivery** | Swarm config file at `/etc/pgedge/pgedge-rag-server.yaml` | Environment variables | Bind-mount at `/app/data/` |
| **Default CPU limit** | from `ServiceSpec.cpus` | same | same |
| **Default memory limit** | from `ServiceSpec.memory_bytes` | same | same |
| **Extra resources** | `ServiceConfigResource` (Swarm config) | none | `DirResource` + `MCPConfigResource` |

Resource lifecycle (all service types follow this 5-phase model):

```
Phase 1:  NetworkResource          (no deps)
          ServiceUserRole          (no deps)
Phase 2:  ServiceInstanceSpec      (depends on Network + ServiceUserRole)
Phase 3:  ServiceInstance          (depends on ServiceUserRole + ServiceInstanceSpec)
Phase 4:  ServiceInstanceMonitor   (depends on ServiceInstance)
```

RAG adds `ServiceConfigResource` (Swarm config) as an additional Phase 1 resource, injected into `ServiceInstanceSpec` as `SwarmConfigID`.

---

## What does it need from Postgres?

### RAG

| Requirement | Detail |
|-------------|--------|
| **Access level** | Read-only against the configured tables |
| **Service user** | `svc_{serviceID}_{hostID}`, `LOGIN` |
| **Extensions** | `pgvector` (installed by `RAGSchemaResource` at provisioning time) |
| **Schema** | `documents_content_chunks` table (or user-defined) must exist with a `vector(N)` column matching the embedding model dimensions |
| **Schema ownership** | Control plane creates the table via `RAGSchemaResource` using the Postgres `admin` user |
| **Node targeting** | User is created and schema is set up on the co-located Patroni primary for each host |

The RAG server reads its DB credentials from environment variables (`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`). `PGSSLMODE=prefer`.

### PostgREST

| Requirement | Detail |
|-------------|--------|
| **Access level** | Depends on `db_anon_role` grants — PostgREST uses `SET ROLE` per request |
| **Service user** | `svc_{serviceID}_{hostID}`, `LOGIN NOINHERIT` |
| **Extensions** | None required |
| **Schema / grants** | DBA must create the `db_anon_role` role and grant it schema access — **not automated** |
| **NOINHERIT required** | Without it, the service user would inherit `db_anon_role` privileges permanently, defeating per-request role switching |
| **`PGTARGETSESSIONATTRS=read-write`** | Ensures PostgREST connects to the Patroni primary, not a replica |

**Manual DBA setup required before PostgREST will serve data:**

```sql
CREATE ROLE web_anon NOLOGIN;
CREATE SCHEMA api;
GRANT USAGE ON SCHEMA api TO web_anon;
GRANT SELECT ON ALL TABLES IN SCHEMA api TO web_anon;
```

This is not automated today (see open questions).

---

## What are its networking needs?

All services attach to two Docker networks:

1. **`bridge`** — provides control-plane access to the service health endpoint and routes published ports to end-users
2. **Database overlay network** (`{databaseID}`) — provides isolated connectivity to Postgres; one overlay per database

Services are placed on a specific Swarm node via: `node.id == {cohortMemberID}`.

**Port publication** is controlled by the `port` field:
- `null` / omitted → no external port (control-plane only access)
- `0` → Docker assigns a random ingress port
- specific → pins that ingress port

**Docker Desktop caveat (dev only):** Swarm ingress ports are not forwarded to Mac localhost by Docker Desktop. A `docker run -p <port>:<port> alpine/socat` proxy container is required to bridge from Mac to the Swarm ingress IP.

**Service discovery:** services are not discoverable by other services. There is no DNS or sidecar registration. Clients reach services via the published port or through the control-plane API response (`service_instances[].status.ports`).

---

## What doesn't fit?

### RAG

| Gap | Detail |
|-----|--------|
| **No ingestion API** | Documents must be inserted directly into Postgres with pre-computed embeddings. This is by design in the current RAG server, but creates friction for end-users. A future ingestion endpoint or a control-plane-assisted ingestion workflow would improve UX. |
| **Embedding dimensions are implicit** | The vector column dimensions must match the configured embedding model. There is no validation at provisioning time. Mismatches produce silent bad results. |
| **Schema bootstrap fragility** | `RAGSchemaResource` creates the table on the co-located primary. If the table already exists with a different schema, provisioning silently succeeds but the RAG server may fail at runtime. |
| **Single-node dev port access** | With all hosts sharing one Swarm node, ingress ports require a socat proxy to reach Mac localhost. |

### PostgREST

| Gap | Detail |
|-----|--------|
| **No automated `db_anon_role` setup** | The DBA must manually create the anonymous role and grant schema access. Unlike RAG, PostgREST has no equivalent of `RAGSchemaResource`. |
| **Health check method** | PostgREST image lacks curl; health check must poll the admin port directly. The current health check pattern in the codebase (`curl /health`) needs a PostgREST-specific override similar to the RAG `/dev/tcp` approach. |
| **Postgres version constraint** | PostgREST requires Postgres ≥ 15 (pgEdge decision). This is enforced at image registration but not surfaced clearly in API errors. |

### Framework-level open questions

| Question | Detail |
|----------|--------|
| **Inter-service dependencies** | Today services depend only on the database. If a service needs another service (e.g. a future ingestion pipeline that calls the RAG server), the resource model needs extension. |
| **Config updates** | Changing service config today requires delete + recreate. There is no `UpdateService` path that diffs config and redeploys in-place. |
| **Secrets management** | API keys live in the service config map and flow into Swarm config files or env vars. They are stripped from API responses but are stored unencrypted in the control-plane database. A secrets backend integration is not scoped. |
| **Multi-pipeline RAG** | The current config supports multiple `tables` but a single pipeline name. Running multiple RAG pipelines with different embedding models on the same database requires multiple service instances. |
