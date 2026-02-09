# Service Instance Database Credentials

The Control Plane generates and manages database credentials for each service instance.

## Overview

Each service instance receives dedicated database credentials with read-only access.
The credentials provide security isolation between service instances; services cannot modify database data.

## Credential Generation Workflow

The `CreateServiceUser` workflow activity generates credentials during service instance provisioning.

The credential generation workflow follows these steps:

1. The activity connects to the primary database instance using admin credentials.
2. The activity generates a deterministic username from the service ID and host ID.
3. The activity generates a 44-character base64url password from 32 random bytes.
4. The activity executes SQL statements to create a user with a read-only role.
5. The activity stores the credentials in etcd and injects them into the service container.

The following source files implement credential generation:

- The `CreateServiceUser` activity resides in `server/internal/workflows/activities/create_service_user.go`.
- The `GenerateServiceUsername()` function resides in `server/internal/database/service_instance.go`.
- The `RandomString(32)` function in the `server/internal/utils` package generates passwords.
- The `CreateUserRole()` function in the `server/internal/postgres` package creates database users.

## Username Format

Service usernames follow a deterministic pattern based on the service ID and host ID.

In the following example, the username combines the `svc_` prefix with the service and host identifiers:

```text
Format: svc_{service_id}_{host_id}

Example:
  Service ID:         "mcp-server"
  Host ID:            "host1"
  Generated Username: "svc_mcp-server_host1"
```

The username format provides the following benefits:

- The `svc_` prefix distinguishes service accounts from application users.
- The same service ID and host ID combination always produces the same username.
- The service ID and host ID combination is unique within each database.

### PostgreSQL Compatibility

PostgreSQL limits identifier length to 63 characters.
The system truncates the username to 63 characters when the combined values exceed that limit.

## Password Generation

The `utils.RandomString(32)` function reads 32 bytes from `crypto/rand` and base64url-encodes the result.

The generated passwords have the following properties:

- The password contains 256 bits of entropy from 32 random bytes.
- The character set includes base64url characters: `A-Z`, `a-z`, `0-9`, `-`, and `_`.
- The encoded password is 44 characters long.
- The `crypto/rand` package provides cryptographic randomness.

The password strength protects against brute-force attacks; the format is compatible with PostgreSQL.

## Database Permissions

The system grants each service user the `pgedge_application_read_only` role.

The role provides the following permissions:

- The user can execute `SELECT` queries on all tables.
- The user can execute read-only functions.
- The user cannot execute `INSERT`, `UPDATE`, `DELETE`, or DDL statements.

This approach follows the principle of least privilege; services can query data but cannot modify the data.

### Permission Rationale

Read-only access prevents several categories of risk:

- A compromised service cannot corrupt the database data.
- A buggy service cannot accidentally modify application data.
- A service cannot execute schema changes that could break the application.

All data modifications must go through the application layer for business logic enforcement.

## Credential Storage and Injection

The system stores credentials in etcd and injects them into service containers at startup.

### Storage in etcd

The system stores credentials in etcd as part of the `ServiceInstance` metadata.
Credentials are stored as plaintext JSON; etcd access control is the primary protection layer.

In the following example, the credentials appear within the service instance record:

```json
{
  "service_instance_id": "...",
  "credentials": {
    "username": "svc_mcp-server_host1",
    "password": "<plaintext-base64url>",
    "role": "pgedge_application_read_only"
  }
}
```

The etcd key follows the pattern `/service_instances/{database_id}/{service_instance_id}`.

### Injection into Containers

The system injects credentials as environment variables into service containers at startup.

In the following example, the container receives standard PostgreSQL connection variables:

```bash
PGUSER=svc_mcp-server_host1
PGPASSWORD=<44-char-base64url-password>
PGHOST=postgres-instance-hostname
PGPORT=5432
PGDATABASE=database_name
PGSSLMODE=prefer
```

PostgreSQL client libraries automatically recognize these standard environment variables.

## Security Considerations

The credential system addresses isolation, rotation, and revocation.

### Isolation

The following measures enforce credential isolation:

- Each service instance receives unique credentials that are not shared.
- One compromised service cannot access the credentials of another service.
- Read-only access limits the damage from a compromised service.
- The system never logs or prints passwords to `stdout` or `stderr`.

