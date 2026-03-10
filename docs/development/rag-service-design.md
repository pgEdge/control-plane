# RAG Service Design Specification

**Status:** Implemented — spike complete, production-ready for single-node
**Authors:** Siva, Moiz
**Related:** PLAT-445, [Supported Services Developer Guide](./supported-services.md), [Integration Guide](./rag-service-integration.md)

---

## Component overview

### System-level: where the RAG service fits

The RAG service is deployed as a Docker Swarm service managed entirely by the control plane. It sits between the client and the database — the client sends natural language queries to the RAG service, and the RAG service translates those queries into vector SQL, retrieves relevant document chunks, and passes them to an LLM to generate an answer.

The control plane owns everything before the container starts: network, Postgres user, API key files on the host filesystem, Swarm config YAML, pgvector schema, and container deployment. Once running, the control plane only monitors health — it does not participate in query traffic.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  pgEdge Control Plane                                           │
  │                                                                 │
  │  ┌──────────────┐    ┌─────────────────────────────────────┐   │
  │  │  REST API    │    │  Workflow Engine                     │   │
  │  │  /v1/        │───▶│  PlanUpdate → UpdateDatabase        │   │
  │  │  databases   │    │                                     │   │
  │  └──────────────┘    │  Resource lifecycle per host:       │   │
  │                      │  1. NetworkResource                 │   │
  │                      │  2. ServiceUserRole ──────────────┐ │   │
  │                      │  3. RAGAPIKeysResource ───────────┤ │   │
  │                      │  4. ServiceConfigResource ────────┤ │   │
  │                      │  5. RAGSchemaResource ────────────┤ │   │
  │                      │  6. ServiceInstanceSpecResource ◀─┘ │   │
  │                      │  7. ServiceInstanceResource         │   │
  │                      │  8. ServiceInstanceMonitor          │   │
  │                      └─────────────────────────────────────┘   │
  └──────────────────────────────┬──────────────────────────────────┘
                                 │ Docker Swarm API
                    ┌────────────▼──────────────────────────┐
                    │  Docker Swarm node                     │
                    │                                        │
                    │  ┌──────────────────────────────────┐ │
                    │  │  rag-server container            │ │
                    │  │  image: ghcr.io/pgedge/rag-server│ │
                    │  │  port: 8080 (ingress → 9200)     │ │
                    │  │                                  │ │
                    │  │  Mounts:                         │ │
                    │  │   Swarm config → /etc/pgedge/    │ │
                    │  │   pgedge-rag-server.yaml         │ │
                    │  │   host keys dir → /etc/pgedge/   │ │
                    │  │   keys/ (read-only bind mount)   │ │
                    │  └──────────────┬───────────────────┘ │
                    │                 │ overlay network      │
                    │  ┌──────────────▼───────────────────┐ │
                    │  │  postgres container (Patroni)    │ │
                    │  │  user: svc_rag_host_1 (read-only)│ │
                    │  └──────────────────────────────────┘ │
                    └────────────────────────────────────────┘
                                 │ port 9200
                    ┌────────────▼──────────────────────────┐
                    │  Client (application / Postman)        │
                    │  POST /v1/pipelines/default            │
                    └────────────────────────────────────────┘
