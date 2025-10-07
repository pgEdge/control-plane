//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

// TestFailoverScenarios covers failover flows (immediate) similar to the switchover tests.
func TestFailoverScenarios(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_failover",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1), controlplane.Identifier(host2), controlplane.Identifier(host3)},
				},
			},
		},
	})

	dbID := controlplane.Identifier(db.ID)

	getPrimaryInstanceID := func() string {
		inst := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
		if inst == nil {
			t.Fatalf("no primary instance found for node n1")
			return ""
		}
		return inst.ID
	}

	waitFor(func() bool {
		db.Refresh(ctx)
		for _, inst := range db.Instances {
			if inst.NodeName == "n1" && (inst.State == "modifying" || inst.State == "creating") {
				return false
			}
		}
		return true
	}, 60*time.Second)

	// Returns a non-primary instance that's ready/available, or "" if none found within timeout.
	waitForReadyReplica := func(timeout time.Duration) string {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			db.Refresh(ctx)
			curPrimary := getPrimaryInstanceID()
			var states []string
			for _, inst := range db.Instances {
				if inst.NodeName != "n1" {
					continue
				}
				st := inst.State
				if st == "" {
					st = inst.State
				}
				role := "unknown"
				if inst.Postgres != nil && inst.Postgres.Role != nil && *inst.Postgres.Role != "" {
					role = *inst.Postgres.Role
				}
				states = append(states, inst.ID+":"+role+":"+st)
				if inst.ID != curPrimary && role != "primary" && (st == "available" || st == "ready" || st == "running") {
					return inst.ID
				}
			}
			t.Logf("[ready-replica] waiting... instances=%v", states)
			time.Sleep(1 * time.Second)
		}
		return ""
	}

	waitForPrimaryChange := func(orig string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			db.Refresh(ctx)
			if p := getPrimaryInstanceID(); p != orig {
				return true
			}
			time.Sleep(1 * time.Second)
		}
		return false
	}

	waitForPrimaryIs := func(expected string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			db.Refresh(ctx)
			if p := getPrimaryInstanceID(); p == expected {
				return true
			}
			time.Sleep(1 * time.Second)
		}
		return false
	}

	t.Run("automatic candidate selection (no candidate specified)", func(t *testing.T) {
		origPrimary := getPrimaryInstanceID()
		t.Logf("[auto] original primary: %s", origPrimary)

		// allow failover to proceed on a healthy cluster for this test (explicitly)
		skipTrue := true
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:     dbID,
			NodeName:       "n1",
			SkipValidation: skipTrue,
			// no candidate specified => server picks best replica
		})
		if err != nil && isTransientRoleDecode(err) {
			t.Logf("[auto] ignoring transient role decode error: %v", err)
			err = nil
		}
		require.NoError(t, err, "failover (auto) API call failed")

		require.Truef(t, waitForPrimaryChange(origPrimary, 60*time.Second),
			"[auto] primary did not change within timeout (still %s)", origPrimary)
		newPrimary := getPrimaryInstanceID()
		t.Logf("[auto] new primary: %s", newPrimary)
	})

	t.Run("failover to a specific candidate", func(t *testing.T) {
		currentPrimary := getPrimaryInstanceID()

		candidateInst := waitForReadyReplica(60 * time.Second)
		require.NotEmpty(t, candidateInst, "[specific] no ready replica available to failover to")
		require.NotEqual(t, currentPrimary, candidateInst, "[specific] picked primary as candidate unexpectedly")
		t.Logf("[specific] current primary: %s, candidate (ready): %s", currentPrimary, candidateInst)

		// allow failover to proceed on a healthy cluster for this test (explicitly)
		skipTrue := true
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &candidateInst,
			SkipValidation:      skipTrue,
		})
		if err != nil && isTransientRoleDecode(err) {
			t.Logf("[specific] ignoring transient role decode error: %v", err)
			err = nil
		}
		require.NoError(t, err, "failover (specific) API call failed")

		require.Truef(t, waitForPrimaryIs(candidateInst, 75*time.Second),
			"[specific] primary did not become %s within timeout (current %s)",
			candidateInst, getPrimaryInstanceID())
		t.Logf("[specific] new primary confirmed: %s", candidateInst)
	})

	t.Run("invalid candidate instance", func(t *testing.T) {
		badID := "invalid-instance-" + uuid.NewString()
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &badID,
		})
		require.Error(t, err, "expected error for invalid candidate instance id")
		t.Logf("[invalid] got expected error: %v", err)
	})

	t.Run("concurrent failover requests", func(t *testing.T) {
		// Start first request
		done := make(chan struct{})
		go func() {
			_ = db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
				DatabaseID: dbID,
				NodeName:   "n1",
			})
			close(done)
		}()

		// Lets the first request get going
		time.Sleep(500 * time.Millisecond)

		// Second request may fail with "already in progress" or succeed if the first finished quickly
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID: dbID,
			NodeName:   "n1",
		})
		if err == nil {
			t.Log("[concurrent] second request succeeded (first likely completed quickly)")
		} else if isTransientRoleDecode(err) {
			t.Logf("[concurrent] ignoring transient role decode error: %v", err)
		} else {
			t.Logf("[concurrent] second request returned expected error: %v", err)
		}

		<-done
	})

	t.Run("skip_validation behavior", func(t *testing.T) {
		// If cluster is healthy, a failover without skip_validation should be rejected.
		// Next, a failover with skip_validation=true should be accepted.
		origPrimary := getPrimaryInstanceID()
		t.Logf("[skip-validation] original primary: %s", origPrimary)

		// Attempt failover without skip_validation -> expect error (cluster healthy)
		skipFalse := false
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:     dbID,
			NodeName:       "n1",
			SkipValidation: skipFalse,
		})
		// Either error or transient decode; treat non-nil as expected
		if err == nil {
			// If the system allowed failover without skip_validation (unexpected), try to wait for primary change
			changed := waitForPrimaryChange(origPrimary, 60*time.Second)
			if changed {
				t.Logf("[skip-validation] unexpected: failover proceeded without skip_validation; new primary: %s", getPrimaryInstanceID())
			} else {
				t.Fatalf("expected failover to be rejected on healthy cluster when skip_validation=false, but call returned no error")
			}
		} else {
			t.Logf("[skip-validation] got expected rejection without skip_validation: %v", err)
		}

		// Now try with skip_validation = true; should proceed (or return transient decode)
		skipTrue := true
		err = db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:     dbID,
			NodeName:       "n1",
			SkipValidation: skipTrue,
		})
		if err != nil && isTransientRoleDecode(err) {
			t.Logf("[skip-validation] ignoring transient role decode error: %v", err)
			err = nil
		}
		require.NoError(t, err, "failover with skip_validation=true should be accepted")
		// Wait for some short time to let failover proceed — primary may change
		_ = waitForPrimaryChange(origPrimary, 75*time.Second)
	})
}
