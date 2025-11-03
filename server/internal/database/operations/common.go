package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

type NodeResources struct {
	NodeName          string
	SourceNode        string
	PrimaryInstanceID string
	InstanceResources []*database.InstanceResources
	RestoreConfig     *database.RestoreConfig
}

func (n *NodeResources) primaryInstance() *database.InstanceResources {
	for _, instance := range n.InstanceResources {
		if instance.InstanceID() == n.PrimaryInstanceID {
			return instance
		}
	}

	return nil
}

func addNodeResource(states []*resource.State, resource *database.NodeResource) error {
	// Add the node resource to the last state
	err := states[len(states)-1].AddResource(resource)
	if err != nil {
		return fmt.Errorf("failed to add node resource to state: %w", err)
	}
	return nil
}

func instanceState(inst *database.InstanceResources) (*resource.State, error) {
	state, err := inst.State()
	if err != nil {
		return nil, fmt.Errorf("failed to compute updated instance state: %w", err)
	}
	err = state.AddResource(&monitor.InstanceMonitorResource{
		DatabaseID:   inst.DatabaseID(),
		InstanceID:   inst.InstanceID(),
		HostID:       inst.HostID(),
		DatabaseName: inst.DatabaseName(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add instance monitor to state: %w", err)
	}
	return state, nil
}

func mergePartialStates(in [][]*resource.State) []*resource.State {
	var out []*resource.State

	for _, states := range in {
		for i, state := range states {
			if i >= len(out) {
				out = append(out, state)
			} else {
				out[i].Merge(state)
			}
		}
	}

	return out
}
