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

type switchoverTestOpts struct {
	// primaries are returned by GetPrimaryInstance in turn, then the last one.
	primaries []string
	// failUpdate is a task status whose update fails.
	failUpdate string
	// failLookup is an instance whose primary lookup fails.
	failLookup string
	// node is returned by GetNodeInstances. By default, the node has n1-a,
	// n1-b and n1-c, all on reachable hosts.
	node    *activities.GetNodeInstancesOutput
	nodeErr error
}

type switchoverTest struct {
	wt         tester.WorkflowTester[*SwitchoverOutput]
	start      time.Time
	events     []string
	taskLog    []string
	lookups    []string
	switchedAt time.Time
	switched   *activities.PerformSwitchoverInput
}

var testInstances = []*activities.InstanceHost{
	{InstanceID: "n1-a", HostID: "host-1"},
	{InstanceID: "n1-b", HostID: "host-2"},
	{InstanceID: "n1-c", HostID: "host-3"},
}

// newSwitchoverTest sets up the Switchover workflow with mocked activities.
func newSwitchoverTest(t *testing.T, opts switchoverTestOpts) *switchoverTest {
	t.Helper()

	w := &Workflows{Activities: &activities.Activities{}}
	st := &switchoverTest{wt: tester.NewWorkflowTester[*SwitchoverOutput](w.Switchover)}
	st.start = st.wt.Now()

	recordUpdate := func(args mock.Arguments) {
		update := args.Get(1).(*activities.UpdateTaskInput)
		st.events = append(st.events, string(*update.UpdateOptions.Status))
	}
	if opts.failUpdate != "" {
		failing := mock.MatchedBy(func(in *activities.UpdateTaskInput) bool {
			return string(*in.UpdateOptions.Status) == opts.failUpdate
		})
		st.wt.OnActivity(w.Activities.UpdateTask, mock.Anything, failing).
			Run(recordUpdate).
			Return((*activities.UpdateTaskOutput)(nil), errors.New("storage unavailable"))
	}
	st.wt.OnActivity(w.Activities.UpdateTask, mock.Anything, mock.Anything).
		Run(recordUpdate).
		Return(&activities.UpdateTaskOutput{}, nil)

	st.wt.OnActivity(w.Activities.LogTaskEvent, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			for _, entry := range args.Get(1).(*activities.LogTaskEventInput).Entries {
				st.taskLog = append(st.taskLog, entry.Message)
			}
		}).
		Return(&activities.LogTaskEventOutput{}, nil)

	recordLookup := func(args mock.Arguments) {
		st.lookups = append(st.lookups, args.Get(1).(*activities.GetPrimaryInstanceInput).InstanceID)
	}
	if opts.failLookup != "" {
		failing := mock.MatchedBy(func(in *activities.GetPrimaryInstanceInput) bool {
			return in.InstanceID == opts.failLookup
		})
		st.wt.OnActivity(w.Activities.GetPrimaryInstance, mock.Anything, failing).
			Run(recordLookup).
			Return((*activities.GetPrimaryInstanceOutput)(nil), errors.New("patroni unavailable"))
	}
	primaries := opts.primaries
	if len(primaries) == 0 {
		primaries = []string{"n1-a"}
	}
	for i, primary := range primaries {
		call := st.wt.OnActivity(w.Activities.GetPrimaryInstance, mock.Anything, mock.Anything).
			Run(recordLookup).
			Return(&activities.GetPrimaryInstanceOutput{PrimaryInstanceID: primary}, nil)
		if i < len(primaries)-1 {
			call.Once()
		}
	}

	node := opts.node
	if node == nil {
		node = &activities.GetNodeInstancesOutput{Instances: testInstances}
	}
	st.wt.OnActivity(w.Activities.GetNodeInstances, mock.Anything, mock.Anything).
		Return(node, opts.nodeErr)

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
	in.Instances = testInstances
	st.wt.Execute(context.Background(), in)
	require.True(t, st.wt.WorkflowFinished())
	_, err := st.wt.WorkflowResult()
	return err
}

