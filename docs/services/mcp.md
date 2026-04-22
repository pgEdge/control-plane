# pgEdge Postgres MCP Server

The MCP service runs a [Model Context Protocol](https://modelcontextprotocol.io)
server alongside your database. The Control Plane provisions an MCP
server container on each specified host; the server connects to the
database using the credentials of the `connect_as` user. AI agents
and LLM-powered applications call the server's tools to query data,
inspect schemas, run EXPLAIN plans, and perform vector similarity
searches. For more information, see the
[pgEdge Postgres MCP](https://github.com/pgEdge/pgedge-postgres-mcp)
project.

See [Managing Services](managing.md) for instructions on adding,
updating, and removing services. The sections below cover MCP-specific
configuration.

## Configuration Reference

All configuration fields are provided in the `config` object of the
service spec.

### LLM Proxy

The MCP server can optionally act as an LLM proxy for the built-in web
client and direct HTTP chat. When the LLM proxy is disabled (the
default), the MCP server still exposes all tools over HTTP. AI clients
such as Claude Desktop or Cursor connect via the MCP protocol and
supply their own LLM. The following table describes the LLM proxy
configuration fields:

| Field                  | Type    | Default | Description |
|------------------------|---------|---------|-------------|
| `llm_enabled`          | boolean | `false` | Set to `true` to enable the LLM proxy. When `false`, the fields below must not be provided. |
| `llm_provider`         | string  | —       | The LLM provider to use. One of: `anthropic`, `openai`, `ollama`. Required when `llm_enabled` is `true`. |
| `llm_model`            | string  | —       | The model name for the selected provider (e.g., `claude-sonnet-4-5`, `gpt-4o`, `llama3.2`). Required when `llm_enabled` is `true`. |
| `anthropic_api_key`    | string  | —       | Your Anthropic API key. Required when `llm_provider` is `anthropic`. |
| `openai_api_key`       | string  | —       | Your OpenAI API key. Required when `llm_provider` is `openai`. |
| `ollama_url`           | string  | —       | The base URL of your Ollama server (e.g., `http://ollama-host:11434`). Required when `llm_provider` is `ollama`. |

### Security

The security fields control database access level and initial
authentication for the MCP server. The following table describes the
security configuration fields:

| Field            | Type    | Default | Description |
|------------------|---------|---------|-------------|
| `allow_writes`   | boolean | `false` | When `true`, the `query_database` tool can execute write statements and the service connects to the primary node. When `false`, write statements are rejected by the MCP server and the service prefers a standby node. |
| `init_token`     | string  | —       | A bootstrap token for initial access to the MCP server. See [Bootstrapping](#bootstrapping). |
| `init_users`     | array   | —       | Initial user accounts to create on the MCP server. See [Bootstrapping](#bootstrapping). |

### Tools

The MCP server exposes tools to AI agents that enable querying, schema
inspection, vector search, and other operations. All tools are enabled
by default; set the corresponding `disable_*` field to `true` to turn
off a specific tool. The following table describes the available tools:

| Tool                    | Disable Flag                    | Description |
|-------------------------|---------------------------------|-------------|
| `query_database`        | `disable_query_database`        | Execute SQL queries against the database. Writes are only permitted when `allow_writes` is `true`. |
| `get_schema_info`       | `disable_get_schema_info`       | Inspect tables, columns, and indexes. |
| `similarity_search`     | `disable_similarity_search`     | Perform vector similarity search. Requires embeddings to be configured. |
| `execute_explain`       | `disable_execute_explain`       | Run `EXPLAIN ANALYZE` on a query. |
| `generate_embedding`    | `disable_generate_embedding`    | Generate a vector embedding for a given text. Requires embeddings to be configured. |
| `search_knowledgebase`  | `disable_search_knowledgebase`  | Search a configured knowledge base. |
| `count_rows`            | `disable_count_rows`            | Count rows matching a condition. |

### Embeddings

Embedding support enables the `similarity_search` and
`generate_embedding` tools. All embedding fields are optional, but
`embedding_model` is required when `embedding_provider` is set. The
following table describes the embedding configuration fields:

| Field                  | Type   | Description |
|------------------------|--------|-------------|
| `embedding_provider`   | string | The embedding provider. One of: `voyage`, `openai`, `ollama`. |
| `embedding_model`      | string | The embedding model name (e.g., `voyage-3`, `text-embedding-3-small`, `nomic-embed-text`). Required when `embedding_provider` is set. |
| `embedding_api_key`    | string | API key for the embedding provider. Required for `voyage` and `openai` providers. |

### LLM Tuning

The LLM tuning fields control the behavior of the LLM proxy and are
only valid when `llm_enabled` is `true`. The following table describes
the LLM tuning fields:

| Field              | Type    | Range            | Description |
|--------------------|---------|------------------|-------------|
| `llm_temperature`  | number  | `0.0`–`2.0`     | Controls randomness in LLM responses. Lower values produce more deterministic output. |
| `llm_max_tokens`   | integer | Positive integer | Maximum number of tokens in the LLM response. |

### Connection Pool

The connection pool fields control how many database connections the
MCP server maintains. The following table describes the connection pool
configuration fields:

| Field              | Type    | Description |
|--------------------|---------|-------------|
| `pool_max_conns`   | integer | Maximum number of database connections the service maintains in its pool. Must be a positive integer. |

## Bootstrapping

You can use `init_token` and `init_users` to establish initial access
when provisioning an MCP service for the first time.

The `init_token` field sets a bootstrap token for authenticating with
the MCP server. The bootstrap token is useful for automating initial
setup or connecting a client immediately after provisioning.

The `init_users` field creates one or more user accounts during
provisioning. In the following example, the `init_users` field defines
two user accounts:

```json
"init_users": [
    { "username": "alice", "password": "s3cr3t" },
    { "username": "bob",   "password": "s3cr3t2" }
]
```

The Control Plane hashes tokens (SHA-256) and passwords (bcrypt) before
writing them to disk. The MCP server stores these files on a persistent
bind-mount volume that survives container restarts. After bootstrap, the
MCP server owns these files; you manage additional tokens and users
through the MCP server's native CLI or API.

!!! warning

    `init_token` and `init_users` can only be set when the service is
    first created. Providing either field in a subsequent update request
    will be rejected. Store your bootstrap credentials before
    provisioning; they cannot be retrieved or modified through the
    Control Plane after the service is created.

## Examples

The following examples show how to configure the MCP service for common
use cases.

### Minimal (No LLM)

In the following example, a `curl` command provisions an MCP service
without the LLM proxy. The MCP server exposes all tools over HTTP, and
you connect via an MCP client that supplies its own LLM:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "mcp_user",
                        "password": "changeme",
                        "db_owner": true,
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "connect_as": "mcp_user",
                        "config": {
                            "init_token": "my-bootstrap-token",
                            "init_users": [
                                { "username": "alice", "password": "s3cr3t" }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Anthropic (Claude) with LLM Proxy

In the following example, a `curl` command enables the LLM proxy with
Anthropic as the provider:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "mcp_user",
                        "password": "changeme",
                        "db_owner": true,
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "connect_as": "mcp_user",
                        "config": {
                            "llm_enabled": true,
                            "llm_provider": "anthropic",
                            "llm_model": "claude-sonnet-4-5",
                            "anthropic_api_key": "sk-ant-...",
                            "init_token": "my-bootstrap-token",
                            "init_users": [
                                { "username": "alice", "password": "s3cr3t" }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### OpenAI with Embeddings

In the following example, a `curl` command enables the LLM proxy with
OpenAI and configures embedding support:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "mcp_user",
                        "password": "changeme",
                        "db_owner": true,
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "connect_as": "mcp_user",
                        "config": {
                            "llm_enabled": true,
                            "llm_provider": "openai",
                            "llm_model": "gpt-4o",
                            "openai_api_key": "sk-...",
                            "embedding_provider": "openai",
                            "embedding_model": "text-embedding-3-small",
                            "embedding_api_key": "sk-...",
                            "init_token": "my-bootstrap-token",
                            "init_users": [
                                { "username": "alice", "password": "s3cr3t" }
                            ]
                        }
                    }
                ]
            }
        }'
    ```

### Ollama (Self-Hosted)

In the following example, a `curl` command configures the MCP service
to use a self-hosted Ollama server for both the LLM and embeddings:

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "example",
            "spec": {
                "database_name": "example",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "database_users": [
                    {
                        "username": "mcp_user",
                        "password": "changeme",
                        "db_owner": true,
                        "attributes": ["LOGIN"]
                    }
                ],
                "services": [
                    {
                        "service_id": "mcp-server",
                        "service_type": "mcp",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 8080,
                        "connect_as": "mcp_user",
                        "config": {
                            "llm_enabled": true,
                            "llm_provider": "ollama",
                            "llm_model": "llama3.2",
                            "ollama_url": "http://ollama-host:11434",
                            "embedding_provider": "ollama",
                            "embedding_model": "nomic-embed-text"
                        }
                    }
                ]
            }
        }'
    ```

## Connecting to the MCP Server

The MCP server accepts JSON-RPC 2.0 requests once the service instance
reaches the `running` state. Send requests to the following endpoint:

```text
POST http://{host}:{port}/mcp/v1
```

Replace `{host}` with the hostname of the host running the instance.
Replace `{port}` with the value from the `port` field of the service
spec.

### Authenticating with an Init Token

If you provisioned the service with an `init_token`, you can use the
token immediately as a Bearer token. In the following example, a `curl`
command calls the `get_schema_info` tool using the bootstrap token:

=== "curl"

    ```sh
    curl -sX POST http://host-1:8080/mcp/v1 \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer my-bootstrap-token" \
        -d '{
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": "get_schema_info",
                "arguments": {}
            }
        }' | jq .
    ```

### Authenticating with a User Account

If you provisioned the service with `init_users`, authenticate using
the `authenticate_user` tool to obtain a session token. In the
following example, a `curl` command authenticates as user `alice`:

=== "curl"

    ```sh
    curl -sX POST http://host-1:8080/mcp/v1 \
        -H "Content-Type: application/json" \
        -d '{
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": "authenticate_user",
                "arguments": {
                    "username": "alice",
                    "password": "s3cr3t"
                }
            }
        }' | jq .
    ```

A successful response returns a `session_token` you can use as a Bearer
token for subsequent requests:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"expires_at\":\"...\",\"message\":\"Authentication successful\",\"session_token\":\"<token>\",\"success\":true}"
      }
    ]
  }
}
```

### Connecting with Claude Desktop

You can connect Claude Desktop to the MCP server using `mcp-remote`.
Claude provides its own LLM; the MCP server only serves tools. This
works regardless of the `llm_enabled` setting.

Add the following to your Claude Desktop config
(`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "pgedge": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "http://{host}:{port}/mcp/v1",
        "--header",
        "Authorization: Bearer {token}"
      ]
    }
  }
}
```

Replace `{host}` and `{port}` with the host and port of your MCP
service instance. Replace `{token}` with your `init_token` or a
session token from `authenticate_user`.

Restart Claude Desktop to apply the configuration. The pgEdge MCP
tools will then appear in your conversations.
