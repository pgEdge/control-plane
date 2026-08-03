package swarm

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

// fakeExec captures the SQL and args passed to Exec so the test can assert the
// correct function is chosen and the credential is bound (not interpolated).
type fakeExec struct {
	sql  string
	args []any
}

func (f *fakeExec) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sql = sql
	f.args = args
	return pgconn.CommandTag{}, nil
}

// TestBuildSetStorageSecretSQL_CloudAWS: no endpoint => endpoint NULL (native
// vhost+HTTPS), no url_style/use_ssl. Passing a non-NULL endpoint here is the
// shape the docs warn breaks modern Regions with HTTP 400.
func TestBuildSetStorageSecretSQL_CloudAWS(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	sql, args := buildSetStorageSecretSQL(cfg)

	assert.Contains(t, sql, "coldfront.set_storage_secret(")
	assert.NotContains(t, sql, "set_storage_secret_azure")
	// endpoint => NULL literal in SQL; url_style/use_ssl omitted entirely.
	assert.Contains(t, sql, "p_endpoint => NULL")
	assert.NotContains(t, sql, "p_url_style")
	assert.NotContains(t, sql, "p_use_ssl")
	// key/secret/region bound as args; no endpoint arg.
	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "us-east-1"}, args)
	// Credential must NOT be interpolated into the SQL text.
	assert.NotContains(t, sql, "AKIA_TEST")
	assert.NotContains(t, sql, "SECRET_TEST")
}

// TestBuildSetStorageSecretSQL_S3CompatAWS: endpoint present => pass the bare
// host:port (scheme stripped) + path-style, with use_ssl derived from the
// scheme (http:// => false here).
func TestBuildSetStorageSecretSQL_S3CompatAWS(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Endpoint: "http://seaweedfs:8333",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	sql, args := buildSetStorageSecretSQL(cfg)

	assert.Contains(t, sql, "coldfront.set_storage_secret(")
	assert.Contains(t, sql, "p_url_style => $5")
	assert.NotContains(t, sql, "p_endpoint => NULL")
	// endpoint present as a bound arg (scheme stripped), path-style, http=>no SSL.
	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "seaweedfs:8333", "us-east-1", "path", false}, args)
	assert.NotContains(t, sql, "AKIA_TEST")
}

// TestBuildSetStorageSecretSQL_GCS: GCS is always S3-compatible, so it uses an
// endpoint (defaulting to the canonical host) with path-style + SSL.
func TestBuildSetStorageSecretSQL_GCS(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "gcs",
		Region:   "us",
		Credential: map[string]string{
			"hmac_access_id": "GOOG_ID",
			"hmac_secret":    "GOOG_SECRET",
		},
	}
	sql, args := buildSetStorageSecretSQL(cfg)
	assert.Contains(t, sql, "coldfront.set_storage_secret(")
	assert.NotContains(t, sql, "set_storage_secret_azure")
	assert.NotContains(t, sql, "p_endpoint => NULL")
	// gcs defaults endpoint to the GCS S3-compatible host and uses path-style.
	assert.Contains(t, args, "storage.googleapis.com")
	assert.Contains(t, args, "path")
	assert.Contains(t, args, "GOOG_ID")
	assert.NotContains(t, sql, "GOOG_SECRET")
}

// TestBuildSetStorageSecretSQL_HTTPEndpoint: a plain-HTTP endpoint (MinIO /
// self-hosted S3) must derive use_ssl=false and be stored WITHOUT the scheme —
// the DuckDB secret wants host:port, not a URL.
func TestBuildSetStorageSecretSQL_HTTPEndpoint(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Endpoint: "http://minio:9000",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	sql, args := buildSetStorageSecretSQL(cfg)

	assert.Contains(t, sql, "p_use_ssl => $6")
	// Scheme stripped to host:port; use_ssl derived false from http://.
	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "minio:9000", "us-east-1", "path", false}, args)
}

// TestBuildSetStorageSecretSQL_HTTPSEndpoint: an https endpoint derives
// use_ssl=true and is stored WITHOUT the scheme.
func TestBuildSetStorageSecretSQL_HTTPSEndpoint(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "eu-west-1",
		Endpoint: "https://s3.example.com",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	_, args := buildSetStorageSecretSQL(cfg)

	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "s3.example.com", "eu-west-1", "path", true}, args)
}

// TestBuildSetStorageSecretSQL_SchemelessEndpoint: an endpoint with no scheme
// (e.g. the GCS canonical host) keeps its value and defaults use_ssl=true.
func TestBuildSetStorageSecretSQL_SchemelessEndpoint(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Endpoint: "objects.internal:9000",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	_, args := buildSetStorageSecretSQL(cfg)

	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "objects.internal:9000", "us-east-1", "path", true}, args)
}

// TestBuildSetStorageSecretSQL_MessyEndpoint: a copy-pasted endpoint with mixed
// scheme case, surrounding whitespace, and a trailing slash still yields a clean
// host:port with use_ssl derived from the (case-insensitive) scheme.
func TestBuildSetStorageSecretSQL_MessyEndpoint(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Endpoint: "  HTTPS://minio.example.com:9000/  ",
		Credential: map[string]string{
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "SECRET_TEST",
		},
	}
	_, args := buildSetStorageSecretSQL(cfg)

	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "minio.example.com:9000", "us-east-1", "path", true}, args)
}

