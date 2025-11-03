# Copilot Instructions for pgEdge Control Plane

## Project Overview
- **pgEdge Control Plane** manages PostgreSQL databases using pgEdge's active-active replication.
- Major components:
  - `api/`: API definitions, versioning, and OpenAPI specs.
  - `server/`: Main service logic, including CLI and internal APIs.
  - `client/`: Client-side logic for interacting with the control plane.
  - `e2e/`: End-to-end Go tests for cluster/database operations.
  - `docs/`: User/developer documentation and OpenAPI references.

## Developer Workflows
- **Build:** Use `make` (see root `Makefile`). Example: `make build`.
- **Test:** Run Go tests with `make test` or `go test ./...`. E2E tests are in `e2e/`.
- **Docker:** Build and run services using Dockerfiles in `docker/` and `docker-compose.yaml` files for local/dev/CI environments.
- **API Generation:** OpenAPI specs in `api/apiv1/openapi.go` and `docs/api/openapi3.json`. Regenerate with scripts in `docs/scripts/` if needed.

## Project Conventions
- **Go Modules:** All Go code uses modules (`go.mod` at root).
- **Versioning:** API versioning in `api/version.go` and `api/version.txt`.
- **Configuration:** Service config via TOML/JSON files (`buildkit.toml`, `create_db.json`, etc.).
- **Testing:** E2E tests use fixtures in `e2e/fixtures/` and custom test scenarios in `bruno/test-scenarios/`.
- **Docs:** Markdown docs in `docs/` follow topic-based structure (guides, concepts, API reference).

## Integration Points
- **pgEdge Replication:** Core logic integrates with pgEdge replication (see `server/` and `api/`).
- **External Services:** Docker Compose setups for multi-node clusters (`docker/control-plane-dev/`, etc.).
- **Bruno:** API scenario tests in `bruno/test-scenarios/` for automated API validation.

## Patterns & Examples
- **API Design:** See `api/apiv1/openapi.go` for endpoint structure and conventions.
- **Error Handling:** Centralized in `client/errors.go` and `mqtt/errors.go`.
- **Cluster Operations:** E2E tests in `e2e/` demonstrate cluster creation, failover, switchover, and backup/restore.
- **Custom Scripts:** Utility scripts in `docs/scripts/` and `hack/` for dev/test automation.

## Quick Start
- Build: `make build`
- Test: `make test`
- Run locally: See `docs/development/running-locally.md`
- API reference: `docs/api/openapi.md`

---
_If any section is unclear or missing key details, please provide feedback to improve these instructions._