```

### Provisioning flow

Resources 2–5 (ServiceUserRole, RAGAPIKeysResource, ServiceConfigResource, RAGSchemaResource) execute in dependency order in Phase 1. `RAGAPIKeysResource` depends on `ServiceUserRole` (needs credentials established first), then `ServiceConfigResource` depends on `RAGAPIKeysResource` (needs the host keys directory path to embed in the YAML `api_keys` section). `ServiceInstanceSpecResource` in Phase 2 gates on all four Phase 1 resources. Only then does Phase 3 deploy the container.

```
User: POST /v1/databases  (services[].service_type = "rag")
        │
        ▼
  API Validation (validate.go)
  ├─ service_type in allowlist?        ✓ "rag"
  ├─ pipelines array non-empty?        ✓
  ├─ embedding_provider valid?         ✓ "openai"
  ├─ tables array non-empty?           ✓
  ├─ openai_api_key present?           ✓ (required for openai provider)
  └─ anthropic_api_key present?        ✓ (required for anthropic llm)
        │
        ▼
  PlanUpdate workflow
  ├─ findPostgresInstance(hostID)      → postgres-<instanceID>:5432
  ├─ GenerateServiceInstanceID()       → "storefront-rag-host-1"
  └─ GenerateServiceUsername()         → "svc_rag_host_1"
        │
        ▼
  GenerateServiceInstanceResources()
  ├─ NetworkResource          → database overlay network
  ├─ ServiceUserRole          → CREATE ROLE svc_rag_host_1 LOGIN
  │                             GRANT pgedge_application_read_only
  │                             (on co-located Patroni primary)
  ├─ RAGAPIKeysResource       → write per-provider key files to
  │                             /var/lib/pgedge/services/<id>/keys/
  │                             on the host filesystem (bind-mounted
  │                             read-only into container)
  ├─ ServiceConfigResource    → render YAML via generateRAGConfig()
  │                             (includes api_keys section with host paths)
  │                             → docker config create rag-config-<id>
  │                             → stores SwarmConfigID
  ├─ RAGSchemaResource        → CREATE EXTENSION IF NOT EXISTS vector
  │                             → GRANT SELECT ON configured tables
  │                             (tables must already exist)
  ├─ ServiceInstanceSpecResource
  │   ├─ reads SwarmConfigID from ServiceConfigResource
  │   ├─ reads credentials from ServiceUserRole
  │   ├─ reads KeysDirPath from RAGAPIKeysResource (for bind mount)
  │   └─ calls ServiceContainerSpec() → swarm.ServiceSpec
  └─ ServiceInstanceResource  → docker service create
                                → WaitForService (5 min timeout)
                                → health check passes → state: running
```

### Query flow: inside the RAG server

Every query goes through a fixed 5-step pipeline. The two key design choices are **hybrid search** and **Reciprocal Rank Fusion (RRF)**.

Hybrid search runs vector similarity (semantic meaning) and BM25 keyword scoring independently against the same Postgres table. Neither alone is sufficient: vector search misses exact keyword matches; BM25 misses paraphrased queries. RRF (k=60) merges both result sets by assigning `1/(60 + rank)` to each document per set and summing. Documents appearing in both sets get a double contribution — a natural boost for documents that are both semantically and keyword-relevant. The token budget (default 4000) limits context sent to the LLM; the last document is truncated at a sentence boundary.

```
Client: POST /v1/pipelines/default  { "query": "What is pgEdge?" }
  │
  ▼
┌──────────────────────────────────────────────────────────────────┐
│  HTTP Server (net/http, port 8080)                               │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│  Pipeline Orchestrator                                           │
│                                                                  │
│  Step 1 ─ Embed query                                           │
│  ┌─────────────────────┐                                        │
│  │  EmbeddingProvider  │──▶  OpenAI /v1/embeddings             │
│  │  (OpenAI / Voyage / │◀──  [0.021, -0.003, …]  1536 dims    │
│  │   Ollama)           │                                        │
│  └─────────────────────┘                                        │
│           │ query vector                                         │
│           ▼                                                      │
│  Step 2 ─ Hybrid search                                         │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  PostgreSQL (pgvector)                                  │   │
│  │  A) Vector: SELECT content,                             │   │
│  │       1 - (embedding <=> $1::vector) AS score           │   │
│  │     ORDER BY embedding <=> $1::vector LIMIT 20          │   │
│  │  B) BM25: SELECT id, content  →  in-memory BM25 index  │   │
│  └───────────────────┬─────────────────┬───────────────────┘   │
│        vector results│                 │BM25 results             │
│                      ▼                 ▼                         │
│  Step 3 ─ RRF merge: score(d) = Σ 1/(60+rank)                  │
│           → deduplicate → top-N                                  │
│                      │                                           │
│                      ▼                                           │
│  Step 4 ─ Token budget packing (default 4000 tokens)            │
│           truncate last doc at sentence boundary                 │
│                      │                                           │
│                      ▼                                           │
│  Step 5 ─ LLM completion                                        │
│  ┌─────────────────────┐                                        │
│  │  CompletionProvider │──▶  Anthropic /v1/messages            │
│  │  (Anthropic / OpenAI│◀──  "pgEdge is a distributed…"        │
│  │   / Ollama)         │                                        │
│  └─────────────────────┘                                        │
└───────────────────────────────┬──────────────────────────────────┘
                                ▼
            { "answer": "…", "sources": […], "tokens_used": 1234 }
