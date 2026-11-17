package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/cschleiden/go-workflows/tester"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func TestMissingSlotCopies(t *testing.T) {
	slot := func(name string, temporary bool) postgres.SpockSlot {
		return postgres.SpockSlot{Name: name, Temporary: temporary, Usable: true}
	}
	invalidated := func(name string) postgres.SpockSlot {
		return postgres.SpockSlot{Name: name}
	}

	for _, tc := range []struct {
		name     string
		primary  []postgres.SpockSlot
		replica  []postgres.SpockSlot
		expected []string
	}{
		{
			name: "no slots on the primary",
		},
		{
			name:    "all copies persistent",
			primary: []postgres.SpockSlot{slot("a", false), slot("b", false)},
			replica: []postgres.SpockSlot{slot("a", false), slot("b", false)},
		},
		{
			name:     "copy still temporary",
			primary:  []postgres.SpockSlot{slot("a", false), slot("b", false)},
			replica:  []postgres.SpockSlot{slot("a", false), slot("b", true)},
			expected: []string{"b"},
		},
		{
			name:     "copy missing",
			primary:  []postgres.SpockSlot{slot("a", false), slot("b", false)},
			replica:  []postgres.SpockSlot{slot("b", false)},
			expected: []string{"a"},
		},
		{
			name:    "temporary slot on the primary is ignored",
			primary: []postgres.SpockSlot{slot("a", false), slot("tmp", true)},
			replica: []postgres.SpockSlot{slot("a", false)},
		},
		{
			name:    "extra slot on the replica is ignored",
			primary: []postgres.SpockSlot{slot("a", false)},
			replica: []postgres.SpockSlot{slot("a", false), slot("old", false)},
		},
		{
			name:     "invalidated copy on the replica",
			primary:  []postgres.SpockSlot{slot("a", false), slot("b", false)},
			replica:  []postgres.SpockSlot{slot("a", false), invalidated("b")},
			expected: []string{"b"},
		},
		{
			name:    "invalidated slot on the primary is ignored",
			primary: []postgres.SpockSlot{slot("a", false), invalidated("lost")},
			replica: []postgres.SpockSlot{slot("a", false)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, missingSlotCopies(tc.primary, tc.replica))
		})
	}
}

func TestCandidateOrder(t *testing.T) {
	instances := []*activities.InstanceHost{
		{InstanceID: "i1", HostID: "h1"},
		{InstanceID: "i2", HostID: "h2"},
		nil,
		{InstanceID: "i3", HostID: "h3"},
	}
	ids := func(insts []*activities.InstanceHost) []string {
		var out []string
		for _, inst := range insts {
			out = append(out, inst.InstanceID)
		}
		return out
	}

	assert.Equal(t, []string{"i3", "i2"}, ids(candidateOrder(instances, "i1", "", "i3")), "preferred first, leader excluded")
	assert.Equal(t, []string{"i2"}, ids(candidateOrder(instances, "i1", "i2", "")), "requested only")
	assert.Empty(t, candidateOrder(instances, "i1", "unknown", ""), "requested instance isn't in the node")
}

func TestChooseCandidate(t *testing.T) {
	persistent := []postgres.SpockSlot{{Name: "spk_db_n1_sub_n1_n2", Usable: true}}
	temporary := []postgres.SpockSlot{{Name: "spk_db_n1_sub_n1_n2", Temporary: true, Usable: true}}
	instances := []*activities.InstanceHost{
		{InstanceID: "n1-a", HostID: "host-1"},
		{InstanceID: "n1-b", HostID: "host-2"},
		{InstanceID: "n1-c", HostID: "host-3"},
	}

	type slotsResult struct {
		slots []postgres.SpockSlot
		err   error
		// times is how many calls return this result; 0 means every call.
		times int
	}

	for _, tc := range []struct {
		name      string
		requested string
		// selected is what the SelectCandidate activity returns.
		selected string
		slots    map[string][]slotsResult
		expected string
		errMsg   string
	}{
		{
			name:     "selected replica is ready",
			selected: "n1-b",
			slots: map[string][]slotsResult{
				"n1-a": {{slots: persistent}},
				"n1-b": {{slots: persistent}},
			},
			expected: "n1-b",
		},
		{
			name:     "selected replica isn't ready, another is",
			selected: "n1-b",
			slots: map[string][]slotsResult{
				"n1-a": {{slots: persistent}},
				"n1-b": {{slots: temporary}},
				"n1-c": {{slots: persistent}},
			},
			expected: "n1-c",
		},
		{
			name:      "requested replica becomes ready",
			requested: "n1-c",
			slots: map[string][]slotsResult{
				"n1-a": {{slots: persistent}},
				"n1-c": {{slots: temporary, times: 3}, {slots: persistent}},
			},
			expected: "n1-c",
		},
		{
			name:      "requested replica never becomes ready",
			requested: "n1-c",
			slots: map[string][]slotsResult{
				"n1-a": {{slots: persistent}},
				"n1-c": {{slots: temporary}},
			},
			errMsg: "candidate instance n1-c has no persistent copy of spock replication slot(s) spk_db_n1_sub_n1_n2 after 2m0s",
		},
		{
			name:     "no replica becomes ready",
			selected: "n1-b",
			slots: map[string][]slotsResult{
				"n1-a": {{slots: persistent}},
				"n1-b": {{slots: temporary}},
				"n1-c": {{err: errors.New("connection refused")}},
			},
			errMsg: "no replica of node n1 has persistent copies of all spock replication slots after 2m0s",
		},
		{
			name:     "primary has no spock slots",
			selected: "n1-b",
			slots: map[string][]slotsResult{
				"n1-a": {{}},
				"n1-b": {{}},
			},
			expected: "n1-b",
		},
		{
			name:     "primary is unreachable",
			selected: "n1-b",
			slots: map[string][]slotsResult{
				"n1-a": {{err: errors.New("connection refused")}},
			},
			expected: "n1-b",
		},
		{
			name:      "requested instance is the leader",
			requested: "n1-a",
			expected:  "n1-a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &Workflows{Activities: &activities.Activities{}}

			wf := func(ctx workflow.Context) (string, error) {
				return w.chooseCandidate(ctx, workflow.Logger(ctx), &chooseCandidateInput{
					DatabaseID:       "db",
					NodeName:         "n1",
					Instances:        instances,
					LeaderInstanceID: "n1-a",
					LeaderHostID:     "host-1",
					Requested:        tc.requested,
				})
			}

			wt := tester.NewWorkflowTester[string](wf)
			if tc.selected != "" {
				wt.OnActivity(w.Activities.SelectCandidate, mock.Anything, mock.Anything).
					Return(&activities.SelectCandidateOutput{CandidateInstanceID: tc.selected}, nil)
			}
			for instanceID, results := range tc.slots {
				matchInstance := mock.MatchedBy(func(in *activities.GetSpockSlotsInput) bool {
					return in.InstanceID == instanceID
				})
				for _, result := range results {
					var out *activities.GetSpockSlotsOutput
					if result.err == nil {
						out = &activities.GetSpockSlotsOutput{Slots: result.slots}
					}
					call := wt.OnActivity(w.Activities.GetSpockSlots, mock.Anything, matchInstance).
						Return(out, result.err)
					if result.times > 0 {
						call.Times(result.times)
					}
				}
			}

			wt.Execute(context.Background())

			require.True(t, wt.WorkflowFinished())
			candidate, err := wt.WorkflowResult()
			if tc.errMsg != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, candidate)
		})
	}
}
