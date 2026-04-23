# pgEdge RAG Server

The RAG (Retrieval-Augmented Generation) service runs an intelligent
query server alongside your database. The service uses vector and
keyword search to retrieve relevant document chunks from PostgreSQL
and synthesizes LLM-generated answers based on the retrieved context.
For more information, see the
[pgEdge RAG Server](https://github.com/pgEdge/pgedge-rag-server)
project.

## Overview

The Control Plane provisions a RAG service container on each specified
host. The service connects to the database using an existing user
specified in the `connect_as` field, which must be defined in
`database_users`, and automatically embeds that user's credentials in
the service configuration. Client applications submit natural language
queries to the service, which performs hybrid vector and keyword search
against document tables and returns LLM-synthesized answers with source
citations.

See [Managing Services](managing.md) for instructions on adding,
updating, and removing services. The sections below cover RAG-specific
configuration.

## Database Prerequisites

Before deploying a RAG service, your PostgreSQL database must have the
following items configured:

- The pgvector extension must be installed and enabled.
- The database must have document tables with text and vector columns.
- An HNSW index on vector columns enables fast similarity search.
- A GIN index on text columns enables keyword search (BM25).

The Control Plane can automatically provision all of these during
database creation using the `scripts.post_database_create` hook. See
[Preparing the Database](#preparing-the-database) for a complete
example. Alternatively, you can provision these manually after
database creation.

## Configuration Reference

All configuration fields are provided in the `config` object of the
service spec.

### Service Connection

The `connect_as` field at the service level specifies which database
user the RAG service authenticates as. This user must already be
defined in the `database_users` array when creating the database. The
Control Plane automatically embeds that user's credentials in the
service configuration.

The following example shows the `connect_as` field in the service
spec:

```json
{
  "service_id": "rag",
  "service_type": "rag",
  "connect_as": "app_read_only",
  "config": { ... }
}
```

In this example, `app_read_only` must be defined in `database_users`:

```json
{
  "username": "app_read_only",
  "password": "your_password",
  "attributes": ["LOGIN"]
}
```

### Pipeline Configuration

The `pipelines` array (required) defines one or more RAG workflows.
Each pipeline specifies which tables to search, which embedding
provider to use, and which LLM to use to generate answers.

The following table describes the pipeline configuration fields:

| Field | Type | Description |
|---|---|---|
| `pipelines[].name` | string | Required. Pipeline identifier used in query URLs. Lowercase alphanumeric, hyphens, and underscores. Must not start with a hyphen. |
| `pipelines[].description` | string | Optional. Human-readable pipeline description. |
| `pipelines[].tables[]` | array | Required. Array of table specifications. See [Table Configuration](#table-configuration). |
| `pipelines[].embedding_llm` | object | Required. Embedding provider config. See [Embedding Configuration](#embedding-configuration). |
| `pipelines[].rag_llm` | object | Required. LLM provider config. See [LLM Configuration](#llm-configuration). |
| `pipelines[].token_budget` | integer | Optional. Max tokens for context documents sent to the LLM. |
| `pipelines[].top_n` | integer | Optional. Number of documents to retrieve per query. |
| `pipelines[].system_prompt` | string | Optional. Custom system prompt prepended to every LLM request for this pipeline. |
| `pipelines[].search` | object | Optional. Search behavior settings. See [Search Configuration](#search-configuration). |

### Embedding Configuration

The `embedding_llm` object configures the embedding provider used to
vectorize each incoming query. The embedding vector is then used for
similarity search against stored document vectors. All required fields
must be set; `api_key` is not required for `ollama`.

The following table describes the embedding configuration fields:

| Field | Type | Description |
|---|---|---|
| `provider` | string | Required. The embedding provider. One of: `openai`, `voyage`, `ollama`. |
| `model` | string | Required. The embedding model name (e.g., `text-embedding-3-small`, `voyage-3`, `nomic-embed-text`). |
| `api_key` | string | API key for the provider. Required for `openai` and `voyage`. Not used for `ollama`. |
| `base_url` | string | Optional. Custom base URL for the provider API. Required for `ollama` - set this to the network-accessible address of your Ollama server (e.g., `http://192.168.1.10:11434`). |

### LLM Configuration

The `rag_llm` object configures the LLM provider used to synthesize
the final answer from retrieved documents. `api_key` is required for
all providers except `ollama`.

The following table describes the LLM configuration fields:

| Field | Type | Description |
|---|---|---|
| `provider` | string | Required. The LLM provider. One of: `anthropic`, `openai`, `ollama`. |
| `model` | string | Required. The model name (e.g., `claude-sonnet-4-5`, `gpt-4o`, `llama3.2`). |
| `api_key` | string | API key for the provider. Required for `anthropic` and `openai`. Not used for `ollama`. |
| `base_url` | string | Optional. Custom base URL for API gateway routing. Required for `ollama` - set this to the network-accessible address of your Ollama server (e.g., `http://192.168.1.10:11434`). |

!!! note
    If `embedding_llm` and `rag_llm` share the same provider and both
    specify an `api_key`, the values must be identical. The pgEdge RAG
    Server maintains one key slot per provider and cannot reconcile
    two different values.

### Table Configuration

Each table in a pipeline specifies how to access document text and
embeddings. The following table describes the table configuration
fields:

| Field | Type | Description |
|---|---|---|
| `table` | string | Required. The table or view name containing documents. |
| `text_column` | string | Required. Column name containing the document text. |
| `vector_column` | string | Required. Column name containing the embedding vectors. |
| `id_column` | string | Optional. Column name for document IDs. Defaults to the table's primary key. Required for views. |

### Search Configuration

The `search` object tunes how documents are retrieved before being
passed to the LLM. The following table describes the search
configuration fields:

| Field | Type | Default | Description |
|---|---|---|---|
| `hybrid_enabled` | boolean | `true` | Enable hybrid search combining vector similarity and BM25 keyword matching. Set to `false` for vector-only search. |
| `vector_weight` | float | `0.5` | Weight for vector search versus BM25 (0.0-1.0). Higher values prioritize semantic relevance. |

### Defaults Configuration

The optional `defaults` object sets fallback values applied to any
pipeline that does not specify its own `token_budget` or `top_n`. The
following table describes the defaults configuration fields:

| Field | Type | Description |
|---|---|---|
| `defaults.token_budget` | integer | Default max tokens for context documents. Must be a positive integer. |
| `defaults.top_n` | integer | Default number of documents to retrieve. Must be a positive integer. |

## Preparing the Database

Before deploying a RAG service, you must prepare your PostgreSQL
database with pgvector, document tables, and indexes. The Control
Plane automatically executes these during database creation when you
include them in the `scripts.post_database_create` array in your
database specification.

### Required Schema

Include the following SQL statements in `scripts.post_database_create`
to automatically initialize the database schema during creation:

```sql
-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create documents table with embeddings
CREATE TABLE IF NOT EXISTS documents_content_chunks (
    id BIGSERIAL PRIMARY KEY,
    content TEXT NOT NULL,
    embedding vector(1536),
    title TEXT,
    source TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- HNSW index for vector similarity search
CREATE INDEX IF NOT EXISTS documents_embedding_idx
    ON documents_content_chunks USING hnsw (embedding vector_cosine_ops);

-- GIN index for keyword search (BM25)
CREATE INDEX IF NOT EXISTS documents_content_idx
    ON documents_content_chunks USING gin (to_tsvector('english', content));
```

These statements are included as individual entries in the
`scripts.post_database_create` array (see examples below).

### Vector Dimensions

Adjust the `vector(N)` dimension to match your embedding model. The
following table shows common models and their vector dimensions:

| Provider | Model | Dimensions |
|----------|-------|-----------|
| OpenAI | `text-embedding-3-small` | 1536 |
| OpenAI | `text-embedding-3-large` | 3072 |
| Voyage AI | `voyage-3` / `voyage-3-large` | 1024 |
| Ollama | `nomic-embed-text` | 768 |
| Ollama | Other models | Check model documentation |

## Examples

The following examples show how to configure the RAG service for
common use cases. The first example includes the complete
`scripts.post_database_create` setup to automatically provision the
database schema (pgvector extension, tables, and indexes) using
`vector(1536)` for OpenAI embeddings. Subsequent examples focus on
service configuration variations and omit the schema setup for brevity.
If you use a different embedding model, adjust the `vector(N)` dimension
in your schema to match - for example, `vector(1024)` for `voyage-3` or
`vector(768)` for `nomic-embed-text`.

### Minimal (OpenAI + Anthropic)

In the following example, a `curl` command provisions a RAG service
that uses OpenAI for embeddings and Anthropic Claude to generate answers:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "scripts": {
                    "post_database_create": [
                        "CREATE EXTENSION IF NOT EXISTS vector",
                        "CREATE TABLE IF NOT EXISTS documents_content_chunks (id BIGSERIAL PRIMARY KEY, content TEXT NOT NULL, embedding vector(1536), title TEXT, source TEXT)",
                        "CREATE INDEX ON documents_content_chunks USING hnsw (embedding vector_cosine_ops)",
                        "CREATE INDEX ON documents_content_chunks USING gin (to_tsvector('\''english'\'', content))"
                    ]
                },
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "admin",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "description": "Main RAG pipeline",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-..."
                                    },
                                    "token_budget": 4000,
                                    "top_n": 10
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### OpenAI End-to-End

In the following example, OpenAI is used for both embeddings and to generate
answers:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "admin",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "openai",
                                        "model": "gpt-4o",
                                        "api_key": "sk-..."
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Voyage AI with Vector-Only Search

In the following example, Voyage AI is used for embeddings and the
service is configured for vector-only search (disabling BM25 keyword
matching):

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "admin",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "voyage",
                                        "model": "voyage-3",
                                        "api_key": "pa-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-..."
                                    },
                                    "search": {
                                        "hybrid_enabled": false
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Ollama (Self-Hosted)

In the following example, the RAG service uses a self-hosted Ollama
server for both embeddings and answer generation. No API key is
required; the Ollama server URL is provided via `base_url`:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "admin",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "ollama",
                                        "model": "nomic-embed-text",
                                        "base_url": "http://ollama-host:11434"
                                    },
                                    "rag_llm": {
                                        "provider": "ollama",
                                        "model": "llama3.2",
                                        "base_url": "http://ollama-host:11434"
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Multiple Pipelines with Shared Defaults

In the following example, two pipelines share default `token_budget`
and `top_n` values set at the `defaults` level:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    }
                ],
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "admin",
                        "config": {
                            "defaults": {
                                "token_budget": 4000,
                                "top_n": 10
                            },
                            "pipelines": [
                                {
                                    "name": "docs",
                                    "description": "Product documentation",
                                    "tables": [
                                        {
                                            "table": "doc_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-..."
                                    }
                                },
                                {
                                    "name": "support",
                                    "description": "Support ticket history",
                                    "tables": [
                                        {
                                            "table": "ticket_chunks",
                                            "text_column": "body",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-..."
                                    },
                                    "top_n": 5
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

## Deployment Guide

This section shows the complete flow from database creation to a
working pipeline query.

### Step 1 - Create the Database

Include `scripts.post_database_create` to automatically provision the
pgvector schema during database creation. This avoids any manual setup
after deployment. Use a fixed `port` value for the RAG service so the
URL stays stable across container restarts.

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge-base",
            "spec": {
                "database_name": "knowledge_base",
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    },
                    {
                        "username": "app_read_only",
                        "password": "readonly_password",
                        "attributes": ["LOGIN"]
                    }
                ],
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "scripts": {
                    "post_database_create": [
                        "CREATE EXTENSION IF NOT EXISTS vector",
                        "CREATE TABLE IF NOT EXISTS documents_content_chunks (id BIGSERIAL PRIMARY KEY, content TEXT NOT NULL, embedding vector(1536), title TEXT, source TEXT)",
                        "CREATE INDEX ON documents_content_chunks USING hnsw (embedding vector_cosine_ops)",
                        "CREATE INDEX ON documents_content_chunks USING gin (to_tsvector('\''english'\'', content))",
                        "GRANT SELECT ON documents_content_chunks TO app_read_only"
                    ]
                },
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "app_read_only",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "description": "Main RAG pipeline",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-..."
                                    },
                                    "token_budget": 4000,
                                    "top_n": 10
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Step 2 - Check the Database and Service Status

Run the following command after approximately 60-90 seconds to check
that the database is ready and the RAG service is running:

=== "curl"

    ```sh
    curl -s http://host-1:3000/v1/databases/knowledge-base
    ```

In the response, look for the following items:

- The `state: "available"` field at the top level confirms that the
  database is provisioned and healthy.
- The `service_ready: true` field inside `service_instances[].status`
  confirms that the RAG container is up and accepting requests.

```text
{
  state: "available"
  instances: [
    {
      state: "available"
      postgres: {
        patroni_state: "running"
        role: "primary"
      }
    }
  ]
  service_instances: [
    {
      state: "running"
      status: {
        service_ready: true
        ports: [
          {
            container_port: 8080
            host_port: 9200
            name: "tcp"
          }
        ]
        last_health_at: "2026-04-22T10:00:00Z"
      }
    }
  ]
}
```

The `host_port` value is the port to use when querying the RAG
service. If you used a fixed `port: 9200` in the service spec, the
host port will always be `9200`.

!!! tip
    Use a fixed `port` value (e.g. `9200`) in the service spec rather
    than `port: 0`. When `port: 0` is used, Docker assigns a random
    host port that changes each time the RAG container is replaced
    (e.g. after an API key update), requiring you to look up the new
    port each time.

### Step 3 - Load Documents

The RAG service needs documents with embeddings in the database before
it can answer queries. The following Python script generates embeddings
using OpenAI and inserts them into `documents_content_chunks`:

```python
#!/usr/bin/env python3
import psycopg2
from psycopg2.extras import execute_values
from openai import OpenAI
import os

client = OpenAI(api_key=os.environ["OPENAI_API_KEY"])
conn = psycopg2.connect(
    host=os.environ.get("DB_HOST", "host-1"),
    port=int(os.environ.get("DB_PORT", "5432")),
    user=os.environ.get("DB_USER", "admin"),
    password=os.environ.get("DB_PASSWORD", "admin_password"),
    database=os.environ.get("DB_NAME", "knowledge_base"),
)
cur = conn.cursor()

documents = [
    {"title": "My Doc", "content": "Full document text goes here...", "source": "docs"},
]

def chunk_text(text, size=500, overlap=50):
    return [text[i:i+size] for i in range(0, len(text), size-overlap) if text[i:i+size].strip()]

for doc in documents:
    chunks = chunk_text(doc["content"])
    resp = client.embeddings.create(model="text-embedding-3-small", input=chunks)
    embeddings = [item.embedding for item in resp.data]
    execute_values(cur,
        "INSERT INTO documents_content_chunks (content, embedding, title, source) VALUES %s",
        [(c, e, doc["title"], doc["source"]) for c, e in zip(chunks, embeddings)],
    )
    conn.commit()
    print(f"Loaded {len(chunks)} chunks from '{doc['title']}'")

cur.close()
conn.close()
```

Install the dependencies and run the script with the following
commands:

```bash
pip install psycopg2-binary openai
export OPENAI_API_KEY="sk-..."
export DB_HOST="host-1"
export DB_USER="admin"
export DB_PASSWORD="admin_password"
export DB_NAME="knowledge_base"
python3 load_documents.py
```

To verify that documents were inserted, run the following query:

```bash
psql "postgresql://admin:admin_password@host-1:5432/knowledge_base" \
  -c "SELECT COUNT(*), COUNT(embedding) FROM documents_content_chunks;"
```

### Step 4 - Query the Pipeline

Send a query to the RAG service using the following command:

```bash
curl -X POST http://host-1:9200/v1/pipelines/default \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How does multi-active replication work?",
    "include_sources": true
  }'
```

A successful response looks like this:

```json
{
    "answer": "Multi-active replication allows multiple PostgreSQL nodes to accept writes simultaneously...",
    "sources": [
        {"id": "5", "content": "...", "score": 0.00820},
        {"id": "1", "content": "...", "score": 0.00806}
    ],
    "tokens_used": 1243
}
```

`sources` is only populated when `include_sources: true` is set in
the request.

### Step 5 - Update the Service Config

To update the service (for example, to rotate an API key or change
the LLM model), submit a `POST /v1/databases/{id}` with the complete
updated spec. The update endpoint requires all fields - include
`database_name`, `nodes`, `database_users`, and the full `services`
array:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases/knowledge-base \
        -H 'Content-Type: application/json' \
        --data '{
            "spec": {
                "database_name": "knowledge_base",
                "port": 5432,
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "admin",
                        "password": "admin_password",
                        "db_owner": true,
                        "attributes": ["SUPERUSER", "LOGIN"]
                    },
                    {
                        "username": "app_read_only",
                        "password": "readonly_password",
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "rag",
                        "service_type": "rag",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 9200,
                        "connect_as": "app_read_only",
                        "config": {
                            "pipelines": [
                                {
                                    "name": "default",
                                    "tables": [
                                        {
                                            "table": "documents_content_chunks",
                                            "text_column": "content",
                                            "vector_column": "embedding"
                                        }
                                    ],
                                    "embedding_llm": {
                                        "provider": "openai",
                                        "model": "text-embedding-3-small",
                                        "api_key": "sk-..."
                                    },
                                    "rag_llm": {
                                        "provider": "anthropic",
                                        "model": "claude-sonnet-4-5",
                                        "api_key": "sk-ant-NEW-KEY"
                                    },
                                    "token_budget": 4000,
                                    "top_n": 10
                                }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

The RAG service container is replaced with the new configuration.
Poll the database status until `state` is `"available"` and
`service_ready` is `true` before sending queries.

## Querying the RAG Service

Once the service is running, submit queries to retrieve answers based
on your documents.

### List Available Pipelines

To list all configured pipelines, send the following request:

=== "curl"

    ```bash
    curl http://host-1:9200/v1/pipelines
    ```

### Query a Pipeline

To submit a query to a pipeline, send a POST request with the query
text:

=== "curl"

    ```bash
    curl -X POST http://host-1:9200/v1/pipelines/default \
      -H "Content-Type: application/json" \
      -d '{
        "query": "How does RAG improve LLM responses?",
        "include_sources": true
      }'
    ```

### Request Fields

The following table describes the query request fields:

| Field | Type | Default | Description |
|---|---|---|---|
| `query` | string | - | Required. The natural language question to answer. |
| `include_sources` | boolean | `false` | Return the source documents used to generate the answer. |
| `top_n` | integer | - | Override the pipeline's `top_n` for this request. |
| `stream` | boolean | `false` | Stream the answer as Server-Sent Events. |

### Response Format

A successful query response looks like this:

```json
{
    "answer": "RAG (Retrieval-Augmented Generation) improves LLM responses by retrieving relevant documents from your database before generating answers. This grounds the LLM in your specific data, reducing hallucinations and improving accuracy...",
    "sources": [
        {
            "id": "42",
            "content": "The RAG service enables retrieval-augmented generation workflows...",
            "score": 0.00820
        }
    ],
    "tokens_used": 1243
}
```

`sources` is only populated when `include_sources` is `true` in the
request.

The RAG service's hybrid search combines two complementary techniques,
merged using Reciprocal Rank Fusion (RRF):

- Vector similarity search retrieves documents semantically similar to
  the query using cosine distance on embeddings.
- BM25 keyword search retrieves documents with exact keyword matches
  using TF-IDF scoring.

This combination ensures the LLM receives context that is both
semantically relevant and keyword-relevant. Documents appearing in
both result sets receive higher scores, naturally prioritizing
highly-relevant results.

### Token Budget

The `token_budget` field controls how much context is sent to the LLM.
The service ranks documents and packs them in order until the budget
is exhausted. The final document is truncated at a sentence boundary.
Increase the budget to send more context, or decrease it to reduce
LLM costs.

## Troubleshooting

The following sections describe common issues and how to resolve them.

### About Automated Scripts

The `scripts.post_database_create` field executes SQL automatically
during database creation. The following details apply:

| Property | Details |
|---|---|
| Execution timing | Scripts run once, immediately after Spock is initialized. |
| Transactional | All statements execute within a single transaction. |
| No re-execution | If you update the database spec later, scripts are not re-run. |
| Constraints | Some SQL commands are not allowed within transactions, including `VACUUM`, `ANALYZE`, `CREATE INDEX CONCURRENTLY`, `CREATE DATABASE`, and `DROP DATABASE`. |

If a script fails during database creation, you can use
`update-database` to retry after fixing the problematic statement.

### Service Fails to Start

To diagnose a service that fails to start, check database
connectivity and user permissions.

To verify that the database is accessible, run the following command:

```bash
psql -h host-1 -U admin -d knowledge_base -c "SELECT 1"
```

To verify that the service user (`app_read_only`) exists and has table
access, run the following query:

```sql
\du+ app_read_only
\dt documents_content_chunks
```

### Poor Query Results

To diagnose poor query results, verify that documents are loaded and
embeddings are present.

To check document counts and embedding coverage, run the following
queries:

```sql
SELECT COUNT(*) FROM documents_content_chunks;

SELECT COUNT(*) FROM documents_content_chunks WHERE embedding IS NOT NULL;
```

To find documents similar to a test query embedding, run the following
query:

```sql
SELECT id, content, 1 - (embedding <=> '[0.1, 0.2, ...]'::vector) as similarity
FROM documents_content_chunks
ORDER BY similarity DESC
LIMIT 5;
```

Start with factual, keyword-based questions before complex analytical
questions to verify that the pipeline is working correctly.

### Empty Context Window

If the RAG service returns limited context, the token budget may be
exhausted. Increase the budget in the pipeline configuration:

```json
"token_budget": 8000
```

Alternatively, store smaller, more focused document chunks to fit more
context within the budget.

## Responsibility Summary

The following table summarizes which tasks are handled by the Control
Plane and which are your responsibility:

| Step | Who | How |
|---|---|---|
| Provision schema (pgvector, tables, indexes) | Control Plane | `scripts.post_database_create` in database spec |
| Deploy RAG container | Control Plane | Automatic on `POST /v1/databases` |
| Inject database credentials | Control Plane | Automatic via `connect_as` field |
| Health monitoring and restart | Control Plane | Automatic |
| Generate embeddings | You | Call OpenAI / Voyage / Ollama API |
| Load documents into table | You | `INSERT` using psycopg2 or any Postgres client |
| Submit queries | Your application | `POST /v1/pipelines/{name}` on the RAG service |

## Next Steps

The following resources provide more information on related topics:

- The [Managing Services](managing.md) guide describes how to add,
  update, and remove services.
- The [pgEdge RAG Server](https://github.com/pgEdge/pgedge-rag-server)
  repository contains the pgEdge RAG Server source code.
- The [pgEdge RAG Server Documentation](https://docs.pgedge.com/pgedge-rag-server/)
  covers the pgEdge RAG Server API and configuration in detail.
- The [pgvector Documentation](https://github.com/pgvector/pgvector)
  explains how to install and use the pgvector extension.
