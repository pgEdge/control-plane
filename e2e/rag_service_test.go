//go:build e2e_test

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// TestProvisionRAGService provisions a database with a single-host RAG service
// and verifies it reaches running state. Placeholder API keys are used so this
// test runs in CI without incurring LLM costs.
func TestProvisionRAGService(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "latest",
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs: "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-embed-key",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key",
								},
							},
						},
					},
				},
			},
		},
	})

	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 RAG service instance")

	si := db.ServiceInstances[0]
	assert.Equal(t, "rag", si.ServiceID)
	assert.Equal(t, string(host1), si.HostID)

	t.Log("Waiting for RAG service to be running")
	waitForServiceRunning(ctx, t, db, si.ServiceInstanceID, 8*time.Minute)
}

// TestRAGPipelineQuery provisions a RAG service with real API keys, inserts a
// document with a pre-computed embedding, queries the pipeline, and verifies a
// non-empty answer is returned.
//
// Skipped unless E2E_OPENAI_API_KEY and E2E_ANTHROPIC_API_KEY are set.
func TestRAGPipelineQuery(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	openAIKey := getEnvOrSkip(t, "E2E_OPENAI_API_KEY")
	anthropicKey := getEnvOrSkip(t, "E2E_ANTHROPIC_API_KEY")

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_pipeline_query",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "latest",
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs:   "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  openAIKey,
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  anthropicKey,
								},
							},
						},
					},
				},
			},
		},
	})

	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 RAG service instance")

	si := db.ServiceInstances[0]
	assert.Equal(t, "rag", si.ServiceID)
	assert.Equal(t, string(host1), si.HostID)

	t.Log("Waiting for RAG service to be running")
	si = waitForServiceRunning(ctx, t, db, si.ServiceInstanceID, 8*time.Minute)

	ragURL := ragServiceURL(t, si)
	t.Logf("RAG service URL: %s", ragURL)

	t.Log("Setting up docs table and inserting test document")
	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n1"), WithRole("master")),
		Username: "admin",
		Password: "testpassword",
	}, t, func(conn *pgx.Conn) {
		setupRAGSchema(ctx, t, conn)
		insertRAGDocument(ctx, t, conn)
	})

	t.Log("Querying RAG pipeline")
	answer := waitForNonEmptyRAGAnswer(ctx, t, ragURL, "What is pgEdge?", 2*time.Minute)

	require.NotEmpty(t, answer, "RAG pipeline should return a non-empty answer")
	t.Logf("RAG answer: %s", answer)
}

// ragServiceURL builds the base HTTP URL for the RAG service instance.
func ragServiceURL(t testing.TB, si *controlplane.ServiceInstance) string {
	t.Helper()

	require.NotNil(t, si.Status, "service instance status must be populated")
	require.NotEmpty(t, si.Status.Addresses, "service instance must have at least one address")
	require.NotEmpty(t, si.Status.Ports, "service instance must have port mappings")

	var hostPort int
	for _, p := range si.Status.Ports {
		if p.Name == "http" && p.HostPort != nil {
			hostPort = *p.HostPort
			break
		}
	}
	require.NotZero(t, hostPort, "http port mapping not found in service instance status")

	return fmt.Sprintf("http://%s:%d", si.Status.Addresses[0], hostPort)
}

// waitForServiceRunning polls the database until the named service instance
// reaches state "running" or the deadline is exceeded.
func waitForServiceRunning(
	ctx context.Context,
	t testing.TB,
	db *DatabaseFixture,
	serviceInstanceID string,
	maxWait time.Duration,
) *controlplane.ServiceInstance {
	t.Helper()

	deadline := time.Now().Add(maxWait)
	var lastRunning *controlplane.ServiceInstance
	for time.Now().Before(deadline) {
		require.NoError(t, db.Refresh(ctx), "failed to refresh database")
		for _, si := range db.ServiceInstances {
			if si.ServiceInstanceID != serviceInstanceID {
				continue
			}
			if si.State == "running" {
				// State is flipped to "running" deterministically by the
				// deploying resource as soon as the container starts, before
				// ServiceInstanceMonitor's async health-check loop (which
				// runs on its own ~10s cycle) has populated Status. Keep
				// polling until Status/ImageVersion actually shows up so
				// callers can rely on it being present rather than silently
				// skipping checks that depend on it.
				if si.Status != nil && si.Status.ImageVersion != nil {
					return si
				}
				lastRunning = si
			}
			if si.State == "failed" {
				var errMsg string
				if si.Error != nil {
					errMsg = *si.Error
				}
				t.Fatalf("service instance %s entered failed state: %s", serviceInstanceID, errMsg)
			}
		}
		time.Sleep(5 * time.Second)
	}

	if lastRunning != nil {
		t.Fatalf("service instance %s reached running state but status/image version was never populated within %s", serviceInstanceID, maxWait)
	}
	t.Fatalf("service instance %s did not reach running state within %s", serviceInstanceID, maxWait)
	return nil
}

