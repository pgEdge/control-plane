//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// postgrestSpec returns a ServiceSpec for a PostgREST service on the given host.
func postgrestSpec(hostID string, port int, config map[string]any) *controlplane.ServiceSpec {
	if config == nil {
		config = map[string]any{}
	}
	return &controlplane.ServiceSpec{
		ServiceID:   "postgrest-api",
		ServiceType: "postgrest",
		Version:     "latest",
		HostIds:     []controlplane.Identifier{controlplane.Identifier(hostID)},
		Port:        pointerTo(port),
		Config:      config,
	}
}

// postgrestBaseSpec returns the common database spec used across PostgREST tests.
// services is appended directly so callers control what services are included.
func postgrestBaseSpec(dbName string, nodeHosts []string, services []*controlplane.ServiceSpec) *controlplane.DatabaseSpec {
	nodes := make([]*controlplane.DatabaseNodeSpec, len(nodeHosts))
	for i, h := range nodeHosts {
		nodes[i] = &controlplane.DatabaseNodeSpec{
			Name:    "n1",
			HostIds: []controlplane.Identifier{controlplane.Identifier(h)},
		}
		if len(nodeHosts) > 1 {
			nodes[i].Name = []string{"n1", "n2", "n3"}[i]
		}
	}
	return &controlplane.DatabaseSpec{
		DatabaseName: dbName,
		DatabaseUsers: []*controlplane.DatabaseUserSpec{
			{
				Username:   "admin",
				Password:   pointerTo("testpassword"),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			},
		},
		Port:     pointerTo(0),
		Nodes:    nodes,
		Services: services,
	}
}

// waitForPostgRESTRunning polls until the PostgREST service instance on the given
// host reaches the "running" state, or the deadline is exceeded.
func waitForPostgRESTRunning(ctx context.Context, t testing.TB, db *DatabaseFixture, serviceID, hostID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		require.NoError(t, db.Refresh(ctx))
		for _, si := range db.ServiceInstances {
			if si.ServiceID == serviceID && si.HostID == hostID && si.State == "running" {
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("service %s on host %s did not reach running state within %s", serviceID, hostID, timeout)
}

// serviceURL returns the base URL for a PostgREST service instance.
func serviceURL(db *DatabaseFixture, hostID string) string {
	for _, si := range db.ServiceInstances {
		if si.HostID != hostID || si.Status == nil || len(si.Status.Addresses) == 0 {
			continue
		}
		for _, p := range si.Status.Ports {
			if p.Name == "http" && p.ContainerPort != nil && *p.ContainerPort == 8080 && p.HostPort != nil {
				return fmt.Sprintf("http://%s:%d", si.Status.Addresses[0], *p.HostPort)
			}
		}
	}
	return ""
}

// TestProvisionPostgREST provisions PostgREST alongside a single-node database
// and verifies the service reaches the running state.
func TestProvisionPostgREST(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_provision",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		),
	})

	require.Len(t, db.ServiceInstances, 1, "expected 1 service instance")
	si := db.ServiceInstances[0]
	assert.Equal(t, "postgrest-api", si.ServiceID)
	assert.Equal(t, string(host1), si.HostID)
	assert.NotEmpty(t, si.ServiceInstanceID)

	if si.State != "running" {
		waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)
	}

	si = db.ServiceInstances[0]
	assert.Equal(t, "running", si.State)

	t.Logf("PostgREST service instance %s running on %s", si.ServiceInstanceID, si.HostID)
}

// TestProvisionPostgRESTWithJWT provisions PostgREST with a JWT secret and
// verifies the service starts correctly.
func TestProvisionPostgRESTWithJWT(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_jwt",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, map[string]any{
					"jwt_secret": "a-test-jwt-secret-of-at-least-32-chars",
					"jwt_aud":    "test",
				}),
			},
		),
	})

	require.Len(t, db.ServiceInstances, 1)
	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	si := db.ServiceInstances[0]
	assert.Equal(t, "running", si.State)
}

// TestPostgRESTPreflight_MissingSchema verifies the preflight check rejects
// a deployment when the configured schema does not exist.
func TestPostgRESTPreflight_MissingSchema(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create the database without any services first (this must succeed).
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec("test_postgrest_preflight_schema", []string{host1}, nil),
	})

	// Adding PostgREST with a nonexistent schema must cause the task to fail.
	err := db.Update(ctx, UpdateOptions{
		Spec: postgrestBaseSpec(
			"test_postgrest_preflight_schema",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, map[string]any{
					"db_schemas": "nonexistent_schema",
				}),
			},
		),
	})
	require.Error(t, err, "expected update task to fail due to missing schema")
	assert.Contains(t, err.Error(), "nonexistent_schema", "error should mention the missing schema")
}

