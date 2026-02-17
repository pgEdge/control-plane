//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProvisionMCPService tests provisioning an MCP server service with a database.
func TestProvisionMCPService(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with MCP service")

	// Create database with MCP service in spec
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_mcp_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					//Version:     "1.0.0",
					Version: "latest",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
					Config: map[string]any{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test-key-12345",
					},
				},
			},
		},
	})

	t.Log("Database created, verifying service instances")

	// Verify service instances exist
	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 service instance")

	serviceInstance := db.ServiceInstances[0]

	// Verify service instance properties
	assert.Equal(t, "mcp-server", serviceInstance.ServiceID, "Service ID should match")
	assert.Equal(t, string(host1), serviceInstance.HostID, "Host ID should match")
	assert.NotEmpty(t, serviceInstance.ServiceInstanceID, "Service instance ID should not be empty")

	// Verify service instance state
	// Note: State might be "creating" or "running" depending on timing
	validStates := []string{"creating", "running"}
	assert.Contains(t, validStates, serviceInstance.State, "Service instance should be in a valid state")

	t.Logf("Service instance created: %s (state: %s)", serviceInstance.ServiceInstanceID, serviceInstance.State)

	// Wait for service to be running if it's still creating
	if serviceInstance.State == "creating" {
		t.Log("Service is still creating, waiting for it to become running...")

		maxWait := 5 * time.Minute
		pollInterval := 5 * time.Second
		deadline := time.Now().Add(maxWait)

		for time.Now().Before(deadline) {
			err := db.Refresh(ctx)
			require.NoError(t, err, "Failed to refresh database")

			if len(db.ServiceInstances) > 0 && db.ServiceInstances[0].State == "running" {
				t.Log("Service is now running")
				break
			}

			time.Sleep(pollInterval)
		}

		// Verify final state
		require.Len(t, db.ServiceInstances, 1, "Service instance should still exist")
		assert.Equal(t, "running", db.ServiceInstances[0].State, "Service should be running after wait")
	}

	// Verify service instance status/connection info exists
	serviceInstance = db.ServiceInstances[0]
	if serviceInstance.Status != nil {
		t.Log("Verifying service instance connection info")

		// Verify basic connection info exists
		assert.NotNil(t, serviceInstance.Status.Hostname, "Hostname should be set")
		assert.NotNil(t, serviceInstance.Status.Ipv4Address, "IPv4 address should be set")

		if serviceInstance.Status.Hostname != nil {
			t.Logf("Service hostname: %s", *serviceInstance.Status.Hostname)
		}
		if serviceInstance.Status.Ipv4Address != nil {
			t.Logf("Service IPv4 address: %s", *serviceInstance.Status.Ipv4Address)
		}

		// Verify ports are configured
		if len(serviceInstance.Status.Ports) > 0 {
			t.Logf("Service has %d ports configured", len(serviceInstance.Status.Ports))
			for _, port := range serviceInstance.Status.Ports {
				t.Logf("  - %s: container_port=%v", port.Name, port.ContainerPort)
			}

			// Verify HTTP port (8080) is exposed
			foundHTTPPort := false
			for _, port := range serviceInstance.Status.Ports {
				if port.Name == "http" && port.ContainerPort != nil && *port.ContainerPort == 8080 {
					foundHTTPPort = true
					break
				}
			}
			assert.True(t, foundHTTPPort, "HTTP port (8080) should be configured")
		}
	} else {
		t.Log("Service instance status not yet populated (this may be expected if container is still starting)")
	}

	t.Log("MCP service provisioning test completed successfully")
}

// TestProvisionMultiHostMCPService tests provisioning MCP service on multiple hosts.
func TestProvisionMultiHostMCPService(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with MCP service on multiple hosts")

	// Create database with MCP service on 3 hosts
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_mcp_multihost",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					//Version:     "1.0.0",
					Version: "latest",
					HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
						controlplane.Identifier(host3),
					},
					Config: map[string]any{
						"llm_provider":   "openai",
						"llm_model":      "gpt-4",
						"openai_api_key": "sk-test-key-67890",
					},
				},
			},
		},
	})

	t.Log("Database created, verifying service instances on all hosts")

	// Verify service instances exist for all hosts
	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 3, "Expected 3 service instances (one per host)")

	// Track which hosts have service instances
	hostsWithServices := make(map[string]bool)
	for _, si := range db.ServiceInstances {
		hostsWithServices[si.HostID] = true

		// Verify basic properties
		assert.Equal(t, "mcp-server", si.ServiceID, "Service ID should match")
		assert.NotEmpty(t, si.ServiceInstanceID, "Service instance ID should not be empty")

		t.Logf("Service instance on host %s: %s (state: %s)", si.HostID, si.ServiceInstanceID, si.State)
	}

	// Verify all three hosts have service instances
	assert.True(t, hostsWithServices[host1], "Host 1 should have a service instance")
	assert.True(t, hostsWithServices[host2], "Host 2 should have a service instance")
	assert.True(t, hostsWithServices[host3], "Host 3 should have a service instance")

	t.Log("Multi-host MCP service provisioning test completed successfully")
}

