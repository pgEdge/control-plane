//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// TestScheduledRestartInstance covers PLAT-203: a scheduled restart's task
// must stay open (pending/running) until the restart actually happens, and
// it must be cancellable while it's waiting.
func TestScheduledRestartInstance(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_scheduled_restart",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
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
		},
	})

	instance := db.GetInstance(WithNode("n1"))
	require.NotNil(t, instance, "database has no instance on n1")
	instanceID := instance.ID

	// postmasterStartTime queries pg_postmaster_start_time() directly, so
	// tests can independently verify whether Postgres actually restarted
	// rather than trusting the task status alone.
	postmasterStartTime := func(t *testing.T) time.Time {
		t.Helper()
		conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
			InstanceID: instanceID,
			Username:   "admin",
			Password:   "password",
		})
		require.NoError(t, err)
		defer conn.Close(ctx)

		var ts time.Time
		err = conn.QueryRow(ctx, "select pg_postmaster_start_time()").Scan(&ts)
		require.NoError(t, err)
		return ts
	}

	// scheduleRestart schedules a restart `dur` from now (server time) and
	// returns the resulting task plus the schedule/skew info needed to size a
	// wait budget against server time.
	scheduleRestart := func(t *testing.T, dur time.Duration) (task *controlplane.Task, scheduledAt time.Time, skew time.Duration) {
		hostNow := time.Now().UTC().Truncate(time.Second)
		srvNow := serverNowUTC(t, fixture.APIBaseURL())
		skew = srvNow.Sub(hostNow)

		scheduledAt = srvNow.Add(dur).Truncate(time.Second)
		scheduledAtStr := scheduledAt.Format(time.RFC3339)

		resp, err := fixture.Client.RestartInstance(t.Context(), &controlplane.RestartInstancePayload{
			DatabaseID:  controlplane.Identifier(db.ID),
			InstanceID:  instanceID,
			ScheduledAt: &scheduledAtStr,
		})
		require.NoError(t, err, "scheduled restart API call failed")

		return resp.Task, scheduledAt, skew
	}

	t.Run("stays open until the scheduled restart completes", func(t *testing.T) {
		baseline := postmasterStartTime(t)

		task, scheduledAt, skew := scheduleRestart(t, 90*time.Second)

		// Core regression check: the task must not be reported as completed
		// the instant Patroni acknowledges the schedule.
		immediate, err := fixture.Client.GetDatabaseTask(ctx, &controlplane.GetDatabaseTaskPayload{
			DatabaseID: controlplane.Identifier(db.ID),
			TaskID:     task.TaskID,
		})
		require.NoError(t, err)
		require.Containsf(t, []string{client.TaskStatusPending, client.TaskStatusRunning}, immediate.Status,
			"task closed (%s) immediately after scheduling; it should stay open until the restart completes", immediate.Status)

		waitBudget := time.Until(scheduledAt.Add(skew)) + 4*time.Minute
		if waitBudget < 4*time.Minute {
			waitBudget = 4 * time.Minute
		}
		waitCtx, cancel := context.WithTimeout(ctx, waitBudget)
		defer cancel()

		final, err := fixture.Client.WaitForDatabaseTask(waitCtx, &controlplane.GetDatabaseTaskPayload{
			DatabaseID: controlplane.Identifier(db.ID),
			TaskID:     task.TaskID,
		})
		require.NoError(t, err)
		require.Equal(t, client.TaskStatusCompleted, final.Status)

		// The task saying "completed" isn't enough on its own -- confirm
		// Postgres actually restarted.
		after := postmasterStartTime(t)
		require.Truef(t, after.After(baseline),
			"task reported completed, but pg_postmaster_start_time did not change (still %s); the restart never happened",
			after)
	})

	t.Run("can be cancelled while waiting for the scheduled time", func(t *testing.T) {
		baseline := postmasterStartTime(t)

		task, scheduledAt, skew := scheduleRestart(t, 40*time.Second)

		// Mirrors the sleep workaround in cancel_task_test.go: cancelling
		// immediately after scheduling is occasionally flaky.
		time.Sleep(500 * time.Millisecond)

		_, err := fixture.Client.CancelDatabaseTask(ctx, &controlplane.CancelDatabaseTaskPayload{
			DatabaseID: controlplane.Identifier(db.ID),
			TaskID:     controlplane.Identifier(task.TaskID),
		})
		require.NoError(t, err, "cancel API call failed")

		final, err := fixture.Client.WaitForDatabaseTask(ctx, &controlplane.GetDatabaseTaskPayload{
			DatabaseID: controlplane.Identifier(db.ID),
			TaskID:     task.TaskID,
		})
		require.NoError(t, err)
		require.Equal(t, client.TaskStatusCanceled, final.Status)

		// The task saying "canceled" isn't enough on its own -- a task can
		// close out without Patroni ever actually dropping the schedule.
		// Wait past the original scheduled time (plus a buffer) and confirm
		// Postgres never restarted.
		deadline := scheduledAt.Add(skew).Add(20 * time.Second)
		if remaining := time.Until(deadline); remaining > 0 {
			time.Sleep(remaining)
		}
		after := postmasterStartTime(t)
		require.Equalf(t, baseline, after,
			"postmaster start time changed after cancellation (baseline %s, now %s); the scheduled restart still fired in Patroni",
			baseline, after)
	})
}