func TestBuildSetStorageSecretSQL_Azure(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "azure",
		Credential: map[string]string{
			"connection_string": "DefaultEndpointsProtocol=https;AccountKey=SEKRIT",
		},
	}
	sql, args := buildSetStorageSecretSQL(cfg)
	assert.Contains(t, sql, "coldfront.set_storage_secret_azure(")
	assert.NotContains(t, sql, "set_storage_secret(")
	require.Len(t, args, 1)
	assert.Equal(t, "DefaultEndpointsProtocol=https;AccountKey=SEKRIT", args[0])
	// The connection string must NOT appear in the SQL text.
	assert.NotContains(t, sql, "SEKRIT")
}

func TestExecSetStorageSecret_BindsCredentialAsArgs(t *testing.T) {
	cfg := &lakekeeperStorageConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Credential: map[string]string{
			"access_key_id":     "AKIA_XYZ",
			"secret_access_key": "S3KRET",
		},
	}
	fe := &fakeExec{}
	require.NoError(t, execSetStorageSecret(context.Background(), fe, cfg))

	// Secret is passed as a bound arg, and the SQL text uses placeholders.
	assert.Contains(t, fe.args, "S3KRET")
	assert.NotContains(t, fe.sql, "S3KRET")
	assert.True(t, strings.Contains(fe.sql, "$1"), "expected parameter placeholders in SQL")
}

// TestBuildColdfrontLocalPGDSN pins the libpq keyword DSN the coldfront
// extension attaches as `pglocal` (loopback read of PG rows for Iceberg
// writes). Format mirrors the tiering DSN (finding #8): host/port/user/dbname
// with sslmode=disable. The sslmode=disable + local-trust assumption is the
// same tracked residual as #8, to revisit with the connectivity/auth work.
func TestBuildColdfrontLocalPGDSN(t *testing.T) {
	got := buildColdfrontLocalPGDSN("mydb", "app_user")
	want := "host=localhost port=5432 user=app_user dbname=mydb sslmode=disable"
	assert.Equal(t, want, got)
}

// TestBuildColdfrontGUCStatements pins the three per-database GUCs the
// extension's attach path reads: warehouse (name), lakekeeper_endpoint (MUST
// carry the /catalog path), local_pg_dsn. All are PGC_SUSET so ALTER DATABASE
// SET by the superuser applies to new sessions with no restart. Values are
// single-quoted with embedded quotes doubled (standard_conforming_strings=on),
// matching the roles.go literal-quoting precedent.
func TestBuildColdfrontGUCStatements(t *testing.T) {
	stmts := buildColdfrontGUCStatements(
		"mydb",
		"wh",
		"http://svc:8181/catalog",
		"host=localhost port=5432 user=app_user dbname=mydb sslmode=disable",
	)
	require.Len(t, stmts, 3)
	assert.Equal(t, `ALTER DATABASE "mydb" SET coldfront.warehouse = 'wh';`, stmts[0].SQL)
	assert.Equal(t, `ALTER DATABASE "mydb" SET coldfront.lakekeeper_endpoint = 'http://svc:8181/catalog';`, stmts[1].SQL)
	assert.Equal(t, `ALTER DATABASE "mydb" SET coldfront.local_pg_dsn = 'host=localhost port=5432 user=app_user dbname=mydb sslmode=disable';`, stmts[2].SQL)
}

// TestBuildColdfrontGUCStatements_QuotesValues verifies a single quote in a
// value (e.g. a warehouse name) is doubled, not broken out of the literal.
func TestBuildColdfrontGUCStatements_QuotesValues(t *testing.T) {
	stmts := buildColdfrontGUCStatements("my'db", "wh's", "http://svc:8181/catalog", "dsn")
	require.Len(t, stmts, 3)
	// Identifier: embedded double-quote handling not exercised here, but the db
	// name is quoted as an identifier; a single quote is legal inside it.
	assert.Equal(t, `ALTER DATABASE "my'db" SET coldfront.warehouse = 'wh''s';`, stmts[0].SQL)
}

// TestGenerateLakekeeperInstanceResources_StorageSecretGUCFields verifies the
// orchestrator threads the /catalog endpoint (derived from the generated
// service name) and the connect-as user into the storage-secret resource, so it
// can set the coldfront GUCs.
func TestGenerateLakekeeperInstanceResources_StorageSecretGUCFields(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeManagedLakekeeperSpec()

	result, err := o.generateLakekeeperInstanceResources(spec)
	require.NoError(t, err)

	secretRD := findResourceByType(result.Resources, ResourceTypeLakekeeperStorageSecret)
	require.NotNil(t, secretRD)
	secretRes, err := resource.ToResource[*LakekeeperStorageSecretResource](secretRD)
	require.NoError(t, err)

	serviceName := ServiceInstanceName(spec.DatabaseID, spec.ServiceSpec.ServiceID, spec.HostID)
	wantEndpoint := "http://" + serviceName + ":8181/catalog"
	assert.Equal(t, wantEndpoint, secretRes.LakekeeperEndpoint)
	assert.Equal(t, spec.ConnectAsUsername, secretRes.ConnectAsUsername)
}