```

### Internal RAG server components

The RAG server is a single Go binary. On startup it reads `pgedge-rag-server.yaml` (mounted from the Swarm config), initialises one `Pipeline` per configured pipeline entry, and starts the HTTP server. Each pipeline holds its own database connection pool, embedding provider client, completion provider client, and a stateless BM25 index (rebuilt per request). The `PipelineManager` is the only shared state, protected by a read/write mutex. There is no caching layer — every query hits Postgres fresh.

**Multiple pipelines per service instance.** The control plane generates one pipeline per entry in the `config.pipelines` array. Each pipeline can have its own embedding provider, LLM, model, `base_url`, and table set — all sharing the same database connection (same host/user/password). Two separate databases require two separate service instances.

```
┌────────────────────────────────────────────────────────────┐
│  pgedge-rag-server                                         │
│                                                            │
│  ┌──────────────┐   reads   ┌───────────────────────────┐ │
│  │  config      │◀──────────│  pgedge-rag-server.yaml   │ │
│  │  (config.go) │           │  (Swarm config mount)     │ │
│  └──────┬───────┘           └───────────────────────────┘ │
│         │ initialises                                       │
│         ▼                                                   │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  PipelineManager (RWMutex)                           │  │
│  │  ┌─────────────────────────────────────────────────┐ │  │
│  │  │  Pipeline "default"                             │ │  │
│  │  │  ┌──────────────┐  ┌──────────────────────────┐ │ │  │
│  │  │  │ EmbeddingProv│  │ CompletionProvider       │ │ │  │
│  │  │  │ (OpenAI /    │  │ (Anthropic / OpenAI /   │ │ │  │
│  │  │  │  Voyage /    │  │  Ollama)                 │ │ │  │
│  │  │  │  Ollama)     │  └──────────────────────────┘ │ │  │
│  │  │  └──────────────┘                               │ │  │
│  │  │  ┌──────────────────────────────────────────┐   │ │  │
│  │  │  │ Database Pool (pgxpool)                  │   │ │  │
│  │  │  │ host: postgres-<instanceID>:5432         │   │ │  │
│  │  │  │ user: svc_rag_host_1 (read-only)         │   │ │  │
│  │  │  └──────────────────────────────────────────┘   │ │  │
│  │  └─────────────────────────────────────────────────┘ │  │
│  │  ┌─────────────────────────────────────────────────┐ │  │
│  │  │  Pipeline "ollama"                              │ │  │
│  │  │  ┌──────────────┐  ┌──────────────────────────┐ │ │  │
│  │  │  │ EmbeddingProv│  │ CompletionProvider       │ │ │  │
│  │  │  │ (Ollama)     │  │ (Ollama)                 │ │ │  │
│  │  │  └──────────────┘  └──────────────────────────┘ │ │  │
│  │  └─────────────────────────────────────────────────┘ │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  HTTP Server (net/http, port 8080)                   │  │
│  │  GET  /v1/health                                     │  │
│  │  GET  /v1/pipelines                                  │  │
│  │  POST /v1/pipelines/{name}  (+ SSE streaming)        │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────┘
        │ TCP:5432 (overlay network)        │ HTTPS (external)
        ▼                                  ▼
  PostgreSQL (pgvector)            OpenAI / Anthropic / Voyage
