package patroni_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestClusterState(t *testing.T) {
	t.Run("MostAlignedReplica", func(t *testing.T) {
		for _, tc := range []struct {
			name           string
			state          *patroni.ClusterState
			expectedMember patroni.ClusterMember
			expectedOk     bool
		}{
			{
				name: "three eligible",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
						{
							Name:  utils.PointerTo("instance-2"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(4)),
						},
						{
							Name:  utils.PointerTo("instance-3"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(3)),
						},
						{
							Name:  utils.PointerTo("instance-4"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(5)),
						},
					},
				},
				expectedMember: patroni.ClusterMember{
					Name:  utils.PointerTo("instance-3"),
					Role:  utils.PointerTo(patroni.ClusterRoleReplica),
					State: utils.PointerTo(patroni.StateStreaming),
					Lag:   utils.PointerTo(patroni.Lag(3)),
				},
				expectedOk: true,
			},
			{
				name: "one eligible",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
						{
							Name:  utils.PointerTo("instance-2"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(4)),
						},
						{
							Name:  utils.PointerTo("instance-3"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateCrashed),
							Lag:   utils.PointerTo(patroni.Lag(3)),
						},
						{
							Name:  utils.PointerTo("instance-4"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateRestartFailed),
							Lag:   utils.PointerTo(patroni.Lag(5)),
						},
					},
				},
				expectedMember: patroni.ClusterMember{
					Name:  utils.PointerTo("instance-2"),
					Role:  utils.PointerTo(patroni.ClusterRoleReplica),
					State: utils.PointerTo(patroni.StateStreaming),
					Lag:   utils.PointerTo(patroni.Lag(4)),
				},
				expectedOk: true,
			},
			{
				name: "no replicas",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
					},
				},
				expectedMember: patroni.ClusterMember{},
				expectedOk:     false,
			},
			{
				name: "nil lag",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
						{
							Name:  utils.PointerTo("instance-2"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
						},
					},
				},
				expectedMember: patroni.ClusterMember{},
				expectedOk:     false,
			},
			{
				name: "unknown lag",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
						{
							Name:  utils.PointerTo("instance-2"),
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(-1)),
						},
					},
				},
				expectedMember: patroni.ClusterMember{},
				expectedOk:     false,
			},
			{
				name: "nil name",
				state: &patroni.ClusterState{
					Members: []patroni.ClusterMember{
						{
							Name:  utils.PointerTo("instance-1"),
							Role:  utils.PointerTo(patroni.ClusterRoleLeader),
							State: utils.PointerTo(patroni.StateRunning),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
						{
							Role:  utils.PointerTo(patroni.ClusterRoleReplica),
							State: utils.PointerTo(patroni.StateStreaming),
							Lag:   utils.PointerTo(patroni.Lag(0)),
						},
					},
				},
				expectedMember: patroni.ClusterMember{},
				expectedOk:     false,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				member, ok := tc.state.MostAlignedReplica()
				assert.Equal(t, tc.expectedMember, member)
				assert.Equal(t, tc.expectedOk, ok)
			})
		}
	})
}
