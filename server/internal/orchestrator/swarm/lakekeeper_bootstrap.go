package swarm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// lakekeeperStorageConfig holds the object-store coordinates and parsed
// credential needed to build a Lakekeeper warehouse storage profile. It is
// derived from the lakekeeper ServiceSpec.Config supplied by saas.
//
// The Credential map is provider-specific and MUST NOT be logged: it carries
// secret access keys / connection strings.
type lakekeeperStorageConfig struct {
	Provider   string // "aws" | "azure" | "gcs"
	Bucket     string
	Region     string
	Endpoint   string
	Warehouse  string
	PathPrefix string
	// Credential is the parsed provider-specific credential JSON.
	// aws: {"access_key_id","secret_access_key"}
	// azure: {"connection_string"}
	// gcs: {"hmac_access_id","hmac_secret"}
	Credential map[string]string
}

// lakekeeperBootstrapHTTPTimeout bounds each individual REST call to the
// Lakekeeper management/catalog API.
const lakekeeperBootstrapHTTPTimeout = 30 * time.Second

// parseLakekeeperStorageConfig extracts the object-store configuration and
// parses the provider-specific credential JSON from a lakekeeper
// ServiceSpec.Config map. It fails loud when a required key is missing so that
// a database is never left with an unbootstrapped (broken) warehouse.
//
// Some of these keys are a saas follow-up; an absent required key therefore
// yields a clear, actionable error rather than a silent misconfiguration.
func parseLakekeeperStorageConfig(config map[string]any) (*lakekeeperStorageConfig, error) {
	get := func(key string) string {
		v, _ := config[key].(string)
		return strings.TrimSpace(v)
	}

	provider := get("provider")
	switch provider {
	case "aws", "azure", "gcs":
	case "":
		return nil, fmt.Errorf("lakekeeper bootstrap: provider is required in config (one of aws, azure, gcs)")
	default:
		return nil, fmt.Errorf("lakekeeper bootstrap: unsupported provider %q (expected one of aws, azure, gcs)", provider)
	}

	warehouse := get("warehouse")
	if warehouse == "" {
		return nil, fmt.Errorf("lakekeeper bootstrap: warehouse is required in config")
	}

	credRaw := get("credential")
	if credRaw == "" {
		return nil, fmt.Errorf("lakekeeper bootstrap: credential is required in config")
	}
	var cred map[string]string
	if err := json.Unmarshal([]byte(credRaw), &cred); err != nil {
		// Deliberately do not include credRaw in the error: it is a secret.
		return nil, fmt.Errorf("lakekeeper bootstrap: credential is not valid JSON")
	}

	cfg := &lakekeeperStorageConfig{
		Provider:   provider,
		Bucket:     get("bucket"),
		Region:     get("region"),
		Endpoint:   get("endpoint"),
		Warehouse:  warehouse,
		PathPrefix: get("path_prefix"),
		Credential: cred,
	}

	// Provider-specific required fields.
	switch provider {
	case "aws", "gcs":
		if cfg.Bucket == "" {
			return nil, fmt.Errorf("lakekeeper bootstrap: bucket is required in config for provider %q", provider)
		}
		if provider == "aws" {
			if cred["access_key_id"] == "" || cred["secret_access_key"] == "" {
				return nil, fmt.Errorf("lakekeeper bootstrap: aws credential must contain access_key_id and secret_access_key")
			}
		} else { // gcs
			if cred["hmac_access_id"] == "" || cred["hmac_secret"] == "" {
				return nil, fmt.Errorf("lakekeeper bootstrap: gcs credential must contain hmac_access_id and hmac_secret")
			}
		}
	case "azure":
		if cfg.Bucket == "" {
			return nil, fmt.Errorf("lakekeeper bootstrap: bucket (container/account) is required in config for provider %q", provider)
		}
		if cred["connection_string"] == "" {
			return nil, fmt.Errorf("lakekeeper bootstrap: azure credential must contain connection_string")
		}
	}

	return cfg, nil
}