```

---

## 1. What is this service and why does it live next to the database?

The pgEdge RAG Server is an HTTP query service that provides semantic search and AI-generated answers over content stored in a Postgres database. Given a natural-language question, it:

1. Generates a vector embedding of the query using a configured embedding model
2. Queries the target Postgres table(s) using pgvector cosine similarity to find semantically relevant content
3. Combines vector results with BM25 full-text keyword ranking via Reciprocal Rank Fusion (hybrid search)
4. Passes the retrieved context and query to an LLM to generate a grounded answer

It lives next to the database because its entire data layer is the database — it reads directly from user-managed Postgres tables that contain text and pre-computed vector embeddings. Co-locating on the same Swarm node and database overlay network eliminates cross-node hops on the query path and keeps the Postgres connection local.

**Important distinction from MCP:** The RAG server is query-only. It does not ingest documents or write embeddings. Documents must be inserted with pre-computed embeddings by the caller. The `pgedge_vectorizer` extension (shipped with the pgEdge Postgres image) can automate background embedding generation — but that is a database-side concern, not a RAG server function.

---

## 2. What does its config look like?

### How the RAG server is configured

The RAG server reads a YAML file at `/etc/pgedge/pgedge-rag-server.yaml`. The control plane renders this YAML from the `ServiceSpec.Config` map, stores it as a Docker Swarm config object, and mounts it into the container.

**API keys are stored as bind-mounted host files, not in the Swarm config.** Docker Swarm config objects are stored in the Docker daemon Raft log and are readable via `docker config inspect` — making them unsuitable for secrets. Instead, `RAGAPIKeysResource` writes one file per provider to `/var/lib/pgedge/services/<instanceID>/keys/` on the host filesystem. The directory is bind-mounted read-only into the container at `/etc/pgedge/keys/`. The generated YAML references each key by its container-internal path under `api_keys`.

### `ServiceSpec.Config` fields

The top-level `config` object must contain a `pipelines` array. Each pipeline is an independent query path with its own embedding model, LLM, and table set.

#### Pipeline fields (per entry in `pipelines`)

##### Required

| Field | Type | Values |
|-------|------|--------|
| `embedding_provider` | string | `openai`, `voyage`, `ollama` |
| `embedding_model` | string | e.g. `text-embedding-3-small`, `nomic-embed-text` |
| `llm_provider` | string | `anthropic`, `openai`, `ollama` |
| `llm_model` | string | e.g. `claude-sonnet-4-5`, `gpt-4o-mini` |
| `tables` | array | At least one entry — see below |

Each entry in `tables`:

| Field | Required | Notes |
|-------|----------|-------|
| `table` | yes | Postgres table name (optionally schema-qualified) |
| `text_column` | yes | Column holding the raw text chunk |
| `vector_column` | yes | Column holding the `vector(N)` embedding |
| `id_column` | no | Defaults to `id` |

##### Conditionally required (top-level, by provider)

API keys are set once at the top level of `config` and apply to all pipelines. They are written to the host filesystem by `RAGAPIKeysResource` and referenced by path in the generated YAML — they are never embedded in the Swarm config object.

| Field | Required when |
|-------|--------------|
| `openai_api_key` | any pipeline uses `embedding_provider=openai` OR `llm_provider=openai` |
| `anthropic_api_key` | any pipeline uses `llm_provider=anthropic` |
| `voyage_api_key` | any pipeline uses `embedding_provider=voyage` |
| `ollama_url` | per-pipeline — required when `embedding_provider=ollama` OR `llm_provider=ollama` |

API keys are validated as non-empty strings. They are **stripped from all API responses** — never returned after submission.

##### Optional

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `pipeline_name` | string | `"default"` | Pipeline name used in `POST /v1/pipelines/{name}` |
| `pipeline_description` | string | `""` | Human-readable description |
| `embedding_base_url` | string | — | Custom base URL for embedding provider (e.g. self-hosted) |
| `llm_base_url` | string | — | Custom base URL for LLM provider |
| `ollama_url` | string | — | Required for Ollama provider |

#### Top-level optional fields

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `token_budget` | int | `4000` | Max context tokens passed to LLM (all pipelines) |
| `top_n` | int | `10` | Documents retrieved per query (all pipelines) |

#### Validation rules

- `pipelines` array must be present and non-empty
- `embedding_provider` and `llm_provider` must be known values; unknown values rejected at API layer (HTTP 400)
- `tables` must have at least one entry; each must have all three required column fields as non-empty strings
- API keys validated as present and non-empty when their provider is selected
- Unknown config keys are ignored (no strict allowlist today)

#### Example spec

```json
{
  "service_id": "rag",
  "service_type": "rag",
  "version": "latest",
  "host_ids": ["host-1"],
  "port": 0,
  "config": {
    "openai_api_key": "sk-...",
    "anthropic_api_key": "sk-ant-...",
    "pipelines": [
      {
        "pipeline_name": "default",
        "embedding_provider": "openai",
        "embedding_model": "text-embedding-3-small",
        "llm_provider": "anthropic",
        "llm_model": "claude-sonnet-4-5",
        "tables": [
          {
            "table": "documents_content_chunks",
            "text_column": "content",
            "vector_column": "embedding"
          }
        ]
      },
      {
        "pipeline_name": "ollama",
        "embedding_provider": "ollama",
        "embedding_model": "nomic-embed-text",
        "llm_provider": "ollama",
        "llm_model": "llama3",
        "ollama_url": "http://ollama:11434",
        "tables": [
          {
            "table": "documents_content_chunks",
            "text_column": "content",
            "vector_column": "embedding"
          }
        ]
      }
    ]
  }
}
```

#### Generated YAML (rendered by `generateRAGConfig()`)

```yaml
server:
  listen_address: "0.0.0.0"
  port: 8080

