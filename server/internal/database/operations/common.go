package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

type NodeResources struct {
	DatabaseID        string
	DatabaseOwner     string
	DatabaseName      string
	NodeName          string
	SourceNode        string
	PrimaryInstanceID string
	InstanceResources []*database.InstanceResources
	RestoreConfig     *database.RestoreConfig
	Scripts           database.Scripts
}

func (n *NodeResources) primaryInstance() *database.InstanceResources {
	for _, instance := range n.InstanceResources {
		if instance.InstanceID() == n.PrimaryInstanceID {
			return instance
		}
	}

	return nil
}

func (n *NodeResources) nodeResourceState() (*resource.State, error) {
	var instanceIDs []string
	state := resource.NewState()
	for _, instance := range n.InstanceResources {
		instanceIDs = append(instanceIDs, instance.InstanceID())
	}

	err := state.AddResource(&database.NodeResource{
		Name:        n.NodeName,
		InstanceIDs: instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add node resources to state: %w", err)
	}

	return state, nil
}

func (n *NodeResources) databaseResourceState() (*resource.State, error) {
	hasRestoreConfig := n.RestoreConfig != nil

	var renameFrom string
	if hasRestoreConfig {
		renameFrom = n.RestoreConfig.SourceDatabaseName
	}

	db := &database.PostgresDatabaseResource{
		DatabaseID:         n.DatabaseID,
		NodeName:           n.NodeName,
		DatabaseName:       n.DatabaseName,
		Owner:              n.DatabaseOwner,
		RenameFrom:         renameFrom,
		HasRestoreConfig:   hasRestoreConfig,
		PostDatabaseCreate: n.Scripts[database.ScriptNamePostDatabaseCreate],
	}

	state := resource.NewState()
	for _, instance := range n.InstanceResources {
		err := state.AddResource(&monitor.InstanceMonitorResource{
			DatabaseID:   instance.DatabaseID(),
			InstanceID:   instance.InstanceID(),
			HostID:       instance.HostID(),
			DatabaseName: instance.DatabaseName(),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add instance monitor resource to state: %w", err)
		}
		for _, dep := range instance.DatabaseDependencies {
			db.ExtraDependencies = append(db.ExtraDependencies, dep.Identifier)
			state.Add(dep)
		}
	}

	err := state.AddResource(db)
	if err != nil {
		return nil, fmt.Errorf("failed to add database resources to state: %w", err)
	}

	return state, nil
}

func instanceState(inst *database.InstanceResources) (*resource.State, error) {
	state, err := inst.State()
	if err != nil {
		return nil, fmt.Errorf("failed to compute updated instance state: %w", err)
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

// ServiceResources represents the resources for a single service instance.
type ServiceResources struct {
	ServiceInstanceID string
	Resources         []*resource.ResourceData
	MonitorResource   resource.Resource
}

func (s *ServiceResources) State() (*resource.State, error) {
	state := resource.NewState()
	state.Add(s.Resources...)
	if s.MonitorResource != nil {
		if err := state.AddResource(s.MonitorResource); err != nil {
			return nil, err
		}
	}
	return state, nil
}
