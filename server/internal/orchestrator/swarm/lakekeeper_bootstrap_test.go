package swarm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awsBootstrapConfig is a native cloud-AWS config: no endpoint, so
// virtual-hosted addressing (flavor "aws", path-style-access false).
func awsBootstrapConfig() map[string]any {
	return map[string]any{
		"provider":    "aws",
		"bucket":      "my-bucket",
		"region":      "us-east-1",
		"warehouse":   "wh1",
		"path_prefix": "iceberg",
		"credential":  `{"access_key_id":"AKIA_TEST","secret_access_key":"SECRET_TEST"}`,
	}
}

// gcsBootstrapConfig is an S3-compatible config (GCS): endpoint present, so
// flavor "s3-compat" and path-style-access true.
func gcsBootstrapConfig() map[string]any {
	return map[string]any{
		"provider":    "gcs",
		"bucket":      "my-bucket",
		"region":      "us",
		"endpoint":    "https://storage.googleapis.com",
		"warehouse":   "wh1",
		"path_prefix": "iceberg",
		"credential":  `{"hmac_access_id":"GOOG_ID","hmac_secret":"GOOG_SECRET"}`,
	}
}

func TestParseLakekeeperStorageConfig_FailLoud(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		errSub string
	}{
		{"missing provider", map[string]any{"warehouse": "w", "credential": "{}"}, "provider is required"},
		{"bad provider", map[string]any{"provider": "digitalocean", "warehouse": "w", "credential": "{}"}, "unsupported provider"},
		{"missing warehouse", map[string]any{"provider": "aws", "credential": "{}"}, "warehouse is required"},
		{"missing credential", map[string]any{"provider": "aws", "warehouse": "w"}, "credential is required"},
		{"bad credential json", map[string]any{"provider": "aws", "warehouse": "w", "bucket": "b", "credential": "not-json"}, "not valid JSON"},
		{"missing bucket aws", map[string]any{"provider": "aws", "warehouse": "w", "credential": `{"access_key_id":"a","secret_access_key":"s"}`}, "bucket is required"},
		{"aws missing keys", map[string]any{"provider": "aws", "bucket": "b", "warehouse": "w", "credential": `{}`}, "access_key_id and secret_access_key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLakekeeperStorageConfig(tc.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSub)
		})
	}
}

func TestParseLakekeeperStorageConfig_AWS(t *testing.T) {
	cfg, err := parseLakekeeperStorageConfig(awsBootstrapConfig())
	require.NoError(t, err)
	assert.Equal(t, "aws", cfg.Provider)
	assert.Equal(t, "my-bucket", cfg.Bucket)
	assert.Equal(t, "AKIA_TEST", cfg.Credential["access_key_id"])
}

// TestRunLakekeeperBootstrap_OrderAndFlow asserts the four REST calls happen in
// order, the warehouse-id is extracted from the create response and used in the
// namespace URL.
func TestRunLakekeeperBootstrap_OrderAndFlow(t *testing.T) {
	var calls []string
	var namespaceBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/management/v1/bootstrap":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/management/v1/warehouse" && r.Method == http.MethodPost:
			// verify the storage profile is well-formed for aws
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "wh1", body["warehouse-name"])
			profile := body["storage-profile"].(map[string]any)
			assert.Equal(t, "s3", profile["type"])
			assert.Equal(t, false, profile["sts-enabled"])
			assert.Equal(t, false, profile["remote-signing-enabled"])
			// Cloud AWS (no endpoint): virtual-hosted addressing.
			assert.Equal(t, "aws", profile["flavor"])
			assert.Equal(t, false, profile["path-style-access"])
			assert.NotContains(t, profile, "endpoint")
			// key-prefix, not path-prefix.
			assert.Equal(t, "iceberg", profile["key-prefix"])
			assert.NotContains(t, profile, "path-prefix")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"warehouse-id":"wh-uuid-123"}`))
		case strings.HasPrefix(r.URL.Path, "/catalog/v1/") && strings.HasSuffix(r.URL.Path, "/namespaces"):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&namespaceBody))
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cfg, err := parseLakekeeperStorageConfig(awsBootstrapConfig())
	require.NoError(t, err)

	err = runLakekeeperBootstrap(context.Background(), srv.Client(), srv.URL, cfg)
	require.NoError(t, err)

	require.Equal(t, []string{
		"POST /management/v1/bootstrap",
		"POST /management/v1/warehouse",
		"POST /catalog/v1/wh-uuid-123/namespaces",
	}, calls)
	assert.Equal(t, []any{"default"}, namespaceBody["namespace"])
}

// TestRunLakekeeperBootstrap_AlreadyBootstrapped tolerates a conflict on
// bootstrap and on the warehouse (already exists), looking up the warehouse-id
// via GET, and tolerates a conflict on the namespace.
func TestRunLakekeeperBootstrap_AlreadyBootstrapped(t *testing.T) {
	var calls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/management/v1/bootstrap":
			// already bootstrapped
			w.WriteHeader(http.StatusConflict)
		case r.URL.Path == "/management/v1/warehouse" && r.Method == http.MethodPost:
			// already exists
			w.WriteHeader(http.StatusConflict)
		case r.URL.Path == "/management/v1/warehouse" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"warehouses":[{"id":"existing-wh","name":"wh1"}]}`))
		case strings.HasSuffix(r.URL.Path, "/namespaces"):
			w.WriteHeader(http.StatusConflict)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg, err := parseLakekeeperStorageConfig(awsBootstrapConfig())
	require.NoError(t, err)

	err = runLakekeeperBootstrap(context.Background(), srv.Client(), srv.URL, cfg)
	require.NoError(t, err)

	// The namespace must be created against the looked-up existing-wh id.
	require.Equal(t, []string{
		"POST /management/v1/bootstrap",
		"POST /management/v1/warehouse",
		"GET /management/v1/warehouse",
		"POST /catalog/v1/existing-wh/namespaces",
	}, calls)
}

