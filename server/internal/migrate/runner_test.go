package migrate_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/election"
	"github.com/pgEdge/control-plane/server/internal/migrate"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestRunner(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)
	logger := testutils.Logger(t)

	t.Run("acquires lock and runs migrations", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		var ran bool
		m := &runnerMockMigration{
			id: "test-migration",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				ran = true
				return nil
			},
		}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m}, candidate)
		err := runner.Run(t.Context())
		require.NoError(t, err)
		assert.True(t, ran, "migration should have run")
	})

	t.Run("multiple runners", func(t *testing.T) {
		// Starts two concurrent runners and asserts that they both exit
		// successfully and that the migration is only run once.

		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		var ranOnce atomic.Bool
		var ranTwice atomic.Bool
		m := &runnerMockMigration{
			id: "test-migration",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				if ranOnce.Load() {
					ranTwice.Store(true)
				} else {
					ranOnce.Store(true)
				}
				return nil
			},
		}

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()

			candidate := testCandidate(t, electionSvc, "host-1")
			runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m}, candidate)
			require.NoError(t, runner.Run(t.Context()))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()

			candidate := testCandidate(t, electionSvc, "host-2")
			runner := migrate.NewRunner("host-2", store, i, logger, []migrate.Migration{m}, candidate)
			require.NoError(t, runner.Run(t.Context()))
		}()

		wg.Wait()

		assert.True(t, ranOnce.Load())
		assert.False(t, ranTwice.Load())
	})
}

func TestRunnerMigrationOrdering(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)
	logger := zerolog.Nop()

	t.Run("runs migrations in order", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		var order []string
		m1 := &runnerMockMigration{
			id: "migration-1",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-1")
				return nil
			},
		}
		m2 := &runnerMockMigration{
			id: "migration-2",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-2")
				return nil
			},
		}
		m3 := &runnerMockMigration{
			id: "migration-3",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-3")
				return nil
			},
		}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2, m3}, candidate)
		err := runner.Run(t.Context())
		require.NoError(t, err)

		assert.Equal(t, []string{"migration-1", "migration-2", "migration-3"}, order)
	})

	t.Run("starts from current revision", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		// Pre-set revision to migration-2
		err := store.Revision.Create(&migrate.StoredRevision{Identifier: "migration-2"}).Exec(t.Context())
		require.NoError(t, err)

		var order []string
		m1 := &runnerMockMigration{
			id: "migration-1",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-1")
				return nil
			},
		}
		m2 := &runnerMockMigration{
			id: "migration-2",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-2")
				return nil
			},
		}
		m3 := &runnerMockMigration{
			id: "migration-3",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-3")
				return nil
			},
		}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2, m3}, candidate)
		err = runner.Run(t.Context())
		require.NoError(t, err)

		// Should only run migration-3
		assert.Equal(t, []string{"migration-3"}, order)
	})

	t.Run("stops on first failure", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		var order []string
		m1 := &runnerMockMigration{
			id: "migration-1",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-1")
				return nil
			},
		}
		m2 := &runnerMockMigration{
			id: "migration-2",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-2")
				return errors.New("migration failed")
			},
		}
		m3 := &runnerMockMigration{
			id: "migration-3",
			runFunc: func(_ context.Context, _ *do.Injector) error {
				order = append(order, "migration-3")
				return nil
			},
		}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2, m3}, candidate)
		err := runner.Run(t.Context())
		assert.ErrorContains(t, err, "migration failed")

		// Should stop after migration-2 fails
		assert.Equal(t, []string{"migration-1", "migration-2"}, order)
	})

	t.Run("records status for each migration", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		m1 := &runnerMockMigration{id: "migration-1"}
		m2 := &runnerMockMigration{id: "migration-2", err: errors.New("failed")}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2}, candidate)
		err := runner.Run(t.Context())
		assert.ErrorContains(t, err, "failed")

		status1, err := store.Result.Get("migration-1").Exec(t.Context())
		require.NoError(t, err)
		assert.True(t, status1.Successful)
		assert.Equal(t, "host-1", status1.RunByHostID)
		assert.NotEmpty(t, status1.StartedAt)
		assert.NotEmpty(t, status1.CompletedAt)
		assert.NotNil(t, status1.RunByVersionInfo)

		status2, err := store.Result.Get("migration-2").Exec(t.Context())
		require.NoError(t, err)
		assert.False(t, status2.Successful)
		assert.Equal(t, "failed", status2.Error)
		assert.Equal(t, "host-1", status2.RunByHostID)
		assert.NotEmpty(t, status2.StartedAt)
		assert.NotEmpty(t, status2.CompletedAt)
		assert.NotNil(t, status2.RunByVersionInfo)
	})

	t.Run("updates revision after each successful migration", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		m1 := &runnerMockMigration{id: "migration-1"}
		m2 := &runnerMockMigration{id: "migration-2"}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2}, candidate)
		err := runner.Run(t.Context())
		require.NoError(t, err)

		rev, err := store.Revision.Get().Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "migration-2", rev.Identifier)
	})

	t.Run("does not update revision after failed migration", func(t *testing.T) {
		root := uuid.NewString()
		electionSvc := election.NewService(election.NewElectionStore(client, root), logger)
		store := migrate.NewStore(client, root)
		i := do.New()

		m1 := &runnerMockMigration{id: "migration-1"}
		m2 := &runnerMockMigration{id: "migration-2", err: errors.New("failed")}

		candidate := testCandidate(t, electionSvc, "host-1")
		runner := migrate.NewRunner("host-1", store, i, logger, []migrate.Migration{m1, m2}, candidate)
		err := runner.Run(t.Context())
		assert.ErrorContains(t, err, "failed")

		rev, err := store.Revision.Get().Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "migration-1", rev.Identifier)
	})
}

type runnerMockMigration struct {
	id      string
	err     error
	runFunc func(context.Context, *do.Injector) error
}

func (m *runnerMockMigration) Identifier() string {
	return m.id
}

func (m *runnerMockMigration) Run(ctx context.Context, i *do.Injector) error {
	if m.runFunc != nil {
		return m.runFunc(ctx, i)
	}
	return m.err
}

func testCandidate(t *testing.T, electionSvc *election.Service, holderID string) *election.Candidate {
	t.Helper()

	candidate := electionSvc.NewCandidate(migrate.ElectionName, holderID, 30*time.Second)

	t.Cleanup(func() {
		candidate.Stop(context.Background())
	})

	return candidate
}