### Storage

The system stores credentials as plaintext JSON in etcd.
etcd access control restricts which clients can read credential data.
Docker Swarm transmits credentials within the overlay network.

A future enhancement will integrate a secrets manager (Vault or AWS Secrets Manager) for encrypted storage at rest.

### Rotation

The system does not currently support credential rotation.
A future enhancement will add automatic rotation with zero downtime.

The planned rotation workflow follows these steps:

1. The system generates new credentials for the service instance.
2. The system restarts service containers with the new credentials.
3. The system revokes the old credentials after a grace period.

### Revocation

The system automatically revokes credentials under the following conditions:

- A service instance deletion triggers credential revocation.
- A database deletion triggers credential revocation for all associated services.
- Removing a service from the database spec triggers declarative credential revocation.

The revocation is immediate; the system drops the database user and terminates active connections.

## Credential Lifecycle

The credential lifecycle spans five stages from provisioning through deletion.

1. The `ProvisionServices` workflow creates credentials via the `CreateServiceUser` activity.
   The username is deterministic; the password is cryptographically random.

2. The system stores the credentials in etcd as plaintext JSON.
   The storage path follows `/service_instances/{database_id}/{service_instance_id}`.

3. The Docker Swarm service spec injects credentials as environment variables.
   The service connects to the database using standard `libpq` environment variables.

4. The service connects to the database with read-only access.
   The user can execute `SELECT` queries and read-only functions.

5. The system revokes credentials when the service instance is deleted.
   The system drops the database user and removes the etcd metadata.

## Troubleshooting

The following sections describe common credential-related issues and their solutions.

### Service Cannot Connect

Verify the following items when a service cannot connect to the database:

1. Verify the service instance state is "running" via `GET /v1/databases/{id}`.
2. Ensure the database credentials exist in etcd.
3. Check that the database user exists by running `SELECT * FROM pg_user WHERE usename LIKE 'svc_%'`.
4. Test network connectivity from the service container to the database.
5. Inspect the service logs for connection error messages.

### Permission Denied Errors

Service users have read-only access; write operations fail by design.

The following operations produce expected permission errors:

- `INSERT`, `UPDATE`, and `DELETE` statements fail because the service role is read-only.
- `CREATE`, `ALTER`, and `DROP` statements fail because the service cannot modify the schema.

Consider the following solutions:

- Modify the service to use read-only queries for data access.
- Route data modifications through the application API.

### Username Collision

Username collisions are rare because the service instance ID is unique within each database.

Verify the following items when a collision is suspected:

- Verify there are no duplicate service instance IDs in etcd.
- Run `SELECT * FROM pg_user WHERE usename = 'svc_<prefix>'` to check whether the user exists.

## Future Enhancements

The following features will be considered for future releases.

- Read/Write users based on use-case requirements.
- Automatic credential rotation will provide periodic rotation with zero downtime.
- Secret manager integration will store passwords in Vault or AWS Secrets Manager.
- Custom role support will allow users to specify database roles per service.
- Certificate-based authentication will replace passwords with TLS client certificates.

## References

The following source files implement the credential system:

- The `CreateServiceUser` activity resides in `server/internal/workflows/activities/create_service_user.go`.
- The `ServiceUser` type resides in `server/internal/database/service_instance.go`.
- The `GenerateServiceUsername()` function generates deterministic usernames.
- The `server/internal/postgres` package creates database user roles.

See the [PostgreSQL Roles Documentation](https://www.postgresql.org/docs/current/user-manag.html) for details on role management.

### Workflow Sequence

The following diagram shows the credential creation workflow:

```text
UpdateDatabase Workflow
  └─> ProvisionServices Sub-Workflow
        └─> For each service instance:
              ├─> CreateServiceUser Activity
              │     ├─> Connect to database primary
              │     ├─> Generate username (deterministic)
              │     ├─> Generate password (random)
              │     ├─> Execute CREATE USER
              │     ├─> Grant pgedge_application_read_only role
              │     └─> Return credentials
              ├─> GenerateServiceInstanceResources Activity
              └─> StoreServiceInstance Activity (saves credentials to etcd)
```
