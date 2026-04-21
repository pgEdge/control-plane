# pgEdge RAG Server

The RAG (Retrieval-Augmented Generation) service runs an intelligent query
server alongside your database. The service uses vector and keyword search
to retrieve relevant document chunks from PostgreSQL and synthesizes
LLM-generated answers based on the retrieved context. For more information,
see the [pgEdge RAG Server](https://github.com/pgEdge/pgedge-rag-server)
project.

## Overview

The Control Plane provisions a RAG service container on each specified
host. The service connects to the database using an existing user specified
in the `connect_as` field (which must be defined in `database_users`). The
credentials are automatically embedded in the service configuration by the
Control Plane. Client applications submit natural language queries to the
service, which performs hybrid vector and keyword search against document
tables and returns LLM-synthesized answers with source citations.

See [Managing Services](managing.md) for instructions on adding,
updating, and removing services. The sections below cover RAG-specific
configuration.

## Database Prerequisites

Before deploying a RAG service, your PostgreSQL database must have:

1. **pgvector extension** installed and enabled
2. **Document table(s)** with text and vector columns
3. **HNSW index** on vector columns for fast similarity search
4. **GIN index** on text columns for keyword search (BM25)

The Control Plane can automatically provision all of these during database
creation using the `scripts.post_database_create` hook. See [Preparing the
Database](#preparing-the-database) for a complete example. Alternatively,
you can provision these manually after database creation.

## Automation & Responsibilities

The Control Plane handles certain setup tasks automatically during database
and service creation:

**Automated (Control Plane)**
- Creating pgvector extension
- Creating document tables and indexes (via `scripts.post_database_create`)
- Embedding RAG service credentials into configuration files
- Deploying RAG container and health monitoring

**Manual (You Provide)**
- **Schema Design**: Deciding table structure, column names, vector dimensions
- **Embedding Generation**: Using external APIs (OpenAI, Voyage, Ollama) to vectorize documents
- **Document Loading**: Inserting documents and embeddings into the database
- **API Credentials**: Providing LLM and embedding provider API keys

## Configuration Reference

All configuration fields are provided in the `config` object of the
service spec.

### Service Connection

The `connect_as` field (at the service level) specifies which database user
the RAG service will authenticate as. This user **must already be defined** in
the `database_users` array when creating the database. The Control Plane
automatically embeds that user's credentials in the service configuration.

Example:
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

The `pipelines` array (required) defines one or more RAG workflows. Each
pipeline specifies which tables to search, which embedding provider to use,
and which LLM to use for answer generation.

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

| Field | Type | Description |
|---|---|---|
| `provider` | string | Required. The embedding provider. One of: `openai`, `voyage`, `anthropic`, `ollama`. |
| `model` | string | Required. The embedding model name (e.g., `text-embedding-3-small`, `voyage-3`, `nomic-embed-text`). |
| `api_key` | string | API key for the provider. Required for `openai`, `voyage`, and `anthropic`. Not used for `ollama`. |
| `base_url` | string | Optional. Custom base URL for the provider API. For `ollama`, defaults to `http://localhost:11434`. |

### LLM Configuration

The `rag_llm` object configures the LLM provider used to synthesize the
final answer from retrieved documents. `api_key` is required for all
providers except `ollama`.

| Field | Type | Description |
|---|---|---|
| `provider` | string | Required. The LLM provider. One of: `anthropic`, `openai`, `ollama`. |
| `model` | string | Required. The model name (e.g., `claude-sonnet-4-20250514`, `gpt-4o`, `llama3.2`). |
| `api_key` | string | API key for the provider. Required for `anthropic` and `openai`. Not used for `ollama`. |
| `base_url` | string | Optional. Custom base URL for API gateway routing. For `ollama`, defaults to `http://localhost:11434`. |

!!! note
    If `embedding_llm` and `rag_llm` share the same provider and both specify
    an `api_key`, the values must be identical. The RAG server maintains one
    key slot per provider and cannot reconcile two different values.

### Table Configuration

Each table in a pipeline specifies how to access document text and
embeddings.

| Field | Type | Description |
|---|---|---|
| `table` | string | Required. The table or view name containing documents. |
| `text_column` | string | Required. Column name containing the document text. |
| `vector_column` | string | Required. Column name containing the embedding vectors. |
| `id_column` | string | Optional. Column name for document IDs. Defaults to the table's primary key. Required for views. |

### Search Configuration

The `search` object tunes how documents are retrieved before being passed
to the LLM.

| Field | Type | Default | Description |
|---|---|---|---|
| `hybrid_enabled` | boolean | `true` | Enable hybrid search combining vector similarity and BM25 keyword matching. Set to `false` for vector-only search. |
| `vector_weight` | float | `0.5` | Weight for vector search versus BM25 (0.0–1.0). Higher values prioritize semantic relevance. |

### Defaults Configuration

The optional `defaults` object sets fallback values applied to any pipeline
that does not specify its own `token_budget` or `top_n`.

| Field | Type | Description |
|---|---|---|
| `defaults.token_budget` | integer | Default max tokens for context documents. Must be a positive integer. |
| `defaults.top_n` | integer | Default number of documents to retrieve. Must be a positive integer. |

## Preparing the Database

Before deploying a RAG service, you must prepare your PostgreSQL database
with pgvector, document tables, and indexes. The Control Plane automatically
executes these during database creation when you include them in the
`scripts.post_database_create` array in your database specification.

### Required Schema

The following SQL statements should be included in `scripts.post_database_create`
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

These statements are included as individual entries in the `scripts.post_database_create`
array (see examples below).

### Vector Dimensions

Adjust the `vector(N)` dimension based on your embedding model:

| Provider | Model | Dimensions |
|----------|-------|-----------|
| OpenAI | `text-embedding-3-small` | 1536 |
| OpenAI | `text-embedding-3-large` | 3072 |
| Voyage AI | `voyage-3` / `voyage-3-large` | 1024 |
| Ollama | Varies by model | Check model documentation |

### Loading Documents

After the database and RAG service are deployed, you are responsible for
generating embeddings for your documents and loading them into the database.
The Control Plane does not automate this step—you must run this process
separately, typically via an external application or scheduled task.

Here's a Python example using OpenAI to generate embeddings and load documents:

```python
#!/usr/bin/env python3
"""Generate embeddings and load documents into the RAG database."""

import psycopg2
from psycopg2.extras import execute_values
from openai import OpenAI
import os
import sys

# Configuration
OPENAI_API_KEY = os.environ.get("OPENAI_API_KEY")
DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_USER = os.environ.get("DB_USER", "admin")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "admin_password")
DB_NAME = os.environ.get("DB_NAME", "knowledge_base")

def chunk_text(text, chunk_size=500, overlap=50):
    """Split text into overlapping chunks."""
    chunks = []
    for i in range(0, len(text), chunk_size - overlap):
        chunk = text[i:i + chunk_size]
        if chunk.strip():
            chunks.append(chunk)
    return chunks

def generate_embeddings(texts, client):
    """Generate embeddings for multiple texts."""
    response = client.embeddings.create(
        model="text-embedding-3-small",
        input=texts
    )
    return [item.embedding for item in response.data]

# Sample documents
documents = [
    {
        "title": "pgEdge Overview",
        "content": "pgEdge is a distributed PostgreSQL system...",
        "source": "docs"
    },
    {
        "title": "RAG Guide",
        "content": "RAG enables intelligent question-answering systems...",
        "source": "docs"
    }
]

if not OPENAI_API_KEY:
    print("ERROR: OPENAI_API_KEY environment variable not set")
    sys.exit(1)

client = OpenAI(api_key=OPENAI_API_KEY)
conn = psycopg2.connect(
    host=DB_HOST,
    user=DB_USER,
    password=DB_PASSWORD,
    database=DB_NAME
)
cur = conn.cursor()

total_inserted = 0

for doc in documents:
    print(f"Processing: {doc['title']}")
    chunks = chunk_text(doc["content"])
    
    if chunks:
        # Generate embeddings for all chunks
        embeddings = generate_embeddings(chunks, client)
        
        # Prepare batch insert data
        insert_data = [
            (chunk, embedding, doc["title"], doc["source"])
            for chunk, embedding in zip(chunks, embeddings)
        ]
        
        # Batch insert
        insert_query = """
            INSERT INTO documents_content_chunks
                (content, embedding, title, source)
            VALUES %s
        """
        execute_values(cur, insert_query, insert_data)
        conn.commit()
        
        inserted = len(insert_data)
        total_inserted += inserted
        print(f"  Inserted {inserted} chunks")

print(f"\nTotal chunks inserted: {total_inserted}")
cur.close()
conn.close()
```

**Usage:**
```bash
pip install psycopg2-binary openai
export OPENAI_API_KEY="sk-..."
export DB_HOST="localhost"
export DB_USER="admin"
export DB_PASSWORD="admin_password"
export DB_NAME="knowledge_base"
python3 load_rag_documents.py
```

## Examples

The following examples show how to configure the RAG service for common
use cases. All examples use the `scripts.post_database_create` field to
automatically provision the database schema (pgvector extension, tables,
and indexes) during database creation.

### Minimal (OpenAI + Anthropic)

In the following example, a `curl` command provisions a RAG service with
OpenAI for embeddings and Anthropic Claude for answer generation:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge_base",
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
                                        "model": "claude-sonnet-4-20250514",
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

In the following example, OpenAI is used for both embeddings and answer
generation:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge_base",
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

In the following example, Voyage AI is used for embeddings and the service
is configured for vector-only search (disabling BM25 keyword matching):

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge_base",
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
                                        "model": "claude-sonnet-4-20250514",
                                        "api_key": "sk-ant-..."
                                    },
                                    "search": {
                                        "hybrid_enabled": false,
                                        "vector_weight": 1.0
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

In the following example, the RAG service uses a self-hosted Ollama server
for both embeddings and answer generation. No API key is required; the
Ollama server URL is provided via `base_url`:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge_base",
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

In the following example, two pipelines share default `token_budget` and
`top_n` values set at the `defaults` level:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "knowledge_base",
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
                                        "model": "claude-sonnet-4-20250514",
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
                                        "model": "claude-sonnet-4-20250514",
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

## End-to-End Walkthrough

This section shows the complete flow from database creation to a working
pipeline query.

### Step 1 — Create the Database

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

### Step 2 — Check the Database and Service Status

Run the following command after ~60–90 seconds to check the database is
ready and the RAG service is running:

=== "curl"

    ```sh
    curl -s http://host-1:3000/v1/databases/knowledge-base
    ```

In the response, look for two things:

- `state: "available"` at the top level — the database is provisioned
  and healthy
- `service_ready: true` inside `service_instances[].status` — the RAG
  container is up and accepting requests

```
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

The `host_port` value is the port to use when querying the RAG service.
If you used a fixed `port: 9200` in the service spec, this will always
be `9200`.

!!! tip
    Use a fixed `port` value (e.g. `9200`) in the service spec rather than
    `port: 0`. When `port: 0` is used, Docker assigns a random host port
    that changes each time the RAG container is replaced (e.g. after an
    API key update), requiring you to look up the new port each time.

### Step 3 — Load Documents

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

```bash
pip install psycopg2-binary openai
export OPENAI_API_KEY="sk-..."
export DB_HOST="host-1"
export DB_USER="admin"
export DB_PASSWORD="admin_password"
export DB_NAME="knowledge_base"
python3 load_documents.py
```

Verify documents were inserted:

```bash
psql "postgresql://admin:admin_password@host-1:5432/knowledge_base" \
  -c "SELECT COUNT(*), COUNT(embedding) FROM documents_content_chunks;"
```

### Step 4 — Query the Pipeline

```bash
curl -X POST http://host-1:9200/v1/pipelines/default \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How does multi-active replication work?",
    "include_sources": true
  }'
```

A successful response:

```json
{
    "answer": "Multi-active replication allows multiple PostgreSQL nodes to accept writes simultaneously...",
    "sources": [
        {"id": "5", "content": "...", "score": 0.82},
        {"id": "1", "content": "...", "score": 0.79}
    ],
    "tokens_used": 1243
}
```

`sources` is only populated when `include_sources: true` is set in the
request.

### Step 5 — Update the Service Config

To update the service (for example, to rotate an API key or change the
LLM model), submit a `POST /v1/databases/{id}` with the complete updated
spec. The update endpoint requires all fields — include `database_name`,
`nodes`, `database_users`, and the full `services` array:

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

The RAG service container is replaced with the new configuration. Poll
the database status until `state` is `"available"` and `service_ready`
is `true` before sending queries.

## Querying the RAG Service

Once the service is running, submit queries to retrieve answers based on
your documents.

### List Available Pipelines

=== "curl"

    ```bash
    curl http://localhost:9200/v1/pipelines
    ```

### Query a Pipeline

=== "curl"

    ```bash
    curl -X POST http://localhost:9200/v1/pipelines/default \
      -H "Content-Type: application/json" \
      -d '{
        "query": "How does RAG improve LLM responses?",
        "include_sources": true
      }'
    ```

### Request Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `query` | string | — | Required. The natural language question to answer. |
| `include_sources` | boolean | `false` | Return the source documents used to generate the answer. |
| `top_n` | integer | — | Override the pipeline's `top_n` for this request. |
| `stream` | boolean | `false` | Stream the answer as Server-Sent Events. |

### Response Format

```json
{
    "answer": "RAG (Retrieval-Augmented Generation) improves LLM responses by retrieving relevant documents from your database before generating answers. This grounds the LLM in your specific data, reducing hallucinations and improving accuracy...",
    "sources": [
        {
            "id": "42",
            "content": "The RAG service enables retrieval-augmented generation workflows...",
            "score": 0.87
        }
    ],
    "tokens_used": 1243
}
```

`sources` is only populated when `include_sources` is `true` in the request.

The RAG service uses **hybrid search**, combining two complementary search
techniques that are merged using **Reciprocal Rank Fusion (RRF)**:

1. **Vector Similarity Search**: Retrieves documents semantically similar to
   the query using cosine distance on embeddings.
2. **BM25 Keyword Search**: Retrieves documents with exact keyword matches
   using TF-IDF scoring.

This combination ensures the LLM receives context that is both semantically
relevant and keyword-relevant. Documents appearing in both result sets receive
higher scores, naturally prioritizing highly-relevant results.

### Search Configuration

Configure search behavior in the pipeline:

```json
"search": {
    "hybrid_enabled": true,
    "vector_weight": 0.7
}
```

| Parameter | Range | Description |
|-----------|-------|-------------|
| `hybrid_enabled` | `true` / `false` | Enable hybrid search (default: `true`). Set to `false` for vector-only search. |
| `vector_weight` | 0.0–1.0 | Weight for vector search vs BM25 (default: `0.5`). Higher values prioritize semantic relevance. |

### Token Budget

The `token_budget` field controls how much context is sent to the LLM:

- Documents are ranked and packed in order until the budget is exhausted
- The final document is truncated at a sentence boundary (not mid-word)

Increase the budget to send more context, or decrease it to reduce LLM costs.

## User-Managed Responsibilities

You are responsible for:

1. **Embedding Generation**: Using embedding provider APIs (OpenAI, Voyage AI,
   Ollama) to generate vector embeddings for your documents
2. **Document Ingestion**: Loading document text and embeddings into the
   `documents_content_chunks` table
3. **API Keys**: Providing credentials for embedding and LLM providers in the
   service `config`
4. **Chunking Strategy**: Deciding how to split large documents for optimal
   retrieval (e.g., 500-1000 character chunks with overlap)

The Control Plane handles:

1. **Schema Provisioning**: Automatically creating pgvector extension, tables,
   and indexes via `scripts.post_database_create` during database creation
2. **Service Deployment**: Provisioning and managing the RAG container
3. **Database Credentials**: Automatically embedding the `connect_as` user's
   credentials in the service configuration (credentials must be defined in
   `database_users` during database creation)
4. **Health Monitoring**: Checking service health and restarting on failure

## Troubleshooting

### About Automated Scripts

The `scripts.post_database_create` field executes SQL automatically during
database creation. Some important details:

- **Execution Timing**: Scripts run once, immediately after Spock is initialized
- **Transactional**: All statements execute within a single transaction
- **No Re-Execution**: If you update the database spec later, scripts are not re-run
- **Constraints**: Some SQL commands are not allowed within transactions:
  - `VACUUM`, `ANALYZE` (use `REINDEX` instead)
  - `CREATE INDEX CONCURRENTLY`
  - `CREATE DATABASE`, `DROP DATABASE`

If a script fails during database creation, you can use `update-database` to
retry after fixing the problematic statement.

### Service Fails to Start

**Check database connectivity:**

```bash
# From host, verify database is accessible
psql -h localhost -U admin -d knowledge_base -c "SELECT 1"
```

**Check user permissions:**

```sql
-- Verify the service user exists and has table access
\du+ admin
\dt documents_content_chunks
```

### Poor Query Results

**Verify documents are loaded:**

```sql
-- Check document count
SELECT COUNT(*) FROM documents_content_chunks;