// TestRunLakekeeperBootstrap_ServerErrorSurfaces confirms a genuine 5xx failure
// is surfaced (not swallowed), so an unbootstrapped warehouse blocks.
func TestRunLakekeeperBootstrap_ServerErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/management/v1/bootstrap" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	cfg, err := parseLakekeeperStorageConfig(awsBootstrapConfig())
	require.NoError(t, err)

	err = runLakekeeperBootstrap(context.Background(), srv.Client(), srv.URL, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create warehouse failed")
}

func TestBuildWarehouseRequestBody_CloudAWS(t *testing.T) {
	cfg, err := parseLakekeeperStorageConfig(awsBootstrapConfig())
	require.NoError(t, err)

	body, err := buildWarehouseRequestBody(cfg)
	require.NoError(t, err)
	profile := body["storage-profile"].(map[string]any)
	assert.Equal(t, "s3", profile["type"])
	assert.Equal(t, "aws", profile["flavor"])
	assert.Equal(t, false, profile["path-style-access"])
	assert.NotContains(t, profile, "endpoint")
	assert.Equal(t, "iceberg", profile["key-prefix"])
}

func TestBuildWarehouseRequestBody_S3CompatGCS(t *testing.T) {
	cfg, err := parseLakekeeperStorageConfig(gcsBootstrapConfig())
	require.NoError(t, err)

	body, err := buildWarehouseRequestBody(cfg)
	require.NoError(t, err)
	profile := body["storage-profile"].(map[string]any)
	assert.Equal(t, "s3", profile["type"])
	// endpoint present => s3-compat, path-style addressing.
	assert.Equal(t, "s3-compat", profile["flavor"])
	assert.Equal(t, true, profile["path-style-access"])
	assert.Equal(t, "https://storage.googleapis.com", profile["endpoint"])
	assert.Equal(t, "iceberg", profile["key-prefix"])
	// GCS HMAC creds are carried as S3 access-key credentials.
	credential := body["storage-credential"].(map[string]any)
	assert.Equal(t, "GOOG_ID", credential["aws-access-key-id"])
}

func TestBuildWarehouseRequestBody_Azure(t *testing.T) {
	cfg, err := parseLakekeeperStorageConfig(map[string]any{
		"provider":   "azure",
		"bucket":     "container1",
		"region":     "myaccount",
		"warehouse":  "wh1",
		"credential": `{"connection_string":"DefaultEndpointsProtocol=https;AccountName=x"}`,
	})
	require.NoError(t, err)

	body, err := buildWarehouseRequestBody(cfg)
	require.NoError(t, err)
	profile := body["storage-profile"].(map[string]any)
	assert.Equal(t, "adls", profile["type"])
	credential := body["storage-credential"].(map[string]any)
	assert.Equal(t, "DefaultEndpointsProtocol=https;AccountName=x", credential["connection-string"])
}
