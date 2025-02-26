package database

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

type NetworkType string

const (
	NetworkTypeDocker NetworkType = "docker"
	NetworkTypeHost   NetworkType = "host"
)

type InstanceInterface struct {
	NetworkType NetworkType `json:"network_type"`
	NetworkID   string      `json:"network_id"`
	Hostname    string      `json:"hostname"`
	IPv4Address string      `json:"ipv4_address"`
	Port        int         `json:"port"`
}

type InstanceState string

const (
	InstanceStateCreating  InstanceState = "creating"
	InstanceStateModifying InstanceState = "modifying"
	InstanceStateBackingUp InstanceState = "backing_up"
	InstanceStateRestoring InstanceState = "restoring"
	InstanceStateDeleting  InstanceState = "deleting"
	InstanceStateAvailable InstanceState = "available"
	InstanceStateDegraded  InstanceState = "degraded"
	InstanceStateUnknown   InstanceState = "unknown"
)

type Instance struct {
	InstanceID      uuid.UUID            `json:"instance_id"`
	TenantID        *uuid.UUID           `json:"tenant_id,omitempty"`
	DatabaseID      uuid.UUID            `json:"database_id"`
	HostID          uuid.UUID            `json:"host_id"`
	ReplicaOfID     uuid.UUID            `json:"replica_of_id,omitempty"`
	DatabaseName    string               `json:"database_name"`
	NodeName        string               `json:"node_name"`
	ReplicaName     string               `json:"replica_name,omitempty"`
	PostgresVersion string               `json:"postgres_version"`
	SpockVersion    string               `json:"spock_version"`
	Port            int                  `json:"port"`
	State           InstanceState        `json:"state"`
	PatroniState    patroni.State        `json:"patroni_state"`
	Role            patroni.InstanceRole `json:"role"`
	ReadOnly        bool                 `json:"read_only"`
	PendingRestart  bool                 `json:"pending_restart"`
	PatroniPaused   bool                 `json:"patroni_paused"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Interfaces      []*InstanceInterface `json:"interfaces"`
	Spec            *InstanceSpec        `json:"spec"`
}

func instanceToStored(i *Instance) *StoredInstance {
	return &StoredInstance{
		InstanceID:      i.InstanceID,
		DatabaseID:      i.DatabaseID,
		HostID:          i.HostID,
		ReplicaOfID:     i.ReplicaOfID,
		TenantID:        i.TenantID,
		DatabaseName:    i.DatabaseName,
		NodeName:        i.NodeName,
		ReplicaName:     i.ReplicaName,
		PostgresVersion: i.PostgresVersion,
		SpockVersion:    i.SpockVersion,
		Port:            i.Port,
		State:           i.State,
		PatroniState:    i.PatroniState,
		Role:            i.Role,
		ReadOnly:        i.ReadOnly,
		PendingRestart:  i.PendingRestart,
		PatroniPaused:   i.PatroniPaused,
		UpdatedAt:       i.UpdatedAt,
		Interfaces:      i.Interfaces,
	}
}

func storedToInstance(i *StoredInstance, spec *InstanceSpec) *Instance {
	return &Instance{
		InstanceID:      i.InstanceID,
		DatabaseID:      i.DatabaseID,
		HostID:          i.HostID,
		ReplicaOfID:     i.ReplicaOfID,
		DatabaseName:    i.DatabaseName,
		NodeName:        i.NodeName,
		ReplicaName:     i.ReplicaName,
		PostgresVersion: i.PostgresVersion,
		TenantID:        i.TenantID,
		SpockVersion:    i.SpockVersion,
		Port:            i.Port,
		State:           i.State,
		PatroniState:    i.PatroniState,
		Role:            i.Role,
		ReadOnly:        i.ReadOnly,
		PendingRestart:  i.PendingRestart,
		PatroniPaused:   i.PatroniPaused,
		UpdatedAt:       i.UpdatedAt,
		Interfaces:      i.Interfaces,
		Spec:            spec,
	}
}