func (st *switchoverTest) scheduled(delay time.Duration) time.Time {
	return st.start.Add(delay)
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
			st := newSwitchoverTest(t, switchoverTestOpts{})
			in := &SwitchoverInput{CandidateInstanceID: tc.candidate}
			if tc.delay > 0 {
				in.ScheduledAt = st.scheduled(tc.delay)
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
	st := newSwitchoverTest(t, switchoverTestOpts{})
	in := &SwitchoverInput{ScheduledAt: st.scheduled(-maxScheduledSwitchoverDelay - time.Minute)}

	err := st.run(t, in)
	require.Error(t, err)
	assert.ErrorContains(t, err, "switchover not performed")
	assert.Equal(t, []string{"running", "failed"}, st.events)
}

func TestSwitchoverPrimaryChangedBeforeScheduledTime(t *testing.T) {
	for _, candidate := range []string{"", "n1-c"} {
		t.Run("skipped with candidate "+candidate, func(t *testing.T) {
			st := newSwitchoverTest(t, switchoverTestOpts{primaries: []string{"n1-a", "n1-b"}})
			in := &SwitchoverInput{
				ScheduledAt:         st.scheduled(10 * time.Minute),
				CandidateInstanceID: candidate,
			}

			require.NoError(t, st.run(t, in))
			assert.Equal(t, []string{"running", "completed"}, st.events)
			assert.Equal(t, []string{
				"skipping switchover: the primary changed from n1-a to n1-b before the scheduled time",
			}, st.taskLog)
		})
	}

	t.Run("task left running is reported as failed", func(t *testing.T) {
		// If the skipped switchover can't be marked complete, the task must not
		// be left running, where it would block later switchovers.
		st := newSwitchoverTest(t, switchoverTestOpts{primaries: []string{"n1-a", "n1-b"}, failUpdate: "completed"})
		in := &SwitchoverInput{ScheduledAt: st.scheduled(10 * time.Minute)}

		err := st.run(t, in)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to mark task complete")
		assert.Equal(t, []string{"running", "completed", "failed"}, st.events)
	})
}

func TestSwitchoverCandidateAlreadyPrimary(t *testing.T) {
	st := newSwitchoverTest(t, switchoverTestOpts{})

	require.NoError(t, st.run(t, &SwitchoverInput{CandidateInstanceID: "n1-a"}))
	assert.Equal(t, []string{"running", "completed"}, st.events)
	assert.Equal(t, []string{"skipping switchover: candidate n1-a is already the primary"}, st.taskLog)
}

// After the wait, the switchover must use the node's current instances and
// hosts rather than those from when it was requested.
func TestSwitchoverNodeChangedDuringWait(t *testing.T) {
	scheduled := func(st *switchoverTest, candidate string) *SwitchoverInput {
		return &SwitchoverInput{
			ScheduledAt:         st.scheduled(10 * time.Minute),
			CandidateInstanceID: candidate,
		}
	}

	t.Run("database can't be modified", func(t *testing.T) {
		st := newSwitchoverTest(t, switchoverTestOpts{
			nodeErr: errors.New("database can't be modified in its current state: deleting"),
		})

		err := st.run(t, scheduled(st, ""))
		require.Error(t, err)
		assert.EqualError(t, err, "switchover not performed: database can't be modified in its current state: deleting")
		assert.Equal(t, []string{"running", "failed"}, st.events)
	})

	t.Run("candidate removed from the node", func(t *testing.T) {
		st := newSwitchoverTest(t, switchoverTestOpts{
			node: &activities.GetNodeInstancesOutput{Instances: testInstances[:2]},
		})

		err := st.run(t, scheduled(st, "n1-c"))
		require.Error(t, err)
		assert.EqualError(t, err, "switchover not performed: candidate instance n1-c is no longer an instance of node n1")
		assert.Equal(t, []string{"running", "failed"}, st.events)
	})

	t.Run("candidate on an unreachable host", func(t *testing.T) {
		st := newSwitchoverTest(t, switchoverTestOpts{
			node: &activities.GetNodeInstancesOutput{Instances: testInstances, UnreachableHosts: []string{"host-2"}},
		})

		err := st.run(t, scheduled(st, "n1-b"))
		require.Error(t, err)
		assert.EqualError(t, err, "switchover not performed: candidate instance n1-b is on unreachable host host-2")
		assert.Equal(t, []string{"running", "failed"}, st.events)
	})

	t.Run("primary on an unreachable host", func(t *testing.T) {
		st := newSwitchoverTest(t, switchoverTestOpts{
			node: &activities.GetNodeInstancesOutput{Instances: testInstances, UnreachableHosts: []string{"host-1"}},
		})

		err := st.run(t, scheduled(st, ""))
		require.Error(t, err)
		assert.EqualError(t, err, "switchover not performed: the primary n1-a is on unreachable host host-1")
		assert.Equal(t, []string{"running", "failed"}, st.events)
		// The lookup after the wait skips the unreachable host.
		assert.Equal(t, []string{"n1-a", "n1-b"}, st.lookups)
	})

	t.Run("lookup moves on when an instance doesn't answer", func(t *testing.T) {
		st := newSwitchoverTest(t, switchoverTestOpts{
			failLookup: "n1-b",
			node:       &activities.GetNodeInstancesOutput{Instances: testInstances[1:]},
			primaries:  []string{"n1-c"},
		})

		require.NoError(t, st.run(t, scheduled(st, "")))
		assert.Equal(t, []string{"n1-a", "n1-b", "n1-c"}, st.lookups,
			"n1-a before the wait; after it, n1-b fails and n1-c answers")
		require.NotNil(t, st.switched)
		assert.Equal(t, "n1-c", st.switched.LeaderInstanceID)
		assert.Equal(t, []string{"running", "switchover", "completed"}, st.events)
	})

	t.Run("uses the node's current instances", func(t *testing.T) {
		// n1-b and n1-c were replaced by n1-d during the wait.
		st := newSwitchoverTest(t, switchoverTestOpts{
			node: &activities.GetNodeInstancesOutput{Instances: []*activities.InstanceHost{
				{InstanceID: "n1-a", HostID: "host-1"},
				{InstanceID: "n1-d", HostID: "host-4"},
			}},
		})

		require.NoError(t, st.run(t, scheduled(st, "")))
		require.NotNil(t, st.switched)
		assert.Equal(t, "n1-d", st.switched.CandidateInstanceID)
		assert.Equal(t, []string{"running", "switchover", "completed"}, st.events)
	})
}
