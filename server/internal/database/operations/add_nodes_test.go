package operations_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestAddNode(t *testing.T) {
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
			// With two instances, we should produce one state with the first
			// instance and a second state with the other instance and the node
			// resource.
			name: "two instances",
			input: &operations.NodeResources{
				NodeName: "n1",
				InstanceResources: []*database.InstanceResources{
					instance1,
					instance2,
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						instance1.Instance,
						makeMonitorResource(instance1),
					},
					instance1.Resources,
				),
				makeState(t,
					[]resource.Resource{
						instance2.Instance,
						makeMonitorResource(instance2),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								instance1.InstanceID(),
								instance2.InstanceID(),
							},
						},
					},
					instance2.Resources,
				),
			},
		},
		{
			// With > 2 instances, we should still produce two states, but the
			// second state will have more than one instance. This means that
			// replica instances get created simultaneously.
			name: "three instances",
			input: &operations.NodeResources{
				NodeName: "n1",
				InstanceResources: []*database.InstanceResources{
					instance1,
					instance2,
					instance3,
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						instance1.Instance,
						makeMonitorResource(instance1),
					},
					instance1.Resources,
				),
				makeState(t,
					[]resource.Resource{
						instance2.Instance,
						makeMonitorResource(instance2),
						instance3.Instance,
						makeMonitorResource(instance3),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								instance1.InstanceID(),
								instance2.InstanceID(),
								instance3.InstanceID(),
							},
						},
					},
					slices.Concat(
						instance2.Resources,
						instance3.Resources,
					),
				),
			},
		},
		{
			name:        "no instances",
			input:       &operations.NodeResources{NodeName: "n1"},
			expectedErr: "got empty instances for node n1",
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
			out, err := operations.AddNode(tc.input)
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

func TestAddNodes(t *testing.T) {
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
			// This should look identical to the AddNode output.
			name: "one node with one instance",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
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
			// This should produce one state with both instances.
			name: "two nodes with one instance each",
			input: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
						&database.NodeResource{
							Name:        "n1",
							InstanceIDs: []string{n1Instance1.InstanceID()},
						},
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
			// This should produce two states where n1's replica and node are
			// added in the second state.
			name: "two nodes with one replica",
			input: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
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
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
					},
					slices.Concat(
						n1Instance2.Resources,
					),
				),
			},
		},
		{
			// This should produce two states where both n1 and n2's replicas
			// and nodes are added in the second state.
			name: "two nodes with two replicas",
			input: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName: "n2",
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
			expected: []*resource.State{
				makeState(t,
					[]resource.Resource{
						n1Instance1.Instance,
						makeMonitorResource(n1Instance1),
						n2Instance1.Instance,
						makeMonitorResource(n2Instance1),
					},
					slices.Concat(
						n1Instance1.Resources,
						n2Instance1.Resources,
					),
				),
				makeState(t,
					[]resource.Resource{
						n1Instance2.Instance,
						makeMonitorResource(n1Instance2),
						n2Instance2.Instance,
						makeMonitorResource(n2Instance2),
						&database.NodeResource{
							Name: "n1",
							InstanceIDs: []string{
								n1Instance1.InstanceID(),
								n1Instance2.InstanceID(),
							},
						},
						&database.NodeResource{
							Name: "n2",
							InstanceIDs: []string{
								n2Instance1.InstanceID(),
								n2Instance2.InstanceID(),
							},
						},
					},
					slices.Concat(
						n1Instance2.Resources,
						n2Instance2.Resources,
					),
				),
			},
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
			out, err := operations.AddNodes(tc.input)
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
