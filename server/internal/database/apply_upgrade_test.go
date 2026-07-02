package database_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

// stubOrchestrator is a minimal Orchestrator for service-layer tests.
// Only FindUpgrade is configurable; all other methods return zero values.
type stubOrchestrator struct {
	findUpgradeFn func(*ds.PgEdgeVersion, string) (*database.AvailableUpgrade, error)
}

func (s *stubOrchestrator) FindUpgrade(cur *ds.PgEdgeVersion, img string) (*database.AvailableUpgrade, error) {
	if s.findUpgradeFn != nil {
		return s.findUpgradeFn(cur, img)
	}
	return nil, database.ErrUpgradeNotAvailable
}

func (s *stubOrchestrator) GenerateInstanceResources(*database.InstanceSpec, database.Scripts) (*database.InstanceResources, error) {
	return nil, nil
}
func (s *stubOrchestrator) GenerateInstanceRestoreResources(*database.InstanceSpec, uuid.UUID) (*database.InstanceResources, error) {
	return nil, nil
}
func (s *stubOrchestrator) GenerateServiceInstanceResources(*database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	return nil, nil
}
func (s *stubOrchestrator) GetInstanceConnectionInfo(context.Context, string, string, *int, *int, *ds.PgEdgeVersion) (*database.ConnectionInfo, error) {
	return nil, nil
}
func (s *stubOrchestrator) GetServiceInstanceStatus(context.Context, string) (*database.ServiceInstanceStatus, error) {
	return nil, nil
}
func (s *stubOrchestrator) CreatePgBackRestBackup(context.Context, io.Writer, *database.InstanceSpec, *pgbackrest.BackupOptions) error {
	return nil
}
func (s *stubOrchestrator) ExecuteInstanceCommand(context.Context, io.Writer, string, string, ...string) error {
	return nil
}
func (s *stubOrchestrator) ValidateInstanceSpecs(context.Context, []*database.InstanceSpecChange) ([]*database.ValidationResult, error) {
	return nil, nil
}
func (s *stubOrchestrator) StopInstance(context.Context, string) error  { return nil }
func (s *stubOrchestrator) StartInstance(context.Context, string) error { return nil }
func (s *stubOrchestrator) NodeDSN(context.Context, *resource.Context, string, string, string) (*postgres.DSN, error) {
	return nil, nil
}
func (s *stubOrchestrator) InstancePaths(*ds.Version, string) (database.InstancePaths, error) {
	return database.InstancePaths{}, nil
}
func (s *stubOrchestrator) ReconcileInstanceSpec(_, _ *database.InstanceSpec) error { return nil }
func (s *stubOrchestrator) ReconcileServiceInstanceSpec(_, _ *database.ServiceInstanceSpec) error {
	return nil
}
func (s *stubOrchestrator) AvailableUpgrades(*ds.PgEdgeVersion) []*database.AvailableUpgrade {
	return nil
}

// newTestService constructs a Service wired to an embedded test etcd.
func newTestService(t *testing.T, orch database.Orchestrator) *database.Service {
	t.Helper()
	srv := storagetest.NewEtcdTestServer(t)
	client := srv.Client(t)
	store := database.NewStore(client, uuid.NewString())
	logFactory, err := logging.NewFactory(config.Config{}, zerolog.Nop())
	require.NoError(t, err)
	// hostSvc and portsSvc are not used by ApplyUpgrade / RollbackApplyUpgrade.
	return database.NewService(config.Config{}, orch, store, nil, nil, logFactory)
}

// seedDatabase creates a minimal database record in the store and returns
// its ID. postgresVersion must be a full semver, e.g. "17.9".
func seedDatabase(t *testing.T, svc *database.Service, postgresVersion string) string {
	t.Helper()
	ctx := t.Context()
	db, err := svc.CreateDatabase(ctx, &database.Spec{
		DatabaseName:    "test",
		PostgresVersion: postgresVersion,
		SpockVersion:    "5",
		Nodes: []*database.Node{
			{Name: "n1", HostIDs: []string{"host-1"}},
		},
		DatabaseUsers: []*database.User{
			{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
		},
	})
	require.NoError(t, err)
	// Transition to available so the state is modifiable.
	require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))
	return db.DatabaseID
}

