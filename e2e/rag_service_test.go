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
	for time.Now().Before(deadline) {
		require.NoError(t, db.Refresh(ctx), "failed to refresh database")
		for _, si := range db.ServiceInstances {
			if si.ServiceInstanceID == serviceInstanceID && si.State == "running" {
				return si
			}
		}
		time.Sleep(5 * time.Second)
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
func queryRAGPipeline(ctx context.Context, t testing.TB, baseURL, query string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/pipelines/default",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status %d: %s", resp.StatusCode, string(respBody))

	var result struct {
		Answer string `json:"answer"`
	}
	require.NoError(t, json.Unmarshal(respBody, &result), "failed to parse RAG response")

	return result.Answer
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
					Port: pointerTo(0),
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
	for time.Now().Before(deadline) {
		answer := queryRAGPipeline(ctx, t, baseURL, query)
		if answer != "" {
			return answer
		}
		time.Sleep(3 * time.Second)
	}

	t.Fatalf("RAG answer did not become non-empty within %s", maxWait)
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
