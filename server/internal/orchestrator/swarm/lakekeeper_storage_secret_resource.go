package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LakekeeperStorageSecretResource)(nil)

const ResourceTypeLakekeeperStorageSecret resource.Type = "swarm.lakekeeper_storage_secret"

func LakekeeperStorageSecretResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperStorageSecret,
	}
}

// LakekeeperStorageSecretResource stores the object-store credential inside the
// database via ColdFront's set_storage_secret function, so that the ColdFront
// extension on the node can read/write Iceberg data in the warehouse's bucket.
//
// It runs once against the node's primary Postgres (PrimaryExecutor, like the
// PostgREST preflight resource) after the database — and therefore the
// coldfront extension — is available. It depends on the Postgres database
// resource for that ordering.
//
// The set_storage_secret functions upsert, so re-running is safe; SecretSet is
// a sentinel so Refresh can distinguish "never run" from "already applied".
//
// The credential is passed to Postgres exclusively as bound query parameters
// and is NEVER logged.
type LakekeeperStorageSecretResource struct {
	ServiceInstanceID string         `json:"service_instance_id"`
	DatabaseID        string         `json:"database_id"`
	DatabaseName      string         `json:"database_name"`
	NodeName          string         `json:"node_name"`
	Config            map[string]any `json:"config"`
	SecretSet         bool           `json:"secret_set"`
}

func (r *LakekeeperStorageSecretResource) ResourceVersion() string { return "1" }

func (r *LakekeeperStorageSecretResource) DiffIgnore() []string {
	return []string{"/secret_set"}
}

func (r *LakekeeperStorageSecretResource) Identifier() resource.Identifier {
	return LakekeeperStorageSecretResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperStorageSecretResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *LakekeeperStorageSecretResource) Dependencies() []resource.Identifier {
	// Depend on the database resource so the coldfront extension is available
	// before we call set_storage_secret.
	return []resource.Identifier{
		database.PostgresDatabaseResourceIdentifier(r.NodeName, r.DatabaseName),
	}
}

func (r *LakekeeperStorageSecretResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *LakekeeperStorageSecretResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.SecretSet {
		return fmt.Errorf("%w: coldfront storage secret has not yet been set", resource.ErrNotFound)
	}
	return nil
}

func (r *LakekeeperStorageSecretResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.setSecret(ctx, rc); err != nil {
		return err
	}
	r.SecretSet = true
	return nil
}

func (r *LakekeeperStorageSecretResource) Update(ctx context.Context, rc *resource.Context) error {
	// set_storage_secret upserts, so re-running is safe.
	if err := r.setSecret(ctx, rc); err != nil {
		return err
	}
	r.SecretSet = true
	return nil
}

func (r *LakekeeperStorageSecretResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *LakekeeperStorageSecretResource) setSecret(ctx context.Context, rc *resource.Context) error {
	cfg, err := parseLakekeeperStorageConfig(r.Config)
	if err != nil {
		return err
	}

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("coldfront set_storage_secret: failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("coldfront set_storage_secret: failed to connect to database %s on node %s: %w",
			r.DatabaseName, r.NodeName, err)
	}
	defer conn.Close(ctx)

	return execSetStorageSecret(ctx, conn, cfg)
}

// setStorageSecretExec is the minimal Postgres exec surface needed by
// execSetStorageSecret, satisfied by *pgx.Conn. It lets the SQL selection be
// unit-tested without a live database.
type setStorageSecretExec interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// execSetStorageSecret builds and executes the correct ColdFront function call
// for the configured provider. The credential values are bound as query
// parameters ($1, $2, ...) and never interpolated into the SQL text, so they
// cannot leak via query logging.
func execSetStorageSecret(ctx context.Context, conn setStorageSecretExec, cfg *lakekeeperStorageConfig) error {
	sql, args := buildSetStorageSecretSQL(cfg)
	if _, err := conn.Exec(ctx, sql, args...); err != nil {
		// The error is from Postgres and does not echo bound parameters, so it
		// is safe to wrap; do NOT include the credential.
		return fmt.Errorf("coldfront set_storage_secret failed for provider %q: %w", cfg.Provider, err)
	}
	return nil
}

