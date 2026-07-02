package swarm

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestBuildSetStorageSecretSQL_S3CompatAWS: endpoint present => pass endpoint +
// path-style + SSL.
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
	// endpoint present as a bound arg, alongside path-style + SSL.
	assert.Equal(t, []any{"AKIA_TEST", "SECRET_TEST", "http://seaweedfs:8333", "us-east-1", "path", true}, args)
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
