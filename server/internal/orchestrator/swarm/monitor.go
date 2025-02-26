package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

type Monitor struct {
	spec *database.InstanceSpec
	// instanceID    uuid.UUID
	containerID   string
	ipAddress     string
	docker        *docker.Docker
	patroniClient *patroni.Client
	certSvc       *certificates.Service
	conn          *pgx.Conn
}

func (m *Monitor) refreshContainerInfo(ctx context.Context) error {
	matches, err := m.docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			// Multiple filters get AND'd together
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", m.spec.InstanceID.String())),
			filters.Arg("label", fmt.Sprintf("pgedge.component=%s", "postgres")),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("no postgres container found for %q", m.spec.InstanceID.String())
	}
	match := matches[0]
	m.containerID = match.ID

	// The bridge network will be the only network with a gateway.
	for _, network := range match.NetworkSettings.Networks {
		if network.Gateway != "" {
			m.ipAddress = network.IPAddress
			break
		}
	}

	return nil
}

// func (m *Monitor) refreshConnection(ctx context.Context) error {
// 	if m.conn != nil {
// 		m.conn.Close(ctx)
// 	}

// 	tlsConfig, err := m.certSvc.PostgresUserTLS(ctx, m.spec.InstanceID, "pgedge")
// 	if err != nil {
// 		return fmt.Errorf("failed to get TLS config: %w", err)
// 	}

// 	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
// 		Host:   m.ipAddress,
// 		Port:   5432, // This is the container's port. Should always be 5432.
// 		DBName: "postgres",
// 		TLS:    tlsConfig,
// 	})
// 	if err != nil {
// 		return fmt.Errorf("failed to get connection: %w", err)
// 	}
// 	m.conn = conn

// 	return nil
// }

func (m *Monitor) refreshPatroniClient() {
	m.patroniClient = patroni.NewClient(fmt.Sprintf("http://%s:8888", m.ipAddress), nil) // Use the default client
}

func (m *Monitor) Instance(ctx context.Context) (*database.Instance, error) {
	// m.conn.Exec(ctx, )

	return &database.Instance{
		InstanceID:   m.spec.InstanceID,
		TenantID:     m.spec.TenantID,
		DatabaseID:   m.spec.DatabaseID,
		HostID:       m.spec.HostID,
		ReplicaOfID:  m.spec.ReplicaOfID,
		DatabaseName: m.spec.DatabaseName,
		NodeName:     m.spec.NodeName,
		ReplicaName:  m.spec.ReplicaName,
		// PostgresVersion: m.spec.,
		Port: m.spec.Port,

		UpdatedAt: time.Now(),
	}, nil
}

func GetPostgresContainer(ctx context.Context, dockerClient *docker.Docker, instanceID uuid.UUID) (types.Container, error) {
	matches, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			// Multiple filters get AND'd together
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", instanceID.String())),
			filters.Arg("label", fmt.Sprintf("pgedge.component=%s", "postgres")),
		),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf("failed to list containers: %w", err)
	}
	if len(matches) == 0 {
		return types.Container{}, fmt.Errorf("no postgres container found for %q", instanceID.String())
	}
	return matches[0], nil
}