// buildSetStorageSecretSQL returns the SQL and bound arguments for the
// provider. aws/gcs use coldfront.set_storage_secret; azure uses
// coldfront.set_storage_secret_azure. Credentials are returned as args, never
// embedded in the SQL string.
//
// Endpoint presence is the discriminator, consistent with the warehouse
// storage profile:
//   - endpoint ABSENT (cloud AWS): pass endpoint => NULL, which selects
//     DuckDB's native per-Region virtual-hosted + HTTPS addressing (required
//     for Regions launched after 2019). url_style/use_ssl are left at their
//     defaults — they are irrelevant when the endpoint is NULL, and passing a
//     non-NULL endpoint here forces path-style and breaks modern Regions with
//     HTTP 400 (ColdFront docs/object_store.md).
//   - endpoint PRESENT (S3-compatible / GCS): pass the endpoint with
//     url_style => 'path' and use_ssl derived from the endpoint scheme
//     (https:// => true, http:// => false, schemeless => true). The scheme is
//     stripped so the DuckDB secret stores host:port, not a URL.
func buildSetStorageSecretSQL(cfg *lakekeeperStorageConfig) (string, []any) {
	switch cfg.Provider {
	case "aws":
		return buildS3SetStorageSecretSQL(
			cfg.Credential["access_key_id"],
			cfg.Credential["secret_access_key"],
			cfg.Endpoint,
			cfg.Region,
		)
	case "gcs":
		// GCS is only reachable through its S3-compatible HMAC endpoint, so it
		// always has an endpoint (default to the canonical host when unset).
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "storage.googleapis.com"
		}
		return buildS3SetStorageSecretSQL(
			cfg.Credential["hmac_access_id"],
			cfg.Credential["hmac_secret"],
			endpoint,
			cfg.Region,
		)
	case "azure":
		return `SELECT coldfront.set_storage_secret_azure(p_connection_string => $1)`,
			[]any{cfg.Credential["connection_string"]}
	default:
		// parseLakekeeperStorageConfig has already validated the provider, so
		// this branch is unreachable in practice.
		return "", nil
	}
}

// buildS3SetStorageSecretSQL builds the set_storage_secret call for an S3 or
// S3-compatible store, driving cloud-vs-s3-compat semantics off endpoint
// presence. keyID/secret are always bound as parameters.
func buildS3SetStorageSecretSQL(keyID, secret, endpoint, region string) (string, []any) {
	if endpoint == "" {
		// Cloud AWS: endpoint NULL selects native vhost + HTTPS. Do not pass
		// url_style/use_ssl — defaults apply and a non-NULL endpoint here is
		// exactly the shape the docs warn against.
		return `SELECT coldfront.set_storage_secret(
			p_key_id => $1,
			p_secret => $2,
			p_endpoint => NULL,
			p_region => $3
		)`, []any{keyID, secret, region}
	}
	// S3-compatible / GCS: explicit endpoint with path-style. The DuckDB secret
	// wants a bare host:port, and use_ssl is driven off the endpoint scheme so
	// plain-HTTP stores (MinIO / self-hosted) work, not just HTTPS.
	host, useSSL := deriveEndpointSSL(endpoint)
	return `SELECT coldfront.set_storage_secret(
		p_key_id => $1,
		p_secret => $2,
		p_endpoint => $3,
		p_region => $4,
		p_url_style => $5,
		p_use_ssl => $6
	)`, []any{keyID, secret, host, region, "path", useSSL}
}

// deriveEndpointSSL splits an S3 endpoint into its bare host:port form and the
// use_ssl flag implied by its scheme. An https:// endpoint => TLS; an http://
// endpoint => plaintext; a schemeless endpoint keeps its value and defaults to
// TLS (the safe assumption for a public host such as storage.googleapis.com).
// Surrounding whitespace and a trailing slash are trimmed, and the scheme match
// is case-insensitive, so copy-pasted values like " HTTPS://minio:9000/ " yield
// a clean host:port that DuckDB accepts.
func deriveEndpointSSL(endpoint string) (host string, useSSL bool) {
	endpoint = strings.TrimSpace(endpoint)
	lower := strings.ToLower(endpoint)
	switch {
	case strings.HasPrefix(lower, "https://"):
		host, useSSL = endpoint[len("https://"):], true
	case strings.HasPrefix(lower, "http://"):
		host, useSSL = endpoint[len("http://"):], false
	default:
		host, useSSL = endpoint, true
	}
	return strings.TrimRight(host, "/"), useSSL
}

// ensure *pgx.Conn satisfies setStorageSecretExec at compile time.
var _ setStorageSecretExec = (*pgx.Conn)(nil)
