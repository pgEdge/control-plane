//go:build e2e_test

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/require"
)

func TestSwitchoverScenarios(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_switchover",
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
				role := *inst.Postgres.Role
				if role == "" {
					role = "unknown"
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

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n1",
			// no candidate specified => server picks best replica
		})
		require.NoError(t, err, "switchover (auto) API call failed")

		require.Truef(t, waitForPrimaryChange(origPrimary, 60*time.Second),
			"[auto] primary did not change within timeout (still %s)", origPrimary)
		newPrimary := getPrimaryInstanceID()
		t.Logf("[auto] new primary: %s", newPrimary)
	})

	t.Run("switchover to a specific candidate", func(t *testing.T) {
		currentPrimary := getPrimaryInstanceID()

		candidateInst := waitForReadyReplica(60 * time.Second)
		require.NotEmpty(t, candidateInst, "[specific] no ready replica available to switchover to")
		require.NotEqual(t, currentPrimary, candidateInst, "[specific] picked primary as candidate unexpectedly")
		t.Logf("[specific] current primary: %s, candidate (ready): %s", currentPrimary, candidateInst)

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &candidateInst,
		})
		require.NoError(t, err, "switchover (specific) API call failed")

		require.Truef(t, waitForPrimaryIs(candidateInst, 75*time.Second),
			"[specific] primary did not become %s within timeout (current %s)",
			candidateInst, getPrimaryInstanceID())
		t.Logf("[specific] new primary confirmed: %s", candidateInst)
	})

	t.Run("scheduled switchover", func(t *testing.T) {
		time.Sleep(10 * time.Second)

		origPrimary := getPrimaryInstanceID()
		candidate := waitForReadyReplica(90 * time.Second)
		require.NotEmpty(t, candidate)
		require.NotEqual(t, origPrimary, candidate)

		// Compute skew and schedule using server time.
		hostNow := time.Now().UTC().Truncate(time.Second)
		srvNow := serverNowUTC(t, fixture.APIBaseURL()) // e.g., "http://<cp-host>:3000"
		skew := srvNow.Sub(hostNow)                     // positive means server is ahead

		scheduledAt := srvNow.Add(2 * time.Minute).Truncate(time.Second)
		scheduledAtStr := scheduledAt.Format(time.RFC3339)

		db.Refresh(ctx)
		t.Logf("[scheduled] hostNow=%s serverNow=%s skew=%s scheduled_at=%s (delta=%s)",
			hostNow.Format(time.RFC3339), srvNow.Format(time.RFC3339),
			skew, scheduledAtStr, time.Until(scheduledAt).Truncate(time.Second))

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID:          controlplane.Identifier(db.ID),
			NodeName:            "n1",
			ScheduledAt:         &scheduledAtStr,
			CandidateInstanceID: &candidate,
		})
		require.NoError(t, err, "switchover (scheduled) API call failed")

		// Wait until scheduledAt + 4m (min 5m), computed from *server* time.
		waitBudget := time.Until(scheduledAt.Add(skew)) + 4*time.Minute
		if waitBudget < 5*time.Minute {
			waitBudget = 5 * time.Minute
		}

		require.Truef(t, waitForPrimaryIs(candidate, waitBudget),
			"[scheduled] primary did not become %s within %s (current %s; scheduledAt=%s; skew=%s). "+
				"Hint: ensure scheduler is running and compares due_at <= server-now (UTC).",
			candidate, waitBudget, getPrimaryInstanceID(), scheduledAtStr, skew)

		t.Logf("[scheduled] new primary confirmed: %s", getPrimaryInstanceID())
	})

	t.Run("invalid candidate instance", func(t *testing.T) {
		badID := "invalid-instance-" + uuid.NewString()
		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &badID,
		})
		require.Error(t, err, "expected error for invalid candidate instance id")
		t.Logf("[invalid] got expected error: %v", err)
	})

	t.Run("concurrent switchover requests", func(t *testing.T) {
		// Start first request
		done := make(chan struct{})
		go func() {
			_ = db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
				DatabaseID: dbID,
				NodeName:   "n1",
			})
			close(done)
		}()

		// Lets the first request get going
		time.Sleep(500 * time.Millisecond)

		// Second request may fail with "already in progress" or succeed if the first finished quickly
		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n1",
		})
		if err == nil {
			t.Log("[concurrent] second request succeeded (first likely completed quickly)")
		} else {
			t.Logf("[concurrent] second request returned expected error: %v", err)
		}

		<-done
	})
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func serverNowUTC(t *testing.T, baseURL string) time.Time {
	req, err := http.NewRequest(http.MethodHead, baseURL+"/v1/version", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	date := resp.Header.Get("Date")
	require.NotEmpty(t, date, "server did not return Date header")
	srvNow, err := time.Parse(time.RFC1123, date)
	require.NoError(t, err, "failed to parse server Date header")
	return srvNow.UTC().Truncate(time.Second)
}
