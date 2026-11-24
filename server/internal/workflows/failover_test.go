package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/cschleiden/go-workflows/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func TestFailover(t *testing.T) {
	instances := []*activities.InstanceHost{
		{InstanceID: "n1-a", HostID: "host-1"},
		{InstanceID: "n1-b", HostID: "host-2"},
		{InstanceID: "n1-c", HostID: "host-3"},
	}

	type leaderResult struct {
		leader string
		err    error
	}

	for _, tc := range []struct {
		name      string
		requested string
		// leaders is what the GetNodeLeader activity returns for each
		// instance.
		leaders map[string]leaderResult
		// expected is the PerformFailover input, or nil if it shouldn't be
		// called.
		expected *activities.PerformFailoverInput
		errMsg   string
	}{
		{
			name:      "leader with requested candidate",
			requested: "n1-b",
			leaders: map[string]leaderResult{
				"n1-b": {leader: "n1-a"},
			},
			expected: &activities.PerformFailoverInput{
				DatabaseID:          "db",
				LeaderInstanceID:    "n1-a",
				CandidateInstanceID: "n1-b",
			},
		},
		{
			name:      "leader reported by another instance",
			requested: "n1-b",
			leaders: map[string]leaderResult{
				"n1-a": {leader: "n1-c"},
				"n1-b": {err: errors.New("connection refused")},
			},
			expected: &activities.PerformFailoverInput{
				DatabaseID:          "db",
				LeaderInstanceID:    "n1-c",
				CandidateInstanceID: "n1-b",
			},
		},
		{
			name:      "no leader with requested candidate",
			requested: "n1-b",
			leaders: map[string]leaderResult{
				"n1-b": {},
			},
			expected: &activities.PerformFailoverInput{
				DatabaseID:          "db",
				CandidateInstanceID: "n1-b",
			},
		},
		{
			name: "no leader without requested candidate",
			leaders: map[string]leaderResult{
				"n1-a": {},
			},
			errMsg: "node n1 has no leader; a candidate instance is required to fail over",
		},
		{
			name: "no instance reports the leader",
			leaders: map[string]leaderResult{
				"n1-a": {err: errors.New("connection refused")},
				"n1-b": {err: errors.New("connection refused")},
				"n1-c": {err: errors.New("connection refused")},
			},
			errMsg: "failed to get the leader of node n1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &Workflows{Activities: &activities.Activities{}}

			wt := tester.NewWorkflowTester[*FailoverOutput](w.Failover)
			wt.OnActivity(w.Activities.UpdateTask, mock.Anything, mock.Anything).
				Return(&activities.UpdateTaskOutput{}, nil)
			for instanceID, result := range tc.leaders {
				var out *activities.GetNodeLeaderOutput
				if result.err == nil {
					out = &activities.GetNodeLeaderOutput{LeaderInstanceID: result.leader}
				}
				wt.OnActivity(w.Activities.GetNodeLeader, mock.Anything, mock.MatchedBy(func(in *activities.GetNodeLeaderInput) bool {
					return in.InstanceID == instanceID
				})).Return(out, result.err)
			}
			// The node has no Spock slots, so any candidate is ready.
			wt.OnActivity(w.Activities.GetSpockSlots, mock.Anything, mock.Anything).
				Return(&activities.GetSpockSlotsOutput{}, nil)
			var performed *activities.PerformFailoverInput
			wt.OnActivity(w.Activities.PerformFailover, mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					performed = args.Get(1).(*activities.PerformFailoverInput)
				}).
				Return(&activities.PerformFailoverOutput{}, nil)

			wt.Execute(context.Background(), &FailoverInput{
				DatabaseID:          "db",
				NodeName:            "n1",
				Instances:           instances,
				CandidateInstanceID: tc.requested,
				SkipValidation:      true,
			})

			require.True(t, wt.WorkflowFinished())
			_, err := wt.WorkflowResult()
			if tc.errMsg != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
				assert.Nil(t, performed)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, performed)
		})
	}
}