defaults:
  token_budget: 4000
  top_n: 10

api_keys:
  openai: "/etc/pgedge/keys/openai"
  anthropic: "/etc/pgedge/keys/anthropic"

pipelines:
  - name: "default"
    database:
      host: "postgres-{instanceID}"
      port: 5432
      database: "{dbName}"
      username: "{serviceUser}"
      password: "{servicePassword}"
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
  - name: "ollama"
    database:
      host: "postgres-{instanceID}"
      port: 5432
      database: "{dbName}"
      username: "{serviceUser}"
      password: "{servicePassword}"
      ssl_mode: "prefer"
    tables:
      - table: "documents_content_chunks"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "ollama"
      model: "nomic-embed-text"
    rag_llm:
      provider: "ollama"
      model: "llama3"
    ollama_url: "http://ollama:11434"
```

The `api_keys` section is only written when API key files exist for the instance. Each value is the container-internal path to the key file under `/etc/pgedge/keys/`.

---

## 3. How does the user utilize it after it's running?

### Prerequisites

Before deploying the RAG service, the database must already contain tables with pre-computed embeddings. The control plane does **not** create these tables — that is the user's responsibility. The expected flow is:

1. Deploy a Control Plane database
2. Create your tables and populate them with embeddings
3. Deploy the RAG service pointing `pipelines[].tables` at your tables

Options for populating:
- **`pgedge_vectorizer` extension** (recommended): background worker that auto-generates embeddings as rows are inserted. Run `CREATE EXTENSION IF NOT EXISTS pgedge_vectorizer` in your database, then use `pgedge_vectorizer.enable_vectorization()` on your source table.
- **Direct insert**: compute embeddings externally (e.g. via OpenAI API) and insert rows directly into the table with the vector values.

If the table is empty or does not exist, queries will fail. If a configured table does not exist at provisioning time, `RAGSchemaResource` will fail the `GRANT` and block deployment.

### API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v1/health` | Health check — `{"status": "healthy"}` |
| `GET` | `/v1/pipelines` | List configured pipelines |
| `POST` | `/v1/pipelines/{name}` | Submit a query; returns LLM-synthesised answer |
| `GET` | `/v1/openapi.json` | OpenAPI spec |

```bash
curl -X POST http://localhost:9200/v1/pipelines/default \
  -H "Content-Type: application/json" \
  -d '{"query": "How does Spock replication work?"}'
```

```json
{
  "answer": "Spock replication uses logical replication...",
  "sources": [{"id": "42", "content": "...", "score": 0.94}],
  "tokens_used": 312
}
```

Streaming is supported via `"stream": true` (Server-Sent Events).

Optional request fields: `top_n` (override default), `include_sources` (include source chunks in response), `messages` (conversation history for multi-turn).

### Post-initialization changes

