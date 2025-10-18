package database

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

type InstanceState string

const (
	InstanceStateCreating  InstanceState = "creating"
	InstanceStateModifying InstanceState = "modifying"
	InstanceStateBackingUp InstanceState = "backing_up"
	InstanceStateAvailable InstanceState = "available"
	InstanceStateDegraded  InstanceState = "degraded"
	InstanceStateFailed    InstanceState = "failed"
	InstanceStateStopped   InstanceState = "stopped"
	InstanceStateUnknown   InstanceState = "unknown"
)

var modifyingStates = ds.NewSet(
	patroni.StateStopping,
	patroni.StateStopped,
	patroni.StateStarting,
	patroni.StateRestarting,
	patroni.StateInitializingNewCluster,
	patroni.StateRunningCustomBootstrapScript,
	patroni.StateCreatingReplica,
)

var degradedStates = ds.NewSet(
	patroni.StateStopFailed,
	patroni.StateCrashed,
	patroni.StateStartFailed,
	patroni.StateRestartFailed,
	patroni.StateInitDBFailed,
	patroni.StateCustomBootstrapFailed,
)

func patroniToInstanceState(state *patroni.State) InstanceState {
	if state == nil {
		return InstanceStateUnknown
	}
	switch {
	case modifyingStates.Has(*state):
		return InstanceStateModifying
	case degradedStates.Has(*state):
		return InstanceStateDegraded
	case *state == patroni.StateRunning:
		return InstanceStateAvailable
	default:
		return InstanceStateUnknown
	}
}

type Instance struct {
	InstanceID string          `json:"instance_id"`
	DatabaseID string          `json:"database_id"`
	HostID     string          `json:"host_id"`
	NodeName   string          `json:"node_name"`
	State      InstanceState   `json:"state"`
	Status     *InstanceStatus `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Error      string          `json:"error,omitempty"`
}

type SubscriptionStatus struct {
	ProviderNode string `json:"provider_node"`
	Name         string `json:"name"`
	Status       string `json:"status"`
}

type InstanceStatus struct {
	PostgresVersion *string               `json:"postgres_version,omitempty"`
	SpockVersion    *string               `json:"spock_version,omitempty"`
	Hostname        *string               `json:"hostname,omitempty"`
	IPv4Address     *string               `json:"ipv4_address,omitempty"`
	Port            *int                  `json:"port,omitempty"`
	PatroniState    *patroni.State        `json:"patroni_state,omitempty"`
	Role            *patroni.InstanceRole `json:"role,omitempty"`
	ReadOnly        *string               `json:"read_only,omitempty"`
	PendingRestart  *bool                 `json:"pending_restart,omitempty"`
	PatroniPaused   *bool                 `json:"patroni_paused,omitempty"`
	StatusUpdatedAt *time.Time            `json:"status_updated_at,omitempty"`
	Stopped         *bool                 `json:"stopped,omitempty"`
	Subscriptions   []SubscriptionStatus  `json:"subscriptions,omitempty"`
	Error           *string               `json:"error,omitempty"`
}

func (s *InstanceStatus) IsPrimary() bool {
	return s.Role != nil && *s.Role == patroni.InstanceRolePrimary
}

func storedToInstance(instance *StoredInstance, status *StoredInstanceStatus) *Instance {
	if instance == nil {
		return nil
	}
	out := &Instance{
		InstanceID: instance.InstanceID,
		DatabaseID: instance.DatabaseID,
		HostID:     instance.HostID,
		NodeName:   instance.NodeName,
		State:      instance.State,
		CreatedAt:  instance.CreatedAt,
		UpdatedAt:  instance.UpdateAt,
		Error:      instance.Error,
	}
	if status != nil {
		out.Status = status.Status
	}

	// We want to infer the instance state if the instance is supposed to be
	// available.
	if out.State == InstanceStateAvailable && status != nil {
		if status.Status.Stopped != nil && *status.Status.Stopped {
			out.State = InstanceStateStopped
			instance.State = InstanceStateStopped
		} else {
			out.State = patroniToInstanceState(status.Status.PatroniState)
		}
	}

	return out
}

func storedToInstances(storedInstances []*StoredInstance, storedStatuses []*StoredInstanceStatus, nodeName string) []*Instance {
	statusesByID := make(map[string]*StoredInstanceStatus, len(storedStatuses))
	for _, s := range storedStatuses {
		statusesByID[s.InstanceID] = s
	}

	instances := make([]*Instance, len(storedInstances))
	for idx, stored := range storedInstances {
		if nodeName != "" && stored.NodeName != nodeName {
			continue
		}
		status := statusesByID[stored.InstanceID]
		instance := storedToInstance(stored, status)
		instances[idx] = instance
	}

	return instances
}
