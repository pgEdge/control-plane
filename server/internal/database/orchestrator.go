package database

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const pgEdgeUser = "pgedge"

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
	AdminHost         string
	AdminPort         int
	PeerHost          string
	PeerPort          int
	PeerSSLCert       string
	PeerSSLKey        string
	PeerSSLRootCert   string
	PatroniPort       int
	ClientHost        string
	ClientIPv4Address string
	ClientPort        int
	InstanceHostname  string
}

func (c *ConnectionInfo) PatroniURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", c.AdminHost, c.PatroniPort),
	}
}

func (c *ConnectionInfo) AdminDSN(dbName string) *postgres.DSN {
	return &postgres.DSN{
		Host:   c.AdminHost,
		Port:   c.AdminPort,
		DBName: dbName,
		User:   pgEdgeUser,
	}
}

func (c *ConnectionInfo) PeerDSN(dbName string) *postgres.DSN {
	return &postgres.DSN{
		Host:        c.PeerHost,
		Port:        c.PeerPort,
		DBName:      dbName,
		User:        pgEdgeUser,
		SSLCert:     c.PeerSSLCert,
		SSLKey:      c.PeerSSLKey,
		SSLRootCert: c.PeerSSLRootCert,
	}
}

type Orchestrator interface {
	GenerateInstanceResources(spec *InstanceSpec) (*InstanceResources, error)
	GenerateInstanceRestoreResources(spec *InstanceSpec, taskID uuid.UUID) (*InstanceResources, error)
	GetInstanceConnectionInfo(ctx context.Context, databaseID, instanceID uuid.UUID) (*ConnectionInfo, error)
	CreatePgBackRestBackup(ctx context.Context, w io.Writer, instanceID uuid.UUID, options *pgbackrest.BackupOptions) error
}