All configuration (model, table, provider) requires a service spec update. The control plane re-renders the YAML, creates a new Swarm config object (configs are immutable in Docker), updates the host key files, and triggers a service restart. There is no in-place config hot-reload path today.

---

## 4. How does it run?

### Container image

| Property | Value |
|----------|-------|
| Image | `ghcr.io/pgedge/rag-server:main` |
| Registry | GitHub Container Registry (pgEdge private) |
| Runtime | Red Hat UBI9 Micro — no curl, no wget, no bash shell tools |
| User | `pgedge` (UID 1000, non-root) |
| Config path | `/etc/pgedge/pgedge-rag-server.yaml` |
| Keys path | `/etc/pgedge/keys/` (bind-mounted read-only from host) |
| Port | `8080` |

### Health check

The RAG server image is built on RHEL UBI9 Micro, which ships without `curl` or `wget`. The standard curl health check used by MCP cannot be used. The implementation uses a bash built-in TCP check:

```
CMD-SHELL  exec 3<>/dev/tcp/127.0.0.1/8080
Start period: 30s
Interval:    10s
Timeout:      5s
Retries:      3
```

This confirms the port is accepting connections. It does not validate the HTTP response or Postgres connectivity — a container can pass health while failing queries if the database is unreachable.

### Resource defaults

| Resource | Recommended | Notes |
|----------|-------------|-------|
| CPU | `0.5` | BM25 tokenization is CPU-bound per query |
| Memory | `256MiB` | No local model weights; all inference via external API |

### Differences from MCP

| Aspect | MCP | RAG |
|--------|-----|-----|
| Config delivery | Env vars only | YAML via Docker Swarm config mounted at `/etc/pgedge/pgedge-rag-server.yaml` |
| API keys | Env vars | Bind-mounted host files at `/etc/pgedge/keys/` (never in Swarm config) |
| Health check | `curl -f http://localhost:8080/health` | bash `/dev/tcp` TCP check (no curl in image) |
| Extra resources | `DirResource` + `MCPConfigResource` | `RAGAPIKeysResource` + `ServiceConfigResource` + `RAGSchemaResource` |
| Schema setup | None | Automated: pgvector extension + SELECT grant on user-managed tables |
| Total lifecycle resources | 6 | 8 |
| External API calls | LLM provider on each query | Embedding provider + LLM provider on each query |

---

## 5. What does it need from Postgres?

### Access level

**Read-only.** The RAG server only runs `SELECT` queries. The service user is granted `pgedge_application_read_only`. No write access is needed at any point.

### Service user

| Property | Value |
|----------|-------|
| Username format | `svc_{service_id}_{host_id}` (hyphens → underscores, max 63 chars with hash suffix) |
| Attributes | `LOGIN` |
| Role | `pgedge_application_read_only` |
| Node scope | Created on the Patroni primary **co-located with the service host** — not replicated by Spock |

### Extensions

| Extension | Installed by | When |
|-----------|-------------|------|
| `vector` (pgvector) | Control plane — `RAGSchemaResource.Create()` | Before container starts |
| `pgedge_vectorizer` | User — manual `CREATE EXTENSION` | Before populating tables; needed for background embedding generation |

### Schema

Table creation is the **user's responsibility**. The expected flow is:

1. Deploy a Control Plane database
2. Create your tables and populate them with embeddings (using `pgedge_vectorizer`, a doc loader, or direct inserts)
3. Deploy the RAG service, pointing `pipelines[].tables` at your existing tables

`RAGSchemaResource.Create()` runs on the co-located Patroni primary and does two things:

```sql
-- Enable pgvector (requires superuser — control plane handles this)
CREATE EXTENSION IF NOT EXISTS vector;

-- Grant read access to the service user on each configured table
GRANT SELECT ON <table> TO svc_rag_host_1;
```

The `GRANT` is issued for every table listed across all pipelines. If a table does not exist at provisioning time, the `GRANT` will fail and provisioning will be blocked — the user must create their tables before deploying the RAG service.

`GRANT` **is replicated** by Spock — running on one node propagates to all. `CREATE ROLE` is **not replicated** — the service user must be created per-node on each co-located primary, which `ServiceUserRole` handles.

