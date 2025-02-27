package swarm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// const SwarmCreateDBService = "SwarmCreateDBService"

type InitializeDBInput struct {
	Instance     *database.InstanceSpec   `json:"spec"`
	AllPrimaries []*database.InstanceSpec `json:"all_primaries"`
}

func (i *InitializeDBInput) Validate() error {
	var errs []error
	// if i.DatabaseID == "" {
	// 	errs = append(errs, errors.New("database_id: cannot be empty"))
	// }
	// if i.SizeSpec == "" {
	// 	errs = append(errs, errors.New("size_spec: cannot be empty"))
	// }
	return errors.Join(errs...)
}

type InitializeDBOutput struct {
	// Service docker.ServiceSpec
}

func (a *Activities) ExecuteInitializeDB(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *InitializeDBInput,
) workflow.Future[*InitializeDBOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*InitializeDBOutput](ctx, options, a.InitializeDB, input)
}

func (a *Activities) InitializeDB(ctx context.Context, input *InitializeDBInput) (*InitializeDBOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	container, err := swarm.GetPostgresContainer(ctx, a.Docker, input.Instance.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get postgres container: %w", err)
	}

	// The bridge network will be the only one with a gateway.
	var ipAddress string
	for _, network := range container.NetworkSettings.Networks {
		if network.Gateway != "" {
			ipAddress = network.IPAddress
			break
		}
	}
	if ipAddress == "" {
		return nil, fmt.Errorf("no bridge network IP address found for postgres container %q", container.ID)
	}

	tlsConfig, err := a.CertService.PostgresUserTLS(ctx, input.Instance.InstanceID, "postgres-"+input.Instance.NodeName, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	createDBConn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: &postgres.DSN{
			Host:   ipAddress,
			Port:   5432, // This is the container's port. Should always be 5432.
			DBName: "postgres",
			User:   "pgedge",
		},
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to postgres database: %w", err)
	}
	defer createDBConn.Close(ctx)

	_, err = postgres.CreateDatabase(input.Instance.DatabaseName).Exec(ctx, createDBConn)
	if err != nil {
		return nil, fmt.Errorf("failed to create database %q: %w", input.Instance.DatabaseName, err)
	}

	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: &postgres.DSN{
			Host:   ipAddress,
			Port:   5432,
			DBName: input.Instance.DatabaseName,
			User:   "pgedge",
		},
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to db %q: %w", input.Instance.DatabaseName, err)
	}
	err = postgres.InitializePgEdgeExtensions(input.Instance.NodeName, &postgres.DSN{
		Host:        input.Instance.HostnameWithDomain(),
		Port:        5432,
		DBName:      input.Instance.DatabaseName,
		User:        "pgedge",
		SSLCert:     "/opt/pgedge/certificates/postgres/superuser.crt",
		SSLKey:      "/opt/pgedge/certificates/postgres/superuser.key",
		SSLRootCert: "/opt/pgedge/certificates/postgres/ca.crt",
	}).Exec(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize spock: %w", err)
	}
	roleStatements, err := postgres.CreateBuiltInRoles(postgres.BuiltinRoleOptions{
		PGVersion: int(input.Instance.PgEdgeVersion.PostgresVersion.Major()),
		DBName:    input.Instance.DatabaseName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create built-in roles: %w", err)
	}
	if err := roleStatements.Exec(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to create built-in roles: %w", err)
	}

	for _, user := range input.Instance.DatabaseUsers {
		statement, err := postgres.CreateUserRole(postgres.UserRoleOptions{
			Name:       user.Username,
			Password:   user.Password,
			DBName:     input.Instance.DatabaseName,
			DBOwner:    user.DBOwner,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to produce create user role statement %q: %w", user.Username, err)
		}
		if err := statement.Exec(ctx, conn); err != nil {
			return nil, fmt.Errorf("failed to create user role %q: %w", user.Username, err)
		}
	}

	var peers []*database.InstanceSpec
	for _, instance := range input.AllPrimaries {
		if instance.NodeName != input.Instance.NodeName {
			peers = append(peers, instance)
		}
	}

	for _, peer := range peers {
		err = utils.Retry(5, time.Second, func() error {
			_, err := postgres.CreateSubscription(input.Instance.NodeName, peer.NodeName, &postgres.DSN{
				Host:        peer.HostnameWithDomain(),
				Port:        5432,
				DBName:      input.Instance.DatabaseName,
				User:        "pgedge",
				SSLCert:     "/opt/pgedge/certificates/postgres/superuser.crt",
				SSLKey:      "/opt/pgedge/certificates/postgres/superuser.key",
				SSLRootCert: "/opt/pgedge/certificates/postgres/ca.crt",
			}).Exec(ctx, conn)
			if err != nil {
				return fmt.Errorf("failed to create subscription to peer %q: %w", peer.Hostname(), err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// host, err := a.HostService.GetHost(ctx, a.Config.HostID)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to get host: %w", err)
	// }

	// paths := HostPathsFor(a.Config, input.Instance)
	// spec, err := swarm.DatabaseServiceSpec(host, a.Config, input.Instance, &swarm.HostOptions{
	// 	DatabaseNetworkID: input.DatabaseNetwork.ID,
	// 	Paths: swarm.Paths{
	// 		Configs:      paths.Configs.Dir,
	// 		Certificates: paths.Certificates.Dir,
	// 		Data:         paths.Data.Dir,
	// 	},
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to generate service spec: %w", err)
	// }

	return &InitializeDBOutput{
		// Service: spec,
	}, nil
}