func TestService_ApplyUpgrade(t *testing.T) {
	targetImage := "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.8-standard-1"
	successfulUpgrade := &database.AvailableUpgrade{
		PostgresVersion: "17.10",
		SpockVersion:    "5.0.8",
		Image:           targetImage,
	}

	t.Run("happy path: spec updated and state set to modifying", func(t *testing.T) {
		orch := &stubOrchestrator{
			findUpgradeFn: func(_ *ds.PgEdgeVersion, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "17.9")

		result, err := svc.ApplyUpgrade(t.Context(), dbID, targetImage)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, database.DatabaseStateModifying, result.Database.State)
		assert.Equal(t, "17.10", result.Database.Spec.PostgresVersion)
		assert.Equal(t, database.DatabaseStateAvailable, result.PrevState)
	})

	t.Run("returns ErrDatabaseNotFound for unknown id", func(t *testing.T) {
		svc := newTestService(t, &stubOrchestrator{})
		_, err := svc.ApplyUpgrade(t.Context(), "no-such-db", targetImage)
		assert.True(t, errors.Is(err, database.ErrDatabaseNotFound))
	})

	t.Run("returns ErrDatabaseNotModifiable when not in modifiable state", func(t *testing.T) {
		orch := &stubOrchestrator{}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "17.9")
		require.NoError(t, svc.UpdateDatabaseState(t.Context(), dbID, database.DatabaseStateAvailable, database.DatabaseStateModifying))

		_, err := svc.ApplyUpgrade(t.Context(), dbID, targetImage)
		assert.True(t, errors.Is(err, database.ErrDatabaseNotModifiable))
	})

	t.Run("propagates ErrUpgradeNotAvailable from orchestrator", func(t *testing.T) {
		orch := &stubOrchestrator{
			findUpgradeFn: func(_ *ds.PgEdgeVersion, _ string) (*database.AvailableUpgrade, error) {
				return nil, fmt.Errorf("%w: image not in manifest", database.ErrUpgradeNotAvailable)
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "17.9")

		_, err := svc.ApplyUpgrade(t.Context(), dbID, targetImage)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("clears per-node version override matching old version", func(t *testing.T) {
		orch := &stubOrchestrator{
			findUpgradeFn: func(_ *ds.PgEdgeVersion, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		ctx := t.Context()

		db, err := svc.CreateDatabase(ctx, &database.Spec{
			DatabaseName:    "test",
			PostgresVersion: "17.9",
			SpockVersion:    "5",
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}, PostgresVersion: "17.9"},
			},
			DatabaseUsers: []*database.User{
				{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))

		result, err := svc.ApplyUpgrade(ctx, db.DatabaseID, targetImage)
		require.NoError(t, err)
		for _, node := range result.Database.Spec.Nodes {
			assert.Empty(t, node.PostgresVersion, "per-node override matching old version should be cleared")
		}
	})
}

func TestService_RollbackApplyUpgrade(t *testing.T) {
	targetImage := "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.8-standard-1"

	t.Run("restores spec and state atomically", func(t *testing.T) {
		orch := &stubOrchestrator{
			findUpgradeFn: func(_ *ds.PgEdgeVersion, _ string) (*database.AvailableUpgrade, error) {
				return &database.AvailableUpgrade{PostgresVersion: "17.10", SpockVersion: "5.0.8", Image: targetImage}, nil
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "17.9")

		result, err := svc.ApplyUpgrade(t.Context(), dbID, targetImage)
		require.NoError(t, err)
		assert.Equal(t, "17.10", result.Database.Spec.PostgresVersion)

		require.NoError(t, svc.RollbackApplyUpgrade(t.Context(), result))

		restored, err := svc.GetDatabase(t.Context(), dbID)
		require.NoError(t, err)
		assert.Equal(t, database.DatabaseStateAvailable, restored.State)
		assert.Equal(t, "17.9", restored.Spec.PostgresVersion)
	})
}
