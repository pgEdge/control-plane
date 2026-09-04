package database_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestService_ApplyMajorUpgrade(t *testing.T) {
	targetImage := "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1"
	successfulUpgrade := &database.AvailableUpgrade{
		PostgresVersion: "18.4",
		SpockVersion:    "6",
		Image:           targetImage,
	}

	t.Run("happy path: state set to modifying, spec untouched", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "18.4")

		result, err := svc.ApplyMajorUpgrade(t.Context(), dbID, "6", targetImage, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, database.DatabaseStateAvailable, result.PrevState)
		assert.Equal(t, "6", result.TargetSpockVersion)
		assert.Equal(t, targetImage, result.Image)
		assert.Equal(t, []string{"n1"}, result.NodeOrder)
		assert.Equal(t, database.DatabaseStateModifying, result.Database.State)

		// the spec itself must be untouched - the workflow rolls it forward
		// one node at a time.
		assert.Equal(t, "5", result.Database.Spec.SpockVersion)
	})

	t.Run("returns ErrDatabaseNotFound for unknown id", func(t *testing.T) {
		svc := newTestService(t, &stubOrchestrator{})
		_, err := svc.ApplyMajorUpgrade(t.Context(), "no-such-db", "6", targetImage, nil)
		assert.True(t, errors.Is(err, database.ErrDatabaseNotFound))
	})

	t.Run("returns ErrDatabaseNotModifiable when not in modifiable state", func(t *testing.T) {
		svc := newTestService(t, &stubOrchestrator{})
		dbID := seedDatabase(t, svc, "18.4")
		require.NoError(t, svc.UpdateDatabaseState(t.Context(), dbID, database.DatabaseStateAvailable, database.DatabaseStateModifying))

		_, err := svc.ApplyMajorUpgrade(t.Context(), dbID, "6", targetImage, nil)
		assert.True(t, errors.Is(err, database.ErrDatabaseNotModifiable))
	})

	t.Run("propagates ErrUpgradeNotAvailable from orchestrator", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return nil, fmt.Errorf("%w: image not in manifest", database.ErrUpgradeNotAvailable)
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "18.4")

		_, err := svc.ApplyMajorUpgrade(t.Context(), dbID, "6", targetImage, nil)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("defaults node_order to spec order when empty", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		ctx := t.Context()

		db, err := svc.CreateDatabase(ctx, &database.Spec{
			DatabaseName:    "test",
			PostgresVersion: "18.4",
			SpockVersion:    "5",
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}},
				{Name: "n2", HostIDs: []string{"host-2"}},
			},
			DatabaseUsers: []*database.User{
				{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))

		result, err := svc.ApplyMajorUpgrade(ctx, db.DatabaseID, "6", targetImage, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"n1", "n2"}, result.NodeOrder)
	})

	t.Run("accepts a valid explicit node_order permutation", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		ctx := t.Context()

		db, err := svc.CreateDatabase(ctx, &database.Spec{
			DatabaseName:    "test",
			PostgresVersion: "18.4",
			SpockVersion:    "5",
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}},
				{Name: "n2", HostIDs: []string{"host-2"}},
			},
			DatabaseUsers: []*database.User{
				{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))

		result, err := svc.ApplyMajorUpgrade(ctx, db.DatabaseID, "6", targetImage, []string{"n2", "n1"})
		require.NoError(t, err)
		assert.Equal(t, []string{"n2", "n1"}, result.NodeOrder)
	})

	t.Run("rejects node_order naming an unknown node", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "18.4")

		_, err := svc.ApplyMajorUpgrade(t.Context(), dbID, "6", targetImage, []string{"no-such-node"})
		assert.True(t, errors.Is(err, database.ErrInvalidDatabaseUpdate))
	})

	t.Run("rejects node_order with a duplicate node", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		ctx := t.Context()

		db, err := svc.CreateDatabase(ctx, &database.Spec{
			DatabaseName:    "test",
			PostgresVersion: "18.4",
			SpockVersion:    "5",
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}},
				{Name: "n2", HostIDs: []string{"host-2"}},
			},
			DatabaseUsers: []*database.User{
				{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))

		_, err = svc.ApplyMajorUpgrade(ctx, db.DatabaseID, "6", targetImage, []string{"n1", "n1"})
		assert.True(t, errors.Is(err, database.ErrInvalidDatabaseUpdate))
	})

	t.Run("rejects node_order missing a node", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return successfulUpgrade, nil
			},
		}
		svc := newTestService(t, orch)
		ctx := t.Context()

		db, err := svc.CreateDatabase(ctx, &database.Spec{
			DatabaseName:    "test",
			PostgresVersion: "18.4",
			SpockVersion:    "5",
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}},
				{Name: "n2", HostIDs: []string{"host-2"}},
			},
			DatabaseUsers: []*database.User{
				{Username: "admin", Attributes: []string{"SUPERUSER", "LOGIN"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, svc.UpdateDatabaseState(ctx, db.DatabaseID, database.DatabaseStateCreating, database.DatabaseStateAvailable))

		_, err = svc.ApplyMajorUpgrade(ctx, db.DatabaseID, "6", targetImage, []string{"n1"})
		assert.True(t, errors.Is(err, database.ErrInvalidDatabaseUpdate))
	})
}

func TestService_RollbackApplyMajorUpgrade(t *testing.T) {
	targetImage := "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1"

	t.Run("restores state", func(t *testing.T) {
		orch := &stubOrchestrator{
			findMajorUpgradeFn: func(_ *ds.PgEdgeVersion, _, _ string) (*database.AvailableUpgrade, error) {
				return &database.AvailableUpgrade{PostgresVersion: "18.4", SpockVersion: "6", Image: targetImage}, nil
			},
		}
		svc := newTestService(t, orch)
		dbID := seedDatabase(t, svc, "18.4")

		result, err := svc.ApplyMajorUpgrade(t.Context(), dbID, "6", targetImage, nil)
		require.NoError(t, err)
		assert.Equal(t, database.DatabaseStateModifying, result.Database.State)

		require.NoError(t, svc.RollbackApplyMajorUpgrade(t.Context(), result))

		restored, err := svc.GetDatabase(t.Context(), dbID)
		require.NoError(t, err)
		assert.Equal(t, database.DatabaseStateAvailable, restored.State)
		// the spec was never touched by ApplyMajorUpgrade, so it should still
		// reflect the original spock version.
		assert.Equal(t, "5", restored.Spec.SpockVersion)
	})
}

func TestService_ApplyMajorUpgradeSpecChange(t *testing.T) {
	t.Run("persists a spock major-version change that ordinary updates reject", func(t *testing.T) {
		svc := newTestService(t, &stubOrchestrator{})
		ctx := t.Context()
		dbID := seedDatabase(t, svc, "18.4")
		require.NoError(t, svc.UpdateDatabaseState(ctx, dbID, database.DatabaseStateAvailable, database.DatabaseStateModifying))

		current, err := svc.GetDatabase(ctx, dbID)
		require.NoError(t, err)

		newSpec := current.Spec.Clone()
		newSpec.Nodes[0].SpockVersion = "6"

		updated, err := svc.ApplyMajorUpgradeSpecChange(ctx, newSpec, database.DatabaseStateModifying)
		require.NoError(t, err)
		assert.Equal(t, "6", updated.Spec.Nodes[0].SpockVersion)
	})
}