### Spock / replication

Read-only — no write conflicts. Multiple RAG instances each read from their local Patroni primary, which is correct in a multi-master setup. Spock replicates rows (including embeddings) to all nodes, so each RAG instance searches the full dataset locally.

---

## 6. What are its networking needs?

### Networks

| Network | Purpose |
|---------|---------|
| `bridge` | Control-plane health access; routes published external port to clients |
| `{databaseID}` overlay | Isolated per-database connectivity to Postgres |

### External port

Controlled by `port` in the service spec:

| Value | Behaviour |
|-------|-----------|
| omitted | No external port; service only reachable by control plane |
| `0` | Docker Swarm assigns a random ingress port |
| specific integer | Pinned to that port |

Publish mode is **ingress** (not host). Host mode would block deployment of more than one service per port on a single-node Swarm, which breaks the dev environment where all hosts share one Docker daemon.

### Outbound internet

The RAG server calls external LLM and embedding APIs (Anthropic, OpenAI, Voyage AI) on every query. The Docker host needs outbound HTTPS access to these endpoints. Ollama is the only provider that runs locally and requires no outbound access.

### Service discovery

Published port and container IP are surfaced in the control-plane API under `service_instances[].status.ports`. There is no DNS registration or sidecar. Clients reach the RAG API via the published port directly.

---

## 7. What doesn't fit?

| Gap | Detail |
|-----|--------|
| **No document ingestion API** | The RAG server is read-only. Users must create their own tables and populate them with embeddings before deploying the RAG service. The recommended path is `pgedge_vectorizer` (requires manual `CREATE EXTENSION pgedge_vectorizer`). |
| **No config update path** | Changing any config field (model, table, provider) requires delete + re-provision. An in-place update path would need to create a new Swarm config object, update the service mount, and restart. Resource model supports it but it is not wired up. |
| **Health check does not validate Postgres** | The `/dev/tcp` check confirms the port is open but not that the RAG server successfully connected to Postgres. A failed DB connection passes health until a query is attempted. |
| **Embedding dimension not validated at provisioning** | If a table was previously created with different dimensions and the config specifies a different model, `RAGSchemaResource` skips table creation (IF NOT EXISTS) and the mismatch surfaces only at query time. |
| **`pgedge_vectorizer` not automated** | Schema setup (`RAGSchemaResource`) installs `pgvector` but not `pgedge_vectorizer`. Users who want background embedding generation must run `CREATE EXTENSION` manually. |
| **Multiple databases per service instance** | All pipelines within one service instance share the same Postgres connection (same host, user, database). Two separate databases require two separate service instances. |
| **Swarm config immutability** | Docker Swarm configs are immutable. Any config change requires creating a new config object and restarting the service. This is handled internally but means every config change restarts the container. |

---

## Open questions — resolved

| Question | Answer |
|----------|--------|
| Does the pgEdge Postgres image have `vector` enabled by default? | `vector` (pgvector) is installed but not enabled per-database. `RAGSchemaResource` runs `CREATE EXTENSION IF NOT EXISTS vector` automatically. `pgedge_vectorizer` requires manual `CREATE EXTENSION` today. |
| Single pipeline per service instance, or multiple? | **Multiple pipelines supported** — the control plane generates one pipeline per entry in `config.pipelines`. All pipelines share the same database connection. Two separate databases require two separate service instances. |
| Support Voyage AI now or defer? | Voyage AI is **supported** — `voyage_api_key` is written to a host key file and referenced in the YAML `api_keys` section. The RAG server supports it as an embedding provider. |
| Extension installation automation post-spike? | `pgvector` is automated via `RAGSchemaResource`. `pgedge_vectorizer` is deferred — document as manual prerequisite. A future `ServiceExtensionResource` could automate it. |
| How many lifecycle resources? | **8 resources** per service instance per host: `NetworkResource`, `ServiceUserRole`, `RAGAPIKeysResource`, `ServiceConfigResource`, `RAGSchemaResource`, `ServiceInstanceSpecResource`, `ServiceInstanceResource`, `ServiceInstanceMonitor`. |