// TestUpdateDatabaseAddService tests adding a service to an existing database.
func TestUpdateDatabaseAddService(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database without services")

	// Create database without services
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_add_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
		},
	})

	// Verify no service instances initially
	assert.Empty(t, db.ServiceInstances, "Should have no service instances initially")

	t.Log("Adding MCP service to existing database")

	// Update database to add service
	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_add_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					//Version:     "1.0.0",
					Version: "latest",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host2)},
					Config: map[string]any{
						"llm_provider": "ollama",
						"llm_model":    "llama2",
						"ollama_url":   "http://localhost:11434",
					},
				},
			},
		},
	})
	require.NoError(t, err, "Failed to update database")

	t.Log("Database updated, verifying service instance was added")

	// Verify service instance was created
	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 service instance after update")

	serviceInstance := db.ServiceInstances[0]
	assert.Equal(t, "mcp-server", serviceInstance.ServiceID, "Service ID should match")
	assert.Equal(t, string(host2), serviceInstance.HostID, "Host ID should match")

	t.Logf("Service instance added: %s (state: %s)", serviceInstance.ServiceInstanceID, serviceInstance.State)

	t.Log("Add service to existing database test completed successfully")
}

// TestProvisionMCPServiceUnsupportedVersion tests that database creation succeeds
// even when service provisioning fails due to an unsupported image version.
// Version "99.99.99" passes API validation (semver pattern) but is not registered
// in ServiceVersions, so GenerateServiceInstanceResources fails. The database
// should still become available and Postgres should be accessible.
func TestProvisionMCPServiceUnsupportedVersion(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with MCP service using unsupported version")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_mcp_unsupported_ver",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "99.99.99", // Valid semver but not registered in ServiceVersions
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Config: map[string]any{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test-key-12345",
					},
				},
			},
		},
	})

	t.Log("Database created, verifying database is available despite service failure")

	// Database should be available even though service provisioning failed
	assert.Equal(t, "available", db.State, "Database should be available despite service provisioning failure")

	// Verify Postgres instances exist and are accessible
	require.NotEmpty(t, db.Instances, "Database should have at least one Postgres instance")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  WithRole("primary"),
		Username: "admin",
		Password: "testpassword",
	}, t, func(conn *pgx.Conn) {
		var result int
		row := conn.QueryRow(ctx, "SELECT 1")
		require.NoError(t, row.Scan(&result))
		assert.Equal(t, 1, result)
		t.Log("Postgres is accessible despite service provisioning failure")
	})

	t.Log("Unsupported version test completed successfully")
}

// TestProvisionMCPServiceRecovery tests that a failed service can be recovered
// by updating the database with a corrected service version. The sequence is:
//  1. Create database with an unsupported service version (provisioning fails)
//  2. Verify database is available and Postgres is accessible
//  3. Update database with a corrected service version
//  4. Verify the service instance is created and transitions to running
func TestProvisionMCPServiceRecovery(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with MCP service using unsupported version")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_mcp_recovery",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "99.99.99", // Unsupported version - service provisioning will fail
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Config: map[string]any{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test-key-12345",
					},
				},
			},
		},
	})

	// Database should be available despite service failure
	assert.Equal(t, "available", db.State, "Database should be available despite service provisioning failure")
	t.Log("Database available, now updating with corrected service version")

	// Update database with corrected service version
	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_mcp_recovery",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "latest", // Corrected version
					HostIds:     []controlplane.Identifier{controlplane.Identifier(host1)},
					Config: map[string]any{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test-key-12345",
					},
				},
			},
		},
	})
	require.NoError(t, err, "Failed to update database with corrected service version")

	t.Log("Database updated, verifying service instance recovered")

	// Database should still be available
	assert.Equal(t, "available", db.State, "Database should remain available after update")

	// Service instance should now exist
	require.NotNil(t, db.ServiceInstances, "ServiceInstances should not be nil after recovery")
	require.Len(t, db.ServiceInstances, 1, "Expected 1 service instance after recovery")

	serviceInstance := db.ServiceInstances[0]
	assert.Equal(t, "mcp-server", serviceInstance.ServiceID, "Service ID should match")
	assert.Equal(t, host1, serviceInstance.HostID, "Host ID should match")

	t.Logf("Service instance created: %s (state: %s)", serviceInstance.ServiceInstanceID, serviceInstance.State)

	// Wait for service to become running if it's still creating
	if serviceInstance.State != "running" {
		t.Log("Service is not yet running, waiting...")

		maxWait := 5 * time.Minute
		pollInterval := 5 * time.Second
		deadline := time.Now().Add(maxWait)

		for time.Now().Before(deadline) {
			err := db.Refresh(ctx)
			require.NoError(t, err, "Failed to refresh database")

			if len(db.ServiceInstances) > 0 && db.ServiceInstances[0].State == "running" {
				t.Log("Service has recovered and is now running")
				break
			}

			time.Sleep(pollInterval)
		}
	}

	require.Len(t, db.ServiceInstances, 1, "Service instance should still exist after wait")
	assert.Equal(t, "running", db.ServiceInstances[0].State, "Service should be running after recovery")

	t.Logf("Service instance recovered: %s (state: %s)", db.ServiceInstances[0].ServiceInstanceID, db.ServiceInstances[0].State)
	t.Log("Service recovery test completed successfully")
}

// TestUpdateDatabaseRemoveService tests removing a service from a database.
func TestUpdateDatabaseRemoveService(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Log("Creating database with MCP service")

	// Create database with service
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_remove_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{
				{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					//Version:     "1.0.0",
					Version: "latest",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
					Config: map[string]any{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test",
					},
				},
			},
		},
	})

	// Verify service instance exists
	require.Len(t, db.ServiceInstances, 1, "Expected 1 service instance initially")

	t.Log("Removing service from database")

	// Update database to remove service (empty services array)
	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_remove_service",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("testpassword"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			Services: []*controlplane.ServiceSpec{}, // Empty services array
		},
	})
	require.NoError(t, err, "Failed to update database")

	t.Log("Database updated, verifying service instance was removed")

	// Verify service instance was removed (declarative deletion)
	assert.Empty(t, db.ServiceInstances, "Service instances should be empty after removal")

	t.Log("Remove service test completed successfully")
}
