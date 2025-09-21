package operations_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestUpdateNode(t *testing.T) {
	instance1 := makeInstance(t, "n1", 1)
	instance2 := makeInstance(t, "n1", 2)
	instance3 := makeInstance(t, "n1", 3)

	for _, tc := range []struct {
		name        string
		input       *operations.NodeResources
		expected    []*resource.State
		expectedErr string
	}{
		{
			// When there's one instance, we should produce one state with the
			// instance and the node resource.
			name: "one instance",
			input: &operations.NodeResources{
				NodeName:          "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{instance1},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						instance1.Instance,
						makeMonitorResource(instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{instance1.InstanceID()},
						},
					},
					instance1.Resources,
				),
			},
		},
		{
			// With two instances, we should produce one state with the replica
			// instance and a second state with the primary instance and the
			// node resource.
			name: "two instances",
			input: &operations.NodeResources{
				NodeName:          "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{
					instance1,
					instance2,
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						instance2.Instance,
						makeMonitorResource(instance2),
					},
					instance2.Resources,
				),
				makeState(t,
					[]resource.Resource{
						instance1.Instance,
						makeMonitorResource(instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								instance1.InstanceID(),
								instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     instance1.HostID(),
							InstanceID: instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					instance1.Resources,
				),
			},
		},
		{
			// With 3 instances, we should produce three states, where the last
			// state contains the primary instance and the node resource.
			name: "three instances",
			input: &operations.NodeResources{
				NodeName:          "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{
					instance1,
					instance2,
					instance3,
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						instance2.Instance,
						makeMonitorResource(instance2),
					},
					instance2.Resources,
				),
				makeState(t,
					[]resource.Resource{
						instance3.Instance,
						makeMonitorResource(instance3),
					},
					instance3.Resources,
				),
				makeState(t,
					[]resource.Resource{
						instance1.Instance,
						makeMonitorResource(instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								instance1.InstanceID(),
								instance2.InstanceID(),
								instance3.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     instance1.HostID(),
							InstanceID: instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					instance1.Resources,
				),
			},
		},
		{
			// TODO(PLAT-240): we need to decide how to handle this case. For
			// now, this produces an error to avoid breaking downstream
			// components.
			name: "no primary",
			input: &operations.NodeResources{
				NodeName: "n1",
				InstanceResources: []*database.InstanceResources{
					instance1,
				},
			},
			expectedErr: "node n1 has no primary instance",
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
			out, err := operations.UpdateNode(tc.input)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}

func TestRollingUpdateNodes(t *testing.T) {
	n1Instance1 := makeInstance(t, "n1", 1)
	n1Instance2 := makeInstance(t, "n1", 2)
	n2Instance1 := makeInstance(t, "n2", 1)
	n2Instance2 := makeInstance(t, "n2", 2)

	for _, tc := range []struct {
		name        string
		input       []*operations.NodeResources
		expected    []*resource.State
		expectedErr string
	}{
		{
			// This should look identical to the UpdateNode output.
			name: "one node with one instance",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{n1Instance1.InstanceID()},
						},
					},
					n1Instance1.Resources,
				),
			},
		},
		{
			// This should produce two states with one instance in each.
			name: "two nodes with one instance each",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{n1Instance1.InstanceID()},
						},
					},
					n1Instance1.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name:        "n2",
							InstanceIDs: []string{n2Instance1.InstanceID()},
						},
					},
					n2Instance1.Resources,
				),
			},
		},
		{
			// This should produce three states: n1's replica instance, n1's
			// primary instance and node resource, n2's instance and node
			// resource.
			name: "two nodes with one replica",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
					},
					n1Instance2.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     n1Instance1.HostID(),
							InstanceID: n1Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					n1Instance1.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name:        "n2",
							InstanceIDs: []string{n2Instance1.InstanceID()},
						},
					},
					n2Instance1.Resources,
				),
			},
		},
		{
			// This should produce four states: n1's replica, n1's primary +
			// node, n2's replica, n2's primary + node.
			name: "two nodes with two replicas",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
					},
					n1Instance2.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     n1Instance1.HostID(),
							InstanceID: n1Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					n1Instance1.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n2Instance2.Instance,
						makeMonitorResource(n2Instance2),
					},
					n2Instance2.Resources,
				),
				makeState(t,
					[]resource.Resource{
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name: "n2",
							InstanceIDs: []string{
								n2Instance1.InstanceID(),
								n2Instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     n2Instance1.HostID(),
							InstanceID: n2Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					n2Instance1.Resources,
				),
			},
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
			out, err := operations.RollingUpdateNodes(tc.input)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}

func TestConcurrentUpdateNodes(t *testing.T) {
	n1Instance1 := makeInstance(t, "n1", 1)
	n1Instance2 := makeInstance(t, "n1", 2)
	n2Instance1 := makeInstance(t, "n2", 1)
	n2Instance2 := makeInstance(t, "n2", 2)

	for _, tc := range []struct {
		name        string
		input       []*operations.NodeResources
		expected    []*resource.State
		expectedErr string
	}{
		{
			// This should look identical to the UpdateNode output.
			name: "one node with one instance",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{n1Instance1.InstanceID()},
						},
					},
					n1Instance1.Resources,
				),
			},
		},
		{
			// This should produce one state with both instances/nodes.
			name: "two nodes with one instance each",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{n1Instance1.InstanceID()},
						},
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name:        "n2",
							InstanceIDs: []string{n2Instance1.InstanceID()},
						},
					},
					slices.Concat(
						n1Instance1.Resources,
						n2Instance1.Resources,
					),
				),
			},
		},
		{
			// This should produce two states: n1's replica instance with n2's
			// primary instance + node, followed by n1's primary instance and
			// node resource.
			name: "two nodes with one replica",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name:        "n2",
							InstanceIDs: []string{n2Instance1.InstanceID()},
						},
					},
					slices.Concat(
						n1Instance2.Resources,
						n2Instance1.Resources,
					),
				),
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     n1Instance1.HostID(),
							InstanceID: n1Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					n1Instance1.Resources,
				),
			},
		},
		{
			// This should produce two states: n1's replica and n2's replica,
			// followed by n1's primary + node and n2's primary + node.
			name: "two nodes with two replicas",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
						n2Instance2.Instance,
						makeMonitorResource(n2Instance2),
					},
					slices.Concat(
						n1Instance2.Resources,
						n2Instance2.Resources,
					),
				),
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name: "n2",
							InstanceIDs: []string{
								n2Instance1.InstanceID(),
								n2Instance2.InstanceID(),
							},
						},
						&database.SwitchoverResource{
							HostID:     n1Instance1.HostID(),
							InstanceID: n1Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
						&database.SwitchoverResource{
							HostID:     n2Instance1.HostID(),
							InstanceID: n2Instance1.InstanceID(),
							TargetRole: patroni.InstanceRolePrimary,
						},
					},
					slices.Concat(
						n1Instance1.Resources,
						n2Instance1.Resources,
					),
				),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := operations.ConcurrentUpdateNodes(tc.input)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}
