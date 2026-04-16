package database

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const pgEdgeUser = "pgedge"

// ResourceTypeServiceInstance is the resource type identifier for service instances.
// This constant is defined here to avoid import cycles between the orchestrator
// and workflow packages.
const ResourceTypeServiceInstance = "swarm.service_instance"

type InstanceResources struct {
	Instance             *InstanceResource
	Resources            []*resource.ResourceData
	DatabaseDependencies []*resource.ResourceData
}

func NewInstanceResources(
	instance *InstanceResource,
	resources []resource.Resource,
	databaseDependencies []resource.Resource,
) (*InstanceResources, error) {
	inst := &InstanceResources{
		Instance: instance,
	}
	if err := inst.AddResources(resources...); err != nil {
		return nil, err
	}
	if err := inst.AddDatabaseDependencies(databaseDependencies...); err != nil {
		return nil, err
	}

	return inst, nil
}

func (r *InstanceResources) DatabaseDependencyIdentifiers() []resource.Identifier {
	ids := make([]resource.Identifier, len(r.DatabaseDependencies))
	for i, dep := range r.DatabaseDependencies {
		ids[i] = dep.Identifier
	}

	return ids
}

func (r *InstanceResources) AddResources(resources ...resource.Resource) error {
	resourceDataSlice, err := resource.ToResourceDataSlice(resources...)
	if err != nil {
		return fmt.Errorf("failed to convert instance resources: %w", err)
	}
	r.Resources = append(r.Resources, resourceDataSlice...)

	return nil
}

func (r *InstanceResources) AddDatabaseDependencies(resources ...resource.Resource) error {
	databaseDataSlice, err := resource.ToResourceDataSlice(resources...)
	if err != nil {
		return fmt.Errorf("failed to convert database dependency resources: %w", err)
	}
	r.DatabaseDependencies = append(r.DatabaseDependencies, databaseDataSlice...)

	return nil
}

func (r *InstanceResources) InstanceID() string {
	return r.Instance.Spec.InstanceID
}

func (r *InstanceResources) DatabaseID() string {
	return r.Instance.Spec.DatabaseID
}

func (r *InstanceResources) HostID() string {
	return r.Instance.Spec.HostID
}

func (r *InstanceResources) DatabaseName() string {
	return r.Instance.Spec.DatabaseName
}

func (r *InstanceResources) NodeName() string {
	return r.Instance.Spec.NodeName
}

func (r *InstanceResources) State() (*resource.State, error) {
	state := resource.NewState()
	state.Add(r.Resources...)

	if err := state.AddResource(r.Instance); err != nil {
		return nil, fmt.Errorf("failed to add instance to state: %w", err)
	}

	return state, nil
}

type ServiceInstanceResources struct {
	ServiceInstance *ServiceInstance
	Resources       []*resource.ResourceData
}

type ValidationResult struct {
	InstanceID string   `json:"instance_id"`
	HostID     string   `json:"host_id"`
	NodeName   string   `json:"node_name"`
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors"`
}

type ConnectionInfo struct {
	AdminHost        string
	AdminPort        int
	PeerHost         string
	PeerPort         int
	PeerSSLCert      string
	PeerSSLKey       string
	PeerSSLRootCert  string
	PatroniPort      int
	ClientAddresses  []string
	ClientPort       int
	InstanceHostname string
}

func (c *ConnectionInfo) PatroniURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(c.AdminHost, strconv.Itoa(c.PatroniPort)),
	}
}

func (c *ConnectionInfo) AdminDSN(dbName string) *postgres.DSN {
	return &postgres.DSN{
		Hosts:           []string{c.AdminHost},
		Ports:           []int{c.AdminPort},
		DBName:          dbName,
		User:            pgEdgeUser,
		ApplicationName: "control-plane",
	}
}

type Orchestrator interface {
	GenerateInstanceResources(spec *InstanceSpec, scripts Scripts) (*InstanceResources, error)
	GenerateInstanceRestoreResources(spec *InstanceSpec, taskID uuid.UUID) (*InstanceResources, error)
	GenerateServiceInstanceResources(spec *ServiceInstanceSpec) (*ServiceInstanceResources, error)
	GetInstanceConnectionInfo(ctx context.Context,
		databaseID, instanceID string,
		postgresPort, patroniPort *int,
		pgEdgeVersion *ds.PgEdgeVersion) (*ConnectionInfo, error)
	GetServiceInstanceStatus(ctx context.Context, serviceInstanceID string) (*ServiceInstanceStatus, error)
	CreatePgBackRestBackup(ctx context.Context, w io.Writer, spec *InstanceSpec, options *pgbackrest.BackupOptions) error
	ExecuteInstanceCommand(ctx context.Context, w io.Writer, databaseID, instanceID string, args ...string) error
	ValidateInstanceSpecs(ctx context.Context, changes []*InstanceSpecChange) ([]*ValidationResult, error)
	StopInstance(ctx context.Context, instanceID string) error
	StartInstance(ctx context.Context, instanceID string) error
	NodeDSN(ctx context.Context, rc *resource.Context, nodeName string, fromInstanceID string, dbName string) (*postgres.DSN, error)
}