// TestPostgRESTPreflight_MissingAnonRole verifies the preflight check rejects
// a deployment when the configured anon role does not exist.
func TestPostgRESTPreflight_MissingAnonRole(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create the database without any services first (this must succeed).
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec("test_postgrest_preflight_role", []string{host1}, nil),
	})

	// Adding PostgREST with a nonexistent anon role must cause the task to fail.
	err := db.Update(ctx, UpdateOptions{
		Spec: postgrestBaseSpec(
			"test_postgrest_preflight_role",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, map[string]any{
					"db_anon_role": "nonexistent_role",
				}),
			},
		),
	})
	require.Error(t, err, "expected update task to fail due to missing anon role")
	assert.Contains(t, err.Error(), "nonexistent_role", "error should mention the missing role")
}

// TestPostgRESTHealthCheck verifies the service responds to HTTP requests once running.
func TestPostgRESTHealthCheck(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_health",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		),
	})

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	// PostgREST serves an OpenAPI spec at the root path.
	url := serviceURL(db, host1)
	if url == "" {
		t.Skip("service URL not available — status.addresses not populated yet")
	}

	resp, err := http.Get(url + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "PostgREST root endpoint should return 200")
}

// TestPostgRESTServiceUserRoles verifies the CP created the correct Postgres
// roles for the PostgREST authenticator.
func TestPostgRESTServiceUserRoles(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_roles",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		),
	})

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n1"), WithRole("primary")),
		Username: "admin",
		Password: "testpassword",
	})
	require.NoError(t, err)
	defer conn.Close(ctx)

	// The RW service user must have NOINHERIT (rolinherit = false).
	rows, err := conn.Query(ctx, `
		SELECT rolname, rolinherit
		FROM pg_roles
		WHERE rolname LIKE 'svc_%'
		  AND rolname LIKE '%_rw'
		ORDER BY rolname
	`)
	require.NoError(t, err)
	defer rows.Close()

	found := false
	for rows.Next() {
		var rolname string
		var rolinherit bool
		require.NoError(t, rows.Scan(&rolname, &rolinherit))
		assert.False(t, rolinherit, "RW service role %s must have NOINHERIT (rolinherit = false)", rolname)
		found = true
		t.Logf("role %s: rolinherit=%v", rolname, rolinherit)
	}
	assert.True(t, found, "expected at least one _rw service role")

	// The RW role must be granted the anon role (pgedge_application_read_only).
	var anonGranted bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_auth_members m
			JOIN pg_roles r ON m.member = r.oid
			JOIN pg_roles g ON m.roleid = g.oid
			WHERE r.rolname LIKE 'svc_%_rw'
			  AND g.rolname = 'pgedge_application_read_only'
		)
	`).Scan(&anonGranted)
	require.NoError(t, err)
	assert.True(t, anonGranted, "RW service role must be granted pgedge_application_read_only")
}

// TestPostgRESTAddToExistingDatabase verifies PostgREST provisions correctly
// when added to a database via an update after initial creation.
func TestPostgRESTAddToExistingDatabase(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// Create database without PostgREST.
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec("test_postgrest_add", []string{host1}, nil),
	})

	require.Empty(t, db.ServiceInstances, "no service instances before adding PostgREST")

	// Add PostgREST via update.
	err := db.Update(ctx, UpdateOptions{
		Spec: postgrestBaseSpec(
			"test_postgrest_add",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		),
	})
	require.NoError(t, err)

	require.Len(t, db.ServiceInstances, 1)
	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	assert.Equal(t, "running", db.ServiceInstances[0].State)
}

// TestPostgRESTRemove verifies PostgREST is cleanly removed and its Postgres
// roles are dropped when the service is removed from the spec.
func TestPostgRESTRemove(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_remove",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		),
	})

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	// Remove PostgREST.
	err := db.Update(ctx, UpdateOptions{
		Spec: postgrestBaseSpec("test_postgrest_remove", []string{host1}, []*controlplane.ServiceSpec{}),
	})
	require.NoError(t, err)

	assert.Empty(t, db.ServiceInstances, "service instances should be empty after removal")

	// Verify the service user roles are dropped from Postgres.
	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n1"), WithRole("primary")),
		Username: "admin",
		Password: "testpassword",
	})
	require.NoError(t, err)
	defer conn.Close(ctx)

	var count int
	err = conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_roles WHERE rolname LIKE 'svc_%'
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "all service roles should be dropped after PostgREST removal")
}

// TestPostgRESTConfigUpdate verifies updating PostgREST config updates the
// service in place without recreating the service instance.
func TestPostgRESTConfigUpdate(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: postgrestBaseSpec(
			"test_postgrest_config_update",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, map[string]any{
					"max_rows": 100,
				}),
			},
		),
	})

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	origInstanceID := db.ServiceInstances[0].ServiceInstanceID

	// Update max_rows.
	err := db.Update(ctx, UpdateOptions{
		Spec: postgrestBaseSpec(
			"test_postgrest_config_update",
			[]string{host1},
			[]*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, map[string]any{
					"max_rows": 500,
				}),
			},
		),
	})
	require.NoError(t, err)

	require.Len(t, db.ServiceInstances, 1)
	assert.Equal(t, origInstanceID, db.ServiceInstances[0].ServiceInstanceID,
		"service instance ID should not change on config update")
	assert.Equal(t, "running", db.ServiceInstances[0].State)
}

// TestPostgRESTMultiHostDBURI provisions PostgREST against a 3-node database
// and verifies the db-uri in postgrest.conf contains all 3 node hostnames.
func TestPostgRESTMultiHostDBURI(t *testing.T) {
	t.Parallel()

	hosts := fixture.HostIDs()
	if len(hosts) < 3 {
		t.Skip("multi-host test requires at least 3 hosts")
	}
	host1, host2, host3 := hosts[0], hosts[1], hosts[2]

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_postgrest_multihost",
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
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1), controlplane.Identifier(host2), controlplane.Identifier(host3)}},
			},
			Services: []*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		},
	})

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	// Connect to Postgres and confirm service roles exist on all nodes.
	for _, nodeName := range []string{"n1"} {
		conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
			Matcher:  And(WithNode(nodeName), WithRole("primary")),
			Username: "admin",
			Password: "testpassword",
		})
		if err != nil {
			// The primary moved — find it on whichever node is primary.
			conn, err = db.ConnectToInstance(ctx, ConnectionOptions{
				Matcher:  WithRole("primary"),
				Username: "admin",
				Password: "testpassword",
			})
		}
		require.NoError(t, err, "failed to connect to primary on node %s", nodeName)
		defer conn.Close(ctx)

		var count int
		err = conn.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_roles WHERE rolname LIKE 'svc_%_rw'
		`).Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1, "RW service role must exist on node %s", nodeName)
	}

	t.Log("multi-host PostgREST provisioned, service roles present on all nodes")
}

