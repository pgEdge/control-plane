//go:build cluster_test

package clustertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

func TestExternalUpgrade(t *testing.T) {
	// Tests that the control plane updates its records when the user upgrades
	// a database outside of our API.
	t.Parallel()
	ctx := t.Context()

	const (
		startPostgresVersion   string        = "18.2"
		upgradePostgresVersion string        = "18.3"
		upgradeImage           string        = "ghcr.io/pgedge/pgedge-postgres:18.3-spock5.0.6-standard-1"
		spockVersion           string        = "5"
		sleepDuration          time.Duration = 5 * time.Second
	)

	// Helper functions
	assertSpecVersions := func(t *testing.T, spec *controlplane.DatabaseSpec, expectedSpecVersion string, expectedNodeVersions map[string]string) {
		t.Helper()

		actualNodeVersions := make(map[string]string, len(spec.Nodes))
		for _, node := range spec.Nodes {
			var version string
			if node.PostgresVersion != nil {
				version = *node.PostgresVersion
			}
			actualNodeVersions[node.Name] = version
		}
		var actualSpecVersion string
		if spec.PostgresVersion != nil {
			actualSpecVersion = *spec.PostgresVersion
		}
		require.Equal(t, expectedSpecVersion, actualSpecVersion)
		require.Equal(t, expectedNodeVersions, actualNodeVersions)
	}
	assertInstanceVersions := func(t *testing.T, instances []*controlplane.Instance, expectedNodeHostVersions map[string]map[string]string) {
		t.Helper()

		actualNodeHostVersions := map[string]map[string]string{}
		for _, instance := range instances {
			require.Equal(t, client.InstanceStateAvailable, instance.State)

			if _, ok := actualNodeHostVersions[instance.NodeName]; !ok {
				actualNodeHostVersions[instance.NodeName] = map[string]string{}
			}
			var version string
			if instance.Postgres.Version != nil {
				version = *instance.Postgres.Version
			}
			actualNodeHostVersions[instance.NodeName][instance.HostID] = version
		}
		require.Equal(t, expectedNodeHostVersions, actualNodeHostVersions)
	}
	upgradeService := func(t *testing.T, databaseID, nodeName, hostID string) {
		t.Helper()

		tLogf(t, "upgrading %s %s instance", nodeName, hostID)

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		serviceName := dockerCmd(t, ctx,
			"service",
			"ls",
			fmt.Sprintf("--filter=label=pgedge.database.id=%s", databaseID),
			fmt.Sprintf("--filter=label=pgedge.node.name=%s", nodeName),
			fmt.Sprintf("--filter=label=pgedge.host.id=%s", hostID),
			"--format={{.Name}}",
		)
		require.NotEmpty(t, serviceName)
		dockerCmd(t, ctx,
			"service",
			"update",
			fmt.Sprintf("--image=%s", upgradeImage),
			// disabling healthchecks to speed up startup time
			"--no-healthcheck",
			serviceName,
		)
	}

	env := map[string]string{
		"PGEDGE_DATABASES_MONITOR_INTERVAL_SECONDS": "3",
	}
	cluster := NewCluster(t, ClusterConfig{
		Hosts: []HostConfig{
			{ID: "host-1", ExtraEnv: env},
			{ID: "host-2", ExtraEnv: env},
			{ID: "host-3", ExtraEnv: env},
		},
	})
	cluster.Init(t)

	spec := &controlplane.DatabaseSpec{
		DatabaseName:    "test_upgrade",
		PostgresVersion: pointerTo(startPostgresVersion),
		SpockVersion:    pointerTo(spockVersion),
		Nodes: []*controlplane.DatabaseNodeSpec{
			{
				Name:    "n1",
				HostIds: []controlplane.Identifier{"host-1", "host-2"},
			},
			{
				Name:    "n2",
				HostIds: []controlplane.Identifier{"host-3"},
			},
		},
	}

	tLog(t, "creating database")

	createResp, err := cluster.Client().CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
		Spec: spec,
	})
	require.NoError(t, err)

	databaseID := createResp.Database.ID

	t.Cleanup(func() {
		// Use a new context for cleanup operations since t.Context is canceled.
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if testConfig.skipCleanup {
			tLogf(t, "skipping cleanup for database '%s'", databaseID)
			return
		}

		tLogf(t, "cleaning up database '%s'", databaseID)

		resp, err := cluster.Client().DeleteDatabase(ctx, &controlplane.DeleteDatabasePayload{
			DatabaseID: databaseID,
		})
		if err != nil {
			tLogf(t, "failed to delete database '%s': %v", databaseID, err)
			return
		}

		tLog(t, "waiting for database deletion to complete")

		err = waitForTaskComplete(ctx, cluster.Client(), databaseID, resp.Task.TaskID, time.Minute)
		if err != nil {
			tLogf(t, "failed while waiting for database deletion '%s'", databaseID)
			return
		}
	})

	tLog(t, "waiting for database creation to complete")

	err = waitForTaskComplete(ctx, cluster.Client(), databaseID, createResp.Task.TaskID, 3*time.Minute)
	require.NoError(t, err)

	tLog(t, "sleeping to allow instance monitor interval to complete")

	time.Sleep(sleepDuration)

	tLogf(t, "asserting that all instances and spec versions are %s", startPostgresVersion)

	db, err := cluster.Client().GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: databaseID,
	})
	require.NoError(t, err)

	assertSpecVersions(t, db.Spec, startPostgresVersion, map[string]string{
		"n1": "",
		"n2": "",
	})
	assertInstanceVersions(t, db.Instances, map[string]map[string]string{
		"n1": map[string]string{
			"host-1": startPostgresVersion,
			"host-2": startPostgresVersion,
		},
		"n2": map[string]string{
			"host-3": startPostgresVersion,
		},
	})

	tLog(t, "getting database docker service names")

	upgradeService(t, string(databaseID), "n1", "host-2")
	upgradeService(t, string(databaseID), "n2", "host-3")

	tLog(t, "sleeping to allow instance monitor interval and version reconciliation to complete")

	time.Sleep(sleepDuration)

	tLogf(t, "asserting that n2 is %s in the spec and that the n1-host-2 and n2-host-3 instances are %s", upgradePostgresVersion, upgradePostgresVersion)

	db, err = cluster.Client().GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: databaseID,
	})
	require.NoError(t, err)

	assertSpecVersions(t, db.Spec, startPostgresVersion, map[string]string{
		"n1": "",
		"n2": upgradePostgresVersion,
	})
	assertInstanceVersions(t, db.Instances, map[string]map[string]string{
		"n1": map[string]string{
			"host-1": startPostgresVersion,
			"host-2": upgradePostgresVersion,
		},
		"n2": map[string]string{
			"host-3": upgradePostgresVersion,
		},
	})

	upgradeService(t, string(databaseID), "n1", "host-1")

	tLog(t, "sleeping to allow monitor interval and version reconciliation to complete")

	time.Sleep(sleepDuration)

	tLogf(t, "asserting the top-level version is %s and that all instances are %s", upgradePostgresVersion, upgradePostgresVersion)

	db, err = cluster.Client().GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: databaseID,
	})
	require.NoError(t, err)

	assertSpecVersions(t, db.Spec, upgradePostgresVersion, map[string]string{
		"n1": "",
		"n2": "",
	})
	assertInstanceVersions(t, db.Instances, map[string]map[string]string{
		"n1": map[string]string{
			"host-1": upgradePostgresVersion,
			"host-2": upgradePostgresVersion,
		},
		"n2": map[string]string{
			"host-3": upgradePostgresVersion,
		},
	})

	tLog(t, "performing a no-op update")

	// We still expect to see some resource updates in the logs because the
	// version number shows up in a few resources states. This does trigger a
	// patroni reload in Swarm databases, which eats up time, but no actual
	// changes should occur.

	updateResp, err := cluster.Client().UpdateDatabase(ctx, &controlplane.UpdateDatabasePayload{
		DatabaseID: databaseID,
		Request: &controlplane.UpdateDatabaseRequest{
			Spec: db.Spec,
		},
	})
	require.NoError(t, err)

	tLog(t, "waiting for database update to complete")

	err = waitForTaskComplete(ctx, cluster.Client(), databaseID, updateResp.Task.TaskID, 3*time.Minute)
	require.NoError(t, err)

	tLog(t, "sleeping to allow instance monitor interval to complete")

	time.Sleep(sleepDuration)

	tLog(t, "asserting that top-level versions have not changed")

	db, err = cluster.Client().GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: databaseID,
	})
	require.NoError(t, err)

	assertSpecVersions(t, db.Spec, upgradePostgresVersion, map[string]string{
		"n1": "",
		"n2": "",
	})
	assertInstanceVersions(t, db.Instances, map[string]map[string]string{
		"n1": map[string]string{
			"host-1": upgradePostgresVersion,
			"host-2": upgradePostgresVersion,
		},
		"n2": map[string]string{
			"host-3": upgradePostgresVersion,
		},
	})
}
