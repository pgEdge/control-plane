package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
	// Credentials is only populated during provisioning workflows. It is not
	// persisted to etcd and will be nil when read from the store.
	Credentials *ServiceUser `json:"credentials,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Error       string       `json:"error,omitempty"`
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

// ServiceUser represents database credentials for a service instance.
//
// Each service instance receives two dedicated database users: one read-only (RO) and
// one read-write (RW). The active user is selected based on the service's allow_writes
// setting. This provides security isolation between service instances.
//
// # Credential Generation
//
// Credentials are generated during service instance provisioning by the CreateServiceUser
// workflow activity. The username is deterministic (based on service instance ID and
// mode), while the password is cryptographically random.
//
// # Security Properties
//
// - Unique per service instance (not shared between instances)
// - 32-character random passwords
// - Storage in etcd alongside service instance metadata
// - Injected into service containers via config.yaml
type ServiceUser struct {
	Username string `json:"username"` // Format: "svc_{service_id}_{mode}"
	Password string `json:"password"` // 32-character cryptographically random string
	Role     string `json:"role"`     // Database role, e.g., "pgedge_application_read_only" or "pgedge_application"
}

// GenerateServiceUsername creates a deterministic username for a service.
//
// # Username Format
//
// The username follows the pattern: "svc_{service_id}_{mode}"
//
// Example:
//
//	service_id: "mcp-server", mode: "ro"
//	Generated username: "svc_mcp_server_ro"
//
// # Rationale
//
// - "svc_" prefix: Clearly identifies service accounts vs. application users
// - service_id: Uniquely identifies the service within the database
// - mode: Distinguishes RO ("ro") from RW ("rw") users for the same service
// - Deterministic: Same service_id + mode always generates the same username
// - Shared: One database user role per service per mode, shared across all instances
//
// # Uniqueness
//
// Service IDs are unique within a database. By using the service_id and mode, we
// guarantee uniqueness even when multiple services exist on the same database.
//
// # PostgreSQL Compatibility
//
// PostgreSQL identifier length limit is 63 characters. For short names the full
// service_id is used directly. When the username exceeds 63 characters, the
// function appends an 8-character hex hash (from SHA-256 of the full untruncated
// name) to a truncated prefix. This guarantees uniqueness even when two inputs
// share a long common prefix.
//
// Short name format: svc_{service_id}_{mode}
// Long name format:  svc_{truncated service_id}_{8-hex-hash}_{mode}
func GenerateServiceUsername(serviceID string, mode string) string {
	// Sanitize hyphens to underscores for PostgreSQL compatibility.
	// Hyphens in identifiers require double-quoting in SQL.
	serviceID = strings.ReplaceAll(serviceID, "-", "_")
	username := fmt.Sprintf("svc_%s_%s", serviceID, mode)

	if len(username) <= 63 {
		return username
	}

	// Hash the full untruncated username for uniqueness
	h := sha256.Sum256([]byte(username))
	hashSuffix := hex.EncodeToString(h[:4]) // 8 hex chars

	// svc_ (4) + prefix + _ (1) + hash (8) + _ (1) + mode (len) = 14 + len(mode) + prefix
	// Max prefix = 63 - 14 - len(mode)
	maxPrefix := 63 - 14 - len(mode)
	raw := serviceID
	if len(raw) > maxPrefix {
		raw = raw[:maxPrefix]
	}

	return fmt.Sprintf("svc_%s_%s_%s", raw, hashSuffix, mode)
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
	Credentials        *ServiceUser
	DatabaseNetworkID  string
	NodeName           string             // Database node name (for ServiceUserRole PrimaryExecutor routing)
	DatabaseHosts      []ServiceHostEntry // Ordered list of Postgres host:port entries
	TargetSessionAttrs string             // libpq target_session_attrs value
	Port               *int               // Service instance published port (optional, 0 = random)
	DatabaseNodes      []*NodeInstances   // All database nodes; used to create per-node ServiceUserRole resources
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