-- Verify embeddings exist
SELECT COUNT(*) FROM documents_content_chunks WHERE embedding IS NOT NULL;
```

**Inspect embedding quality:**

```sql
-- Find documents similar to a test query embedding
SELECT id, content, 1 - (embedding <=> '[0.1, 0.2, ...]'::vector) as similarity
FROM documents_content_chunks
ORDER BY similarity DESC
LIMIT 5;
```

**Try simpler queries:**

Start with factual, keyword-based questions before complex analytical questions.

### Empty Context Window

If the RAG service returns limited context, the token budget may be exhausted. Increase it:

```json
"token_budget": 8000
```

Or store smaller, more focused document chunks.

## Next Steps

- Once you've validated the RAG service with manual documents, consider automating embedding generation
- Implement document versioning and updates for evolving knowledge bases
- Set up monitoring for query latency and answer quality
- Explore pgedge_vectorizer for automated chunking and embedding in high-volume scenarios

## Responsibility Summary

| Step | Who | How |
|---|---|---|
| Provision schema (pgvector, tables, indexes) | Control Plane | `scripts.post_database_create` in database spec |
| Deploy RAG container | Control Plane | Automatic on `POST /v1/databases` |
| Inject database credentials | Control Plane | Automatic via `connect_as` field |
| Health monitoring and restart | Control Plane | Automatic |
| Generate embeddings | You | Call OpenAI / Voyage / Ollama API |
| Load documents into table | You | `INSERT` using psycopg2 or any Postgres client |
| Submit queries | Your application | `POST /v1/pipelines/{name}` on the RAG service |

## Additional Resources

- [RAG Server Repository](https://github.com/pgEdge/pgedge-rag-server)
- [RAG Server Documentation](https://docs.pgedge.com/pgedge-rag-server/)
- [pgvector Documentation](https://github.com/pgvector/pgvector)
- [Managing Services](managing.md)
