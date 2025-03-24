package database

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type InstanceResources struct {
	Instance  *InstanceResource
	Resources []*resource.ResourceData
}

func NewInstanceResources(instance *InstanceResource, resources []resource.Resource) (*InstanceResources, error) {
	data := make([]*resource.ResourceData, len(resources))
	for i, res := range resources {
		d, err := resource.ToResourceData(res)
		if err != nil {
			return nil, fmt.Errorf("failed to convert resource to resource data: %w", err)
		}
		data[i] = d
	}

	return &InstanceResources{
		Instance:  instance,
		Resources: data,
	}, nil
}

type ConnectionInfo struct {
	AdminHost       string
	AdminPort       int
	PeerHost        string
	PeerPort        int
	PeerSSLCert     string
	PeerSSLKey      string
	PeerSSLRootCert string
	PatroniPort     int
}

type Orchestrator interface {
	GenerateInstanceResources(spec *InstanceSpec) (*InstanceResources, error)
	// ReadInstanceResource(ctx context.Context, instance *InstanceResource) (*InstanceResource, error)
	GetInstanceConnectionInfo(ctx context.Context, instance *InstanceResource) (*ConnectionInfo, error)
}
