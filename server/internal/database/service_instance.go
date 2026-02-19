package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pgEdge/control-plane/server/internal/host"
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
	Hostname     *string            `json:"hostname,omitempty"`
	IPv4Address  *string            `json:"ipv4_address,omitempty"`
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
// Each service instance receives dedicated database credentials with read-only access.
// This provides security isolation between service instances and prevents services from
// modifying database data. This may be relaxed or configurable in the future depending
// on use-case requirements.
//
// # Credential Generation
//
// Credentials are generated during service instance provisioning by the CreateServiceUser
// workflow activity. The username is deterministic (based on service instance ID), while
// the password is cryptographically random.
//
// # Security Properties
//
// - Unique per service instance (not shared between instances)
// - Read-only database access (SELECT + EXECUTE only, no DML/DDL)
// - 32-character random passwords
// - Storage in etcd alongside service instance metadata
// - Injected as environment variables into service containers
//
// # Usage
//
// Service containers receive credentials via environment variables:
//   - PGUSER: Username (e.g., "svc_db1mcp")
//   - PGPASSWORD: Password (32-character random string)
//
// The service connects to the database using these credentials, which are restricted
// to read-only operations via the "pgedge_application_read_only" role.
type ServiceUser struct {
	Username string `json:"username"` // Format: "svc_{first-8-chars-of-instance-id}"
	Password string `json:"password"` // 32-character cryptographically random string
	Role     string `json:"role"`     // Database role, e.g., "pgedge_application_read_only"
}

// GenerateServiceUsername creates a deterministic username for a service instance.
//
// # Username Format
//
// The username follows the pattern: "svc_{service_id}_{host_id}"
//
// Example:
//
//	service_id: "mcp-server", host_id: "host1"
//	Generated username: "svc_mcp-server_host1"
//
// # Rationale
//
// - "svc_" prefix: Clearly identifies service accounts vs. application users
// - service_id: Uniquely identifies the service within the database
// - host_id: Distinguishes service instances on different hosts
// - Deterministic: Same service_id + host_id always generates the same username
//
// # Uniqueness
//
// Service instance IDs are unique within a database (format: {db_id}-{service_id}-{host_id}).
// By using the full service_id and host_id, we guarantee uniqueness even when
// multiple services exist on the same database.
//
// # PostgreSQL Compatibility
//
// PostgreSQL identifier length limit is 63 characters. For short names the full
// service_id and host_id are used directly. When the combined username exceeds
// 63 characters, the function appends an 8-character hex hash (from SHA-256 of
// the full untruncated name) to a truncated prefix. This guarantees uniqueness
// even when two inputs share a long common prefix.
//
// Short name format: svc_{service_id}_{host_id}
// Long name format:  svc_{first 50 chars of service_id_host_id}_{8-hex-hash}
func GenerateServiceUsername(serviceID, hostID string) string {
	// Sanitize hyphens to underscores for PostgreSQL compatibility.
	// Hyphens in identifiers require double-quoting in SQL.
	serviceID = strings.ReplaceAll(serviceID, "-", "_")
	hostID = strings.ReplaceAll(hostID, "-", "_")
	username := fmt.Sprintf("svc_%s_%s", serviceID, hostID)

	if len(username) <= 63 {
		return username
	}

	// Hash the full untruncated username for uniqueness
	h := sha256.Sum256([]byte(username))
	suffix := hex.EncodeToString(h[:4]) // 8 hex chars

	// svc_ (4) + prefix (50) + _ (1) + hash (8) = 63
	raw := fmt.Sprintf("%s_%s", serviceID, hostID)
	if len(raw) > 50 {
		raw = raw[:50]
	}

	return fmt.Sprintf("svc_%s_%s", raw, suffix)
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

// ServiceInstanceSpec contains the specification for generating service instance resources.
type ServiceInstanceSpec struct {
	ServiceInstanceID string
	ServiceSpec       *ServiceSpec
	PgEdgeVersion     *host.PgEdgeVersion // Database version, used for compatibility validation
	DatabaseID        string
	DatabaseName      string
	HostID            string
	CohortMemberID    string
	Credentials       *ServiceUser
	DatabaseNetworkID string
	PostgresHostID    string // Host where Postgres instance runs (for ServiceUserRole executor routing)
	DatabaseHost      string // Postgres instance hostname to connect to
	DatabasePort      int    // Postgres instance port
	Port              *int   // Service instance published port (optional, 0 = random)
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