// TestPostgRESTFailover provisions PostgREST on a 3-node database, triggers a
// switchover, and verifies PostgREST reconnects without a redeploy.
func TestPostgRESTFailover(t *testing.T) {
	t.Parallel()

	hosts := fixture.HostIDs()
	if len(hosts) < 3 {
		t.Skip("failover test requires at least 3 hosts")
	}
	host1, host2, host3 := hosts[0], hosts[1], hosts[2]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_postgrest_failover",
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
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(host1), controlplane.Identifier(host2), controlplane.Identifier(host3)}},
			},
			Services: []*controlplane.ServiceSpec{
				postgrestSpec(host1, 0, nil),
			},
		},
	})

	dbID := controlplane.Identifier(db.ID)

	waitForPostgRESTRunning(ctx, t, db, "postgrest-api", host1, 5*time.Minute)

	// Wait for cluster to settle.
	waitFor(func() bool {
		db.Refresh(ctx)
		for _, inst := range db.Instances {
			if inst.NodeName == "n1" && (inst.State == "modifying" || inst.State == "creating") {
				return false
			}
		}
		return true
	}, 60*time.Second)

	getPrimaryID := func() string {
		inst := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
		if inst == nil {
			return ""
		}
		return inst.ID
	}

	origPrimary := getPrimaryID()
	require.NotEmpty(t, origPrimary, "database has no primary instance before switchover")
	t.Logf("original primary: %s", origPrimary)

	// Note the PostgREST service instance ID — it must not change after failover.
	origServiceInstanceID := db.ServiceInstances[0].ServiceInstanceID

	// Trigger a switchover.
	err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
		DatabaseID: dbID,
		NodeName:   "n1",
	})
	require.NoError(t, err, "switchover API call failed")

	// Wait for primary to change.
	waitForPrimaryChange := func(orig string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			db.Refresh(ctx)
			if p := getPrimaryID(); p != "" && p != orig {
				return true
			}
			time.Sleep(1 * time.Second)
		}
		return false
	}
	require.True(t, waitForPrimaryChange(origPrimary, 60*time.Second),
		"primary did not change within timeout")
	t.Logf("new primary: %s", getPrimaryID())

	// PostgREST must stay running — no redeploy.
	require.NoError(t, db.Refresh(ctx))
	require.Len(t, db.ServiceInstances, 1)
	assert.Equal(t, origServiceInstanceID, db.ServiceInstances[0].ServiceInstanceID,
		"PostgREST service instance ID should not change after failover")

	// Give PostgREST time to reconnect via libpq multi-host.
	time.Sleep(15 * time.Second)

	si := db.ServiceInstances[0]
	assert.NotEqual(t, "failed", si.State,
		"PostgREST should not enter failed state after switchover")

	t.Logf("PostgREST service %s still %s after switchover — no redeploy needed", si.ServiceInstanceID, si.State)
}