// setupRAGSchema creates the pgvector extension and the docs table.
func setupRAGSchema(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	t.Helper()

	for _, stmt := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS docs (
			id      SERIAL PRIMARY KEY,
			content TEXT NOT NULL,
			embedding vector(1536)
		)`,
	} {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err, "failed to execute: %s", stmt)
	}
}

// insertRAGDocument inserts a single test document with a pre-computed
// 1536-dimension zero vector (sufficient for smoke-testing the pipeline).
func insertRAGDocument(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	t.Helper()

	// Build a 1536-dimension zero vector string: '[0,0,...,0]'
	buf := make([]byte, 0, 1536*3)
	buf = append(buf, '[')
	for i := 0; i < 1536; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '0')
	}
	buf = append(buf, ']')

	_, err := conn.Exec(ctx,
		`INSERT INTO docs (content, embedding) VALUES ($1, $2)`,
		"pgEdge is a distributed Postgres platform that supports multi-active deployments.",
		string(buf),
	)
	require.NoError(t, err, "failed to insert test document")
}

// queryRAGPipeline sends a query to the RAG service and returns the answer.
func queryRAGPipeline(ctx context.Context, t testing.TB, baseURL, query string) (string, error) {
	t.Helper()

	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return "", fmt.Errorf("marshal query payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/pipelines/default",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Answer, nil
}

// TestProvisionMultiHostRAGService tests provisioning a RAG service on multiple
// hosts (PLAT-493). Each host must receive its own service instance.
func TestProvisionMultiHostRAGService(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 3, "requires at least 3 hosts")
	host1, host2, host3 := hosts[0], hosts[1], hosts[2]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service on 3 hosts")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_multihost",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
				{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(host2)}},
				{Name: "n3", HostIds: []controlplane.Identifier{controlplane.Identifier(host3)}},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "latest",
					HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
						controlplane.Identifier(host3),
					},
					Port:      pointerTo(0),
					ConnectAs: "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-embed-key",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key",
								},
							},
						},
					},
				},
			},
		},
	})

	require.NotNil(t, db.ServiceInstances)
	require.Len(t, db.ServiceInstances, 3, "Expected one RAG instance per host")

	// Verify each host has its own independent instance.
	hostsWithService := make(map[string]bool)
	for _, si := range db.ServiceInstances {
		assert.Equal(t, "rag", si.ServiceID)
		assert.NotEmpty(t, si.ServiceInstanceID)
		hostsWithService[si.HostID] = true
		t.Logf("RAG instance on host %s: %s (state: %s)", si.HostID, si.ServiceInstanceID, si.State)
	}

	assert.True(t, hostsWithService[host1], "host-1 should have a RAG instance")
	assert.True(t, hostsWithService[host2], "host-2 should have a RAG instance")
	assert.True(t, hostsWithService[host3], "host-3 should have a RAG instance")

	t.Log("Multi-host RAG service provisioning test completed successfully")
}

// TestAddRAGServiceToExistingDatabase tests adding a RAG service to a database
// that was initially created without any services.
func TestAddRAGServiceToExistingDatabase(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database without services")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_add_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
			},
		},
	})

	assert.Empty(t, db.ServiceInstances, "Should have no service instances initially")

	t.Log("Adding RAG service to existing database")

	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_add_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "latest",
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs:   "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-embed-key",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key",
								},
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err, "Failed to update database with RAG service")

	require.Len(t, db.ServiceInstances, 1, "Expected 1 RAG service instance after update")
	si := db.ServiceInstances[0]
	assert.Equal(t, "rag", si.ServiceID)
	assert.Equal(t, string(host1), si.HostID)

	t.Logf("RAG service instance added: %s (state: %s)", si.ServiceInstanceID, si.State)
	t.Log("Add RAG service to existing database test completed successfully")
}

// TestProvisionRAGServiceUnsupportedVersion verifies that using an unregistered
// RAG image version causes the workflow to fail and the database to enter
// "failed" state (mirrors TestProvisionMCPServiceUnsupportedVersion).
func TestProvisionRAGServiceUnsupportedVersion(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service using unsupported version")

	createResp, err := fixture.Client.CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_unsupported_ver",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1)}},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "99.99.99", // Valid semver but not registered
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs:   "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-key",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key",
								},
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err, "CreateDatabase API call should succeed")
	require.NotNil(t, createResp.Task)
	require.NotNil(t, createResp.Database)

	dbID := createResp.Database.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()

		t.Logf("cleaning up database %s", dbID)
		resp, err := fixture.Client.DeleteDatabase(cleanupCtx, &controlplane.DeleteDatabasePayload{
			DatabaseID: dbID,
			Force:      true,
		})
		if err != nil {
			if !errors.Is(err, client.ErrNotFound) {
				t.Logf("failed to cleanup database %s: %s", dbID, err)
			}
			return
		}
		_, _ = fixture.Client.WaitForDatabaseTask(cleanupCtx, &controlplane.GetDatabaseTaskPayload{
			DatabaseID: dbID,
			TaskID:     resp.Task.TaskID,
		})
	})

	task, err := fixture.Client.WaitForDatabaseTask(ctx, &controlplane.GetDatabaseTaskPayload{
		DatabaseID: dbID,
		TaskID:     createResp.Task.TaskID,
	})
	require.NoError(t, err)
	assert.Equal(t, client.TaskStatusFailed, task.Status, "Task should have failed")
	require.NotNil(t, task.Error)
	assert.Contains(t, *task.Error, "unsupported version", "Task error should mention unsupported version")
	t.Logf("Task failed as expected: %s", *task.Error)

	db, err := fixture.Client.GetDatabase(ctx, &controlplane.GetDatabasePayload{DatabaseID: dbID})
	require.NoError(t, err)
	assert.Equal(t, "failed", db.State, "Database should be in failed state")

	t.Log("RAG unsupported version test completed successfully")
}

// waitForNonEmptyRAGAnswer polls the RAG pipeline until it returns a non-empty
// answer or the deadline is exceeded. This avoids a fixed sleep after document
// ingestion, which is nondeterministic under load.
func waitForNonEmptyRAGAnswer(ctx context.Context, t testing.TB, baseURL, query string, maxWait time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(maxWait)
	var lastErr error
	for time.Now().Before(deadline) {
		answer, err := queryRAGPipeline(ctx, t, baseURL, query)
		if err != nil {
			lastErr = err
			time.Sleep(3 * time.Second)
			continue
		}
		if answer != "" {
			return answer
		}
		time.Sleep(3 * time.Second)
	}

	t.Fatalf("RAG answer did not become non-empty within %s (last error: %v)", maxWait, lastErr)
	return ""
}

// getEnvOrSkip returns the value of the environment variable, or skips the
// test if the variable is not set.
func getEnvOrSkip(t testing.TB, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping: %s not set", key)
	}
	return v
}

// TestProvisionRAGServicePinnedVersion200 provisions a RAG service pinned
// explicitly to "2.0.0" (the current default, but pinning explicitly still
// exercises manifest resolution end to end) and exercises the v2.0.0 config
// fields: allow_include_sources and an optional voyage rerank stage.
// Placeholder API keys are used so this runs without incurring LLM costs.
func TestProvisionRAGServicePinnedVersion200(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service pinned to 2.0.0")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_pinned_2_0_0",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "2.0.0",
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs:   "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-embed-key",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key",
								},
								"allow_include_sources": true,
								"rerank": map[string]any{
									"provider": "voyage",
									"model":    "rerank-2",
									"api_key":  "pa-test-voyage-rerank-key",
									"top_k":    5,
								},
							},
						},
					},
				},
			},
		},
	})

	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 RAG service instance")

	si := db.ServiceInstances[0]
	assert.Equal(t, "rag", si.ServiceID)

	t.Log("Waiting for RAG service to be running")
	si = waitForServiceRunning(ctx, t, db, si.ServiceInstanceID, 8*time.Minute)
	require.NotNil(t, si.Status, "service instance status should be populated once running")
	require.NotNil(t, si.Status.ImageVersion, "service instance image version should be populated once running")
	assert.Contains(t, *si.Status.ImageVersion, "2.0.0", "running container should use the pinned 2.0.0 image")
}

// TestUpdateRAGServiceVersion is the PLAT-715 regression test for RAG: it
// fetches a database via GetDatabase (which strips embedding_llm/rag_llm
// api_key values from the returned pipelines config), mutates only the
// service's Version in that fetched spec, and feeds it straight back into
// UpdateDatabase without resupplying the stripped secrets. Before the
// PLAT-715 fix this always 400s, because RAG config validation unconditionally
// required api_key back for non-ollama providers.
func TestUpdateRAGServiceVersion(t *testing.T) {
	t.Parallel()

	fixture.SkipIfServicesUnsupported(t)

	hosts := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hosts), 1, "requires at least 1 host")
	host1 := hosts[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("Creating database with RAG service pinned to 1.0.0")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_rag_version_update",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "rag_user",
					Password:   pointerTo("ragpassword"),
					Attributes: []string{"LOGIN"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "rag",
					ServiceType: "rag",
					Version:     "1.0.0",
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Port:        pointerTo(0),
					ConnectAs:   "rag_user",
					Config: map[string]any{
						"pipelines": []any{
							map[string]any{
								"name": "default",
								"tables": []any{
									map[string]any{
										"table":         "docs",
										"text_column":   "content",
										"vector_column": "embedding",
									},
								},
								"embedding_llm": map[string]any{
									"provider": "openai",
									"model":    "text-embedding-3-small",
									"api_key":  "sk-test-embed-key-version-update",
								},
								"rag_llm": map[string]any{
									"provider": "anthropic",
									"model":    "claude-haiku-4-5-20251001",
									"api_key":  "sk-ant-test-key-version-update",
								},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, db.ServiceInstances, 1, "Expected 1 RAG service instance")
	waitForServiceRunning(ctx, t, db, db.ServiceInstances[0].ServiceInstanceID, 8*time.Minute)

	t.Log("Fetching the database (this strips embedding_llm/rag_llm api_key from the returned config)")
	require.NoError(t, db.Refresh(ctx), "failed to refresh database")

	var ragSvc *controlplane.ServiceSpec
	for _, svc := range db.Spec.Services {
		if svc.ServiceID == "rag" {
			ragSvc = svc
		}
	}
	require.NotNil(t, ragSvc, "rag service should be present in the fetched spec")
	pipelines, ok := ragSvc.Config["pipelines"].([]any)
	require.True(t, ok && len(pipelines) == 1, "fetched config should have 1 pipeline")
	pipeline := pipelines[0].(map[string]any)
	_, hasEmbedKey := pipeline["embedding_llm"].(map[string]any)["api_key"]
	_, hasRAGKey := pipeline["rag_llm"].(map[string]any)["api_key"]
	require.False(t, hasEmbedKey, "embedding_llm.api_key should have been stripped from the GET response")
	require.False(t, hasRAGKey, "rag_llm.api_key should have been stripped from the GET response")

	t.Log("Bumping only the service version in the fetched spec, then feeding it back into UpdateDatabase")
	ragSvc.Version = "2.0.0"

	err := db.Update(ctx, UpdateOptions{Spec: db.Spec})
	require.NoError(t, err, "UpdateDatabase should succeed even though the fetched spec never had api_key values to resupply")

	require.Len(t, db.ServiceInstances, 1, "Should still have 1 service instance")
	si := waitForServiceRunning(ctx, t, db, db.ServiceInstances[0].ServiceInstanceID, 8*time.Minute)
	require.NotNil(t, si.Status, "service instance status should be populated once running")
	require.NotNil(t, si.Status.ImageVersion, "service instance image version should be populated once running")
	assert.Contains(t, *si.Status.ImageVersion, "2.0.0", "service should be running the new pinned version after update")
}