// buildWarehouseRequestBody builds the POST /management/v1/warehouse request
// body for the configured provider.
//
// For aws and gcs an s3 storage profile is used (gcs is reached through its
// S3-compatible HMAC interface). For azure an adls profile is used.
func buildWarehouseRequestBody(cfg *lakekeeperStorageConfig) (map[string]any, error) {
	switch cfg.Provider {
	case "aws", "gcs":
		profile := map[string]any{
			"type":                   "s3",
			"bucket":                 cfg.Bucket,
			"region":                 cfg.Region,
			"sts-enabled":            false,
			"remote-signing-enabled": false,
		}
		// Endpoint presence is the discriminator between native cloud AWS and
		// an S3-compatible store (GCS via storage.googleapis.com, SeaweedFS,
		// etc.). Cloud AWS must use virtual-hosted addressing (flavor "aws",
		// path-style-access false); path-style fails on any Region launched
		// after 2019. An S3-compatible endpoint uses flavor "s3-compat" with
		// path-style addressing. (ColdFront docs/object_store.md, installation.md.)
		if cfg.Endpoint != "" {
			profile["endpoint"] = cfg.Endpoint
			profile["flavor"] = "s3-compat"
			profile["path-style-access"] = true
		} else {
			profile["flavor"] = "aws"
			profile["path-style-access"] = false
		}
		if cfg.PathPrefix != "" {
			profile["key-prefix"] = cfg.PathPrefix
		}

		var credential map[string]any
		if cfg.Provider == "aws" {
			credential = map[string]any{
				"type":                  "s3",
				"credential-type":       "access-key",
				"aws-access-key-id":     cfg.Credential["access_key_id"],
				"aws-secret-access-key": cfg.Credential["secret_access_key"],
			}
		} else { // gcs via S3-compatible HMAC
			credential = map[string]any{
				"type":                  "s3",
				"credential-type":       "access-key",
				"aws-access-key-id":     cfg.Credential["hmac_access_id"],
				"aws-secret-access-key": cfg.Credential["hmac_secret"],
			}
		}

		return map[string]any{
			"warehouse-name":     cfg.Warehouse,
			"storage-profile":    profile,
			"storage-credential": credential,
		}, nil
	case "azure":
		profile := map[string]any{
			"type":         "adls",
			"account-name": cfg.Region, // account name carried in region for azure
			"filesystem":   cfg.Bucket, // container
		}
		if cfg.Endpoint != "" {
			profile["endpoint-suffix"] = cfg.Endpoint
		}
		if cfg.PathPrefix != "" {
			profile["path-prefix"] = cfg.PathPrefix
		}
		credential := map[string]any{
			"type":              "az",
			"credential-type":   "shared-access-key",
			"connection-string": cfg.Credential["connection_string"],
		}
		return map[string]any{
			"warehouse-name":     cfg.Warehouse,
			"storage-profile":    profile,
			"storage-credential": credential,
		}, nil
	default:
		return nil, fmt.Errorf("lakekeeper bootstrap: unsupported provider %q", cfg.Provider)
	}
}

