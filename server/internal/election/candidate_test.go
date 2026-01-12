package election_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/election"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidate(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)
	logger := testutils.Logger(t)
	store := election.NewElectionStore(client, uuid.NewString())
	electionSvc := election.NewService(store, logger)

	t.Run("basic functionality", func(t *testing.T) {
		ctx := t.Context()
		name := election.Name(uuid.NewString())
		candidate := electionSvc.NewCandidate(name, "host-1", time.Second)

		t.Cleanup(func() {
			candidate.Stop(context.Background())
		})

		// Calling Stop is fine when the candidate is not running
		require.NoError(t, candidate.Stop(ctx))

		require.NoError(t, candidate.Start(ctx))
		// running unopposed, should always be the leader
		assert.True(t, candidate.IsLeader())

		// Wait until we're sure the TTL has expired at least once and that the
		// claim has been refreshed.
		time.Sleep(2 * time.Second)
		assert.True(t, candidate.IsLeader())

		// No error from running start again
		require.NoError(t, candidate.Start(ctx))
		assert.True(t, candidate.IsLeader())

		require.NoError(t, candidate.Stop(ctx))
		assert.False(t, candidate.IsLeader())

		// No error from running stop again
		require.NoError(t, candidate.Stop(ctx))
		assert.False(t, candidate.IsLeader())

		// can be started again after being stopped
		require.NoError(t, candidate.Start(ctx))
		assert.True(t, candidate.IsLeader())

		// Subsequent stop works as normal
		require.NoError(t, candidate.Stop(ctx))
		assert.False(t, candidate.IsLeader())
	})

	t.Run("multiple candidates", func(t *testing.T) {
		bElected := make(chan struct{}, 1)

		ctx := t.Context()
		name := election.Name(uuid.NewString())
		candidateA := electionSvc.NewCandidate(name, "host-1", 30*time.Second)
		candidateB := electionSvc.NewCandidate(name, "host-2", 30*time.Second, func(ctx context.Context) {
			bElected <- struct{}{}
		})

		t.Cleanup(func() {
			candidateA.Stop(context.Background())
			candidateB.Stop(context.Background())
		})

		require.NoError(t, candidateA.Start(ctx))
		assert.True(t, candidateA.IsLeader())

		require.NoError(t, candidateB.Start(ctx))
		assert.False(t, candidateB.IsLeader())

		// Candidate B should take over after we stop candidate A
		require.NoError(t, candidateA.Stop(ctx))
		assert.False(t, candidateA.IsLeader())

		// Block until B has claimed leadership or we time out
		select {
		case <-bElected:
			// B claimed leadership
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for candidate B to claim leadership")
		}
		assert.True(t, candidateB.IsLeader())

		require.NoError(t, candidateB.Stop(ctx))
		assert.False(t, candidateB.IsLeader())
	})
}
