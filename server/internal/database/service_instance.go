package database

import (
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

type ServiceInstanceState string

const (
	ServiceInstanceStateCreating ServiceInstanceState = "creating"
	ServiceInstanceStateRunning  ServiceInstanceState = "running"
	ServiceInstanceStateFailed   ServiceInstanceState = "failed"
	ServiceInstanceStateDeleting ServiceInstanceState = "deleting"
)

type ServiceInstance struct {
	ServiceInstanceID string                 `json:"service_instance_id"`
	ServiceID         string                 `json:"service_id"`
	DatabaseID        string                 `json:"database_id"`
	HostID            string                 `json:"host_id"`
	State             ServiceInstanceState   `json:"state"`
	Status            *ServiceInstanceStatus `json:"status,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	Error             string                 `json:"error,omitempty"`
}

type ServiceInstanceStatus struct {
	ContainerID  *string            `json:"container_id,omitempty"`
	ImageVersion *string            `json:"image_version,omitempty"`
	Addresses    []string           `json:"addresses,omitempty"`
	Ports        []PortMapping      `json:"ports,omitempty"`
	HealthCheck  *HealthCheckResult `json:"health_check,omitempty"`
	LastHealthAt *time.Time         `json:"last_health_at,omitempty"`
	ServiceReady *bool              `json:"service_ready,omitempty"`
}

type PortMapping struct {
	Name          string `json:"name"`
	ContainerPort int    `json:"container_port"`
	HostPort      *int   `json:"host_port,omitempty"`
}

type HealthCheckResult struct {
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// GenerateServiceInstanceID creates a unique ID for a service instance.
// Format: {database_id}-{service_id}-{host_id}
func GenerateServiceInstanceID(databaseID, serviceID, hostID string) string {
	return fmt.Sprintf("%s-%s-%s", databaseID, serviceID, hostID)
}

// GenerateDatabaseNetworkID creates the overlay network ID for a database.
// Format: {database_id}
func GenerateDatabaseNetworkID(databaseID string) string {
	return databaseID
}

// CopyPortFrom copies the port from the current (persisted) spec to this spec,
// retaining any previously allocated stable random port. This mirrors the
// reconcilePort logic used by InstanceSpec.CopySettingsFrom.
func (s *ServiceInstanceSpec) CopyPortFrom(current *ServiceInstanceSpec) {
	s.Port = reconcilePort(current.Port, s.Port)
}

// ServiceInstanceSpec contains the specification for generating service instance resources.
type ServiceInstanceSpec struct {
	ServiceInstanceID  string
	ServiceSpec        *ServiceSpec
	PgEdgeVersion      *ds.PgEdgeVersion // Database version, used for compatibility validation
	DatabaseID         string
	DatabaseName       string
	HostID             string
	CohortMemberID     string
	DatabaseNetworkID  string
	NodeName           string             // Database node name for PrimaryExecutor routing
	DatabaseHosts      []ServiceHostEntry // Ordered list of Postgres host:port entries
	TargetSessionAttrs string             // libpq target_session_attrs value
	Port               *int               // Service instance published port (optional, 0 = random)
	DatabaseNodes      []*NodeInstances   // All database nodes; used to create per-node resources
	ConnectAsUsername  string             // Username from database_users (resolved from ServiceSpec.ConnectAs)
	ConnectAsPassword  string             // Password from database_users (resolved from ServiceSpec.ConnectAs)
}

// storedToServiceInstance converts stored service instance and status to ServiceInstance.
func storedToServiceInstance(serviceInstance *StoredServiceInstance, status *StoredServiceInstanceStatus) *ServiceInstance {
	if serviceInstance == nil {
		return nil
	}
	out := &ServiceInstance{
		ServiceInstanceID: serviceInstance.ServiceInstanceID,
		ServiceID:         serviceInstance.ServiceID,
		DatabaseID:        serviceInstance.DatabaseID,
		HostID:            serviceInstance.HostID,
		State:             serviceInstance.State,
		CreatedAt:         serviceInstance.CreatedAt,
		UpdatedAt:         serviceInstance.UpdatedAt,
		Error:             serviceInstance.Error,
	}
	if status != nil {
		out.Status = status.Status
	}

	return out
}

// storedToServiceInstances converts arrays of stored service instances and statuses to ServiceInstance array.
func storedToServiceInstances(storedServiceInstances []*StoredServiceInstance, storedStatuses []*StoredServiceInstanceStatus) []*ServiceInstance {
	statusesByID := make(map[string]*StoredServiceInstanceStatus, len(storedStatuses))
	for _, s := range storedStatuses {
		statusesByID[s.ServiceInstanceID] = s
	}

	serviceInstances := make([]*ServiceInstance, len(storedServiceInstances))
	for idx, stored := range storedServiceInstances {
		status := statusesByID[stored.ServiceInstanceID]
		serviceInstance := storedToServiceInstance(stored, status)
		serviceInstances[idx] = serviceInstance
	}

	return serviceInstances
}
