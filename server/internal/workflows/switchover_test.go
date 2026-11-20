package workflows

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cschleiden/go-workflows/tester"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type switchoverTest struct {
	wt         tester.WorkflowTester[*SwitchoverOutput]
	start      time.Time
	events     []string
	switchedAt time.Time
	switched   *activities.PerformSwitchoverInput
}

// newSwitchoverTest sets up the Switchover workflow with mocked activities.
// Task updates to the failUpdate status, if any, fail. GetPrimaryInstance
// returns each of primaries in turn, then the last one.
func newSwitchoverTest(t *testing.T, failUpdate string, primaries ...string) *switchoverTest {
	t.Helper()

	w := &Workflows{Activities: &activities.Activities{}}
	st := &switchoverTest{wt: tester.NewWorkflowTester[*SwitchoverOutput](w.Switchover)}
	st.start = st.wt.Now()

	recordUpdate := func(args mock.Arguments) {
		update := args.Get(1).(*activities.UpdateTaskInput)
		st.events = append(st.events, string(*update.UpdateOptions.Status))
	}
	if failUpdate != "" {
		failing := mock.MatchedBy(func(in *activities.UpdateTaskInput) bool {
			return string(*in.UpdateOptions.Status) == failUpdate
		})
		st.wt.OnActivity(w.Activities.UpdateTask, mock.Anything, failing).
			Run(recordUpdate).
			Return((*activities.UpdateTaskOutput)(nil), errors.New("storage unavailable"))
	}
	st.wt.OnActivity(w.Activities.UpdateTask, mock.Anything, mock.Anything).
		Run(recordUpdate).
		Return(&activities.UpdateTaskOutput{}, nil)
	for i, primary := range primaries {
		call := st.wt.OnActivity(w.Activities.GetPrimaryInstance, mock.Anything, mock.Anything).
			Return(&activities.GetPrimaryInstanceOutput{PrimaryInstanceID: primary}, nil)
		if i < len(primaries)-1 {
			call.Once()
		}
	}
	st.wt.OnActivity(w.Activities.SelectCandidate, mock.Anything, mock.Anything).
		Return(&activities.SelectCandidateOutput{CandidateInstanceID: "n1-b"}, nil)
	st.wt.OnActivity(w.Activities.GetSpockSlots, mock.Anything, mock.Anything).
		Return(&activities.GetSpockSlotsOutput{}, nil)
	st.wt.OnActivity(w.Activities.PerformSwitchover, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			st.switchedAt = st.wt.Now()
			st.switched = args.Get(1).(*activities.PerformSwitchoverInput)
			st.events = append(st.events, "switchover")
		}).
		Return(&activities.PerformSwitchoverOutput{}, nil)

	return st
}

func (st *switchoverTest) run(t *testing.T, in *SwitchoverInput) error {
	t.Helper()
	in.DatabaseID = "db"
	in.NodeName = "n1"
	in.TaskID = uuid.New()
	in.Instances = []*activities.InstanceHost{
		{InstanceID: "n1-a", HostID: "host-1"},
		{InstanceID: "n1-b", HostID: "host-2"},
		{InstanceID: "n1-c", HostID: "host-3"},
	}
	st.wt.Execute(context.Background(), in)
	require.True(t, st.wt.WorkflowFinished())
	_, err := st.wt.WorkflowResult()
	return err
}

// PLAT-284: a scheduled switchover must keep its task open until the
// switchover has happened, rather than completing once it's been scheduled.
func TestSwitchoverTaskStaysOpen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delay     time.Duration
		candidate string
	}{
		{name: "immediate", candidate: "n1-b"},
		{name: "scheduled", delay: 10 * time.Minute, candidate: "n1-b"},
		{name: "scheduled without a candidate", delay: 10 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newSwitchoverTest(t, "", "n1-a")
			in := &SwitchoverInput{CandidateInstanceID: tc.candidate}
			if tc.delay > 0 {
				in.ScheduledAt = st.start.Add(tc.delay)
			}

			require.NoError(t, st.run(t, in))
			assert.Equal(t, []string{"running", "switchover", "completed"}, st.events)
			assert.GreaterOrEqual(t, st.switchedAt.Sub(st.start), tc.delay,
				"the switchover must not happen before the scheduled time")

			// Patroni gets an immediate switchover away from the current
			// primary.
			require.NotNil(t, st.switched)
			assert.Equal(t, "n1-a", st.switched.LeaderInstanceID)
			assert.Equal(t, "n1-b", st.switched.CandidateInstanceID)
			assert.True(t, st.switched.ScheduledAt.IsZero())
			if tc.delay > 0 {
				assert.WithinDuration(t, in.ScheduledAt.Add(maxScheduledSwitchoverDelay), st.switched.Deadline, 0)
			} else {
				assert.True(t, st.switched.Deadline.IsZero(), "immediate switchovers have no deadline")
			}
		})
	}
}

func TestSwitchoverMissedScheduledTime(t *testing.T) {
	// The workflow runs long after the scheduled time, as it would if the
	// Control Plane had been down at that time.
	st := newSwitchoverTest(t, "", "n1-a")
	in := &SwitchoverInput{ScheduledAt: st.start.Add(-maxScheduledSwitchoverDelay - time.Minute)}

	err := st.run(t, in)
	require.Error(t, err)
	assert.ErrorContains(t, err, "switchover not performed")
	assert.Equal(t, []string{"running", "failed"}, st.events)
}

func TestSwitchoverPrimaryChangedBeforeScheduledTime(t *testing.T) {
	t.Run("skipped without a requested candidate", func(t *testing.T) {
		st := newSwitchoverTest(t, "", "n1-a", "n1-b")
		in := &SwitchoverInput{ScheduledAt: st.start.Add(10 * time.Minute)}

		require.NoError(t, st.run(t, in))
		assert.Equal(t, []string{"running", "completed"}, st.events)
	})

	t.Run("performed with a requested candidate", func(t *testing.T) {
		st := newSwitchoverTest(t, "", "n1-a", "n1-b")
		in := &SwitchoverInput{
			ScheduledAt:         st.start.Add(10 * time.Minute),
			CandidateInstanceID: "n1-c",
		}

		require.NoError(t, st.run(t, in))
		assert.Equal(t, []string{"running", "switchover", "completed"}, st.events)
	})
	t.Run("task left running is reported as failed", func(t *testing.T) {
		// If the skipped switchover can't be marked complete, the task must not
		// be left running, where it would block later switchovers.
		st := newSwitchoverTest(t, "completed", "n1-a", "n1-b")
		in := &SwitchoverInput{ScheduledAt: st.start.Add(10 * time.Minute)}

		err := st.run(t, in)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to mark task complete")
		assert.Equal(t, []string{"running", "completed", "failed"}, st.events)
	})
}
