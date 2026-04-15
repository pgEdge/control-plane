# PostgREST

PostgREST turns your PostgreSQL schema into a REST API. The Control
Plane deploys and manages a PostgREST container alongside your database,
handling connection strings, multi-host failover, and schema validation
on every deploy.

## Overview

The Control Plane provisions a PostgREST container on each host you
specify. The container connects to the database using
automatically-managed credentials and serves HTTP at your configured
port. Anonymous requests run as the configured `db_anon_role`.
JWT-authenticated requests switch to any role granted to the
authenticator.

See [Managing Services](managing.md) for instructions on adding,
updating, and removing services. The sections below cover
PostgREST-specific configuration.

## Configuration Reference

All configuration fields go in the `config` object of the service spec.
All fields are optional. The defaults work for a read-only API.

### Database

| Field          | Type    | Default                          | Description |
|----------------|---------|----------------------------------|-------------|
| `db_schemas`   | string  | `"public"`                       | Comma-separated schemas to expose. Each schema is checked at deploy time. Provisioning fails if any schema does not exist. |
| `db_anon_role` | string  | `"pgedge_application_read_only"` | PostgreSQL role for unauthenticated requests. The role must exist before deployment. |
| `db_pool`      | integer | `10`                             | Connection pool size. Range: 1-30. |
| `max_rows`     | integer | `1000`                           | Maximum rows returned per response. Range: 1-10000. |

### JWT Authentication

JWT authentication lets clients switch PostgreSQL roles per request
using a signed token. Omit these fields to run in anonymous-only mode.

| Field                | Type   | Description |
|----------------------|--------|-------------|
| `jwt_secret`         | string | Signing key for validating incoming JWTs. Minimum 32 characters. Required to enable JWT auth. |
| `jwt_aud`            | string | Expected audience claim. PostgREST rejects tokens without a matching `aud` claim. |
| `jwt_role_claim_key` | string | JSONPath to the role field in the JWT payload. Defaults to `".role"`. |

### CORS

| Field                         | Type   | Description |
|-------------------------------|--------|-------------|
| `server_cors_allowed_origins` | string | Comma-separated list of allowed CORS origins. Omit to disable CORS headers. |

## Examples

### Read-Only API (No JWT)

This example provisions a PostgREST service with default settings. All
requests run as the anonymous role.

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "storefront",
            "spec": {
                "database_name": "storefront",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "api",
                        "service_type": "postgrest",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 3100,
                        "config": {}
                    }
                ]
            }
        }'
    ```

### JWT-Authenticated API

This example enables JWT authentication. Clients send a signed token to
switch to a specific PostgreSQL role.

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "storefront",
            "spec": {
                "database_name": "storefront",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "api",
                        "service_type": "postgrest",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 3100,
                        "config": {
                            "jwt_secret": "a-secret-key-of-at-least-32-characters"
                        }
                    }
                ]
            }
        }'
    ```

### Multiple Schemas

This example exposes two schemas. The Control Plane checks both exist
before deploying.

=== "curl"

    ```sh
    curl -X POST http://host-1:3000/v1/databases \
        -H 'Content-Type: application/json' \
        --data '{
            "id": "storefront",
            "spec": {
                "database_name": "storefront",
                "nodes": [
                    { "name": "n1", "host_ids": ["host-1"] }
                ],
                "services": [
                    {
                        "service_id": "api",
                        "service_type": "postgrest",
                        "version": "latest",
                        "host_ids": ["host-1"],
                        "port": 3100,
                        "config": {
                            "db_schemas": "public,api",
                            "jwt_secret": "a-secret-key-of-at-least-32-characters"
                        }
                    }
                ]
            }
        }'
    ```

## Querying the API

PostgREST accepts requests once the service instance reaches the
`running` state. Send requests to:

```text
http://{host}:{port}/{table_or_view}
```

Replace `{host}` with your host name, `{port}` with your service port,
and `{table_or_view}` with a table or view in an exposed schema.

### Anonymous Request

```sh
curl http://host-1:3100/products
```

### JWT-Authenticated Request

Generate a signed JWT and pass a Bearer token. The `role` claim sets
the PostgreSQL role PostgREST uses for the request.

=== "curl"

    ```sh
    TOKEN=$(python3 - <<'EOF'
    import hmac, hashlib, base64, json, time

    secret = b"a-secret-key-of-at-least-32-characters"

    def b64url(data):
        if isinstance(data, str):
            data = data.encode()
        return base64.urlsafe_b64encode(data).rstrip(b"=").decode()

    header  = b64url(json.dumps({"alg":"HS256","typ":"JWT"}))
    payload = b64url(json.dumps({"role":"app","exp":int(time.time())+3600}))
    sig     = b64url(hmac.new(secret, f"{header}.{payload}".encode(), hashlib.sha256).digest())
    print(f"{header}.{payload}.{sig}")
    EOF
    )

    curl -X POST http://host-1:3100/products \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        --data '{"name": "Widget", "price": 9.99}'
    ```

The `role` claim must name a PostgreSQL role granted to the PostgREST
authenticator user. The authenticator username is visible in the
`service_instances` field of your database status response. Grant your
application roles to that user before sending authenticated requests.

## Preflight Checks

At deploy time, the Control Plane connects to the primary Postgres node
and checks:

1. Every schema in `db_schemas` exists.
2. The role in `db_anon_role` exists.

Deployment fails with a descriptive error if either check fails. No
container starts until both checks pass.

## Multi-Host Failover

The connection string in `postgrest.conf` includes every Postgres node
hostname with `target_session_attrs=read-write`. After a primary
switchover, PostgREST reconnects to the new primary automatically. No
configuration change or restart needed.

## Schema Cache

PostgREST caches your database schema at startup. To expose a newly
added table or view without restarting the container, send a POST
request to the admin server:

```sh
curl -X POST http://host-1:3101/notify-reload
```

The admin port is always one higher than your main PostgREST port
(`{port} + 1`).

To trigger a full redeploy with a fresh schema cache, update the
service spec (for example, change `db_schemas`) and submit an update
request.