// runLakekeeperBootstrap performs the Lakekeeper REST bootstrap sequence
// against baseURL (the Lakekeeper service, e.g. http://<service>:8181) using
// the supplied HTTP client, in order and idempotently:
//
//  1. POST /management/v1/bootstrap {"accept-terms-of-use": true}
//  2. POST /management/v1/warehouse (storage profile + credential)
//  3. extract warehouse-id from the warehouse response (or look it up on
//     conflict)
//  4. POST /catalog/v1/{warehouse-id}/namespaces {"namespace":["default"]}
//
// Already-bootstrapped / already-exists responses (4xx conflicts) are treated
// as success so the sequence is safe to re-run.
func runLakekeeperBootstrap(ctx context.Context, client *http.Client, baseURL string, cfg *lakekeeperStorageConfig) error {
	baseURL = strings.TrimRight(baseURL, "/")

	// Step 1: bootstrap (accept terms). Idempotent: an already-bootstrapped
	// server returns a client error (400/409) which we tolerate.
	if err := lakekeeperPost(ctx, client, baseURL+"/management/v1/bootstrap",
		map[string]any{"accept-terms-of-use": true}, nil, true); err != nil {
		return fmt.Errorf("lakekeeper bootstrap: accept-terms failed: %w", err)
	}

	// Step 2: create the warehouse.
	body, err := buildWarehouseRequestBody(cfg)
	if err != nil {
		return err
	}
	var whResp struct {
		WarehouseID string `json:"warehouse-id"`
		ID          string `json:"id"`
	}
	created, err := lakekeeperPostDecode(ctx, client, baseURL+"/management/v1/warehouse", body, &whResp)
	if err != nil {
		return fmt.Errorf("lakekeeper bootstrap: create warehouse failed: %w", err)
	}

	// Step 3: resolve the warehouse id. On a fresh create it is in the
	// response; on conflict (already exists) we look it up by name.
	warehouseID := firstNonEmpty(whResp.WarehouseID, whResp.ID)
	if !created || warehouseID == "" {
		warehouseID, err = lookupLakekeeperWarehouseID(ctx, client, baseURL, cfg.Warehouse)
		if err != nil {
			return fmt.Errorf("lakekeeper bootstrap: resolve warehouse id: %w", err)
		}
	}
	if warehouseID == "" {
		return fmt.Errorf("lakekeeper bootstrap: could not determine warehouse id for warehouse %q", cfg.Warehouse)
	}

	// Step 4: create the default namespace (required for compaction to be able
	// to create tables). Idempotent: already-exists is tolerated.
	nsURL := fmt.Sprintf("%s/catalog/v1/%s/namespaces", baseURL, warehouseID)
	if err := lakekeeperPost(ctx, client, nsURL,
		map[string]any{"namespace": []string{"default"}}, nil, true); err != nil {
		return fmt.Errorf("lakekeeper bootstrap: create default namespace failed: %w", err)
	}

	return nil
}

// lookupLakekeeperWarehouseID lists warehouses and returns the id of the one
// matching name. Used when the warehouse already exists (create returned a
// conflict).
func lookupLakekeeperWarehouseID(ctx context.Context, client *http.Client, baseURL, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/management/v1/warehouse", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("list warehouses returned status %d", resp.StatusCode)
	}
	var listResp struct {
		Warehouses []struct {
			WarehouseID string `json:"warehouse-id"`
			ID          string `json:"id"`
			Name        string `json:"name"`
		} `json:"warehouses"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		return "", fmt.Errorf("decode warehouse list: %w", err)
	}
	for _, w := range listResp.Warehouses {
		if w.Name == name {
			// Prefer the canonical warehouse-id field; fall back to the
			// deprecated id, matching the create-response handling.
			return firstNonEmpty(w.WarehouseID, w.ID), nil
		}
	}
	return "", nil
}

// lakekeeperPost issues a POST with a JSON body. When out is non-nil the
// response body is decoded into it. When tolerateConflict is true a 4xx
// response is treated as success (idempotent already-exists), otherwise any
// status >= 300 is an error.
func lakekeeperPost(ctx context.Context, client *http.Client, url string, body any, out any, tolerateConflict bool) error {
	_, err := lakekeeperDo(ctx, client, url, body, out, tolerateConflict)
	return err
}

// lakekeeperPostDecode issues a POST that always tolerates conflicts and
// decodes a success response into out. It returns whether the resource was
// newly created (2xx) as opposed to already existing (4xx conflict).
func lakekeeperPostDecode(ctx context.Context, client *http.Client, url string, body any, out any) (created bool, err error) {
	return lakekeeperDo(ctx, client, url, body, out, true)
}

func lakekeeperDo(ctx context.Context, client *http.Client, url string, body any, out any, tolerateConflict bool) (created bool, err error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return false, fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return true, fmt.Errorf("decode response: %w", err)
			}
		}
		return true, nil
	case tolerateConflict && resp.StatusCode >= 400 && resp.StatusCode < 500:
		// Already bootstrapped / already exists — idempotent success.
		return false, nil
	default:
		return false, fmt.Errorf("POST %s returned status %d: %s", url, resp.StatusCode, string(data))
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
