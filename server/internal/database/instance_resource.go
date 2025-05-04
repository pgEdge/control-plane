package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*InstanceResource)(nil)

const ResourceTypeInstance resource.Type = "database.instance"

func InstanceResourceIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypeInstance,
	}
}

type InstanceResource struct {
	Spec                     *InstanceSpec         `json:"spec"`
	InstanceHostname         string                `json:"instance_hostname"`
	PrimaryInstanceID        uuid.UUID             `json:"primary_instance_id"`
	OrchestratorDependencies []resource.Identifier `json:"dependencies"`
	ConnectionInfo           *ConnectionInfo       `json:"connection_info"`
}

func (r *InstanceResource) ResourceVersion() string {
	return "1"
}

func (r *InstanceResource) DiffIgnore() []string {
	return []string{
		"/primary_instance_id",
		"/connection_info",
	}
}

func (r *InstanceResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   r.Spec.HostID.String(),
	}
}

func (r *InstanceResource) Identifier() resource.Identifier {
	return InstanceResourceIdentifier(r.Spec.InstanceID)
}

func (r *InstanceResource) Validate() error {
	var errs []error
	if r.Spec == nil {
		errs = append(errs, errors.New("spec: instance spec is required"))
	}
	return errors.Join(errs...)
}

func (r *InstanceResource) Dependencies() []resource.Identifier {
	dependencies := slices.Clone(r.OrchestratorDependencies)

	return dependencies
}

func (r *InstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	primaryInstanceID, err := r.getPrimaryInstanceID(ctx, rc.Injector)
	if err != nil {
		return resource.ErrNotFound // TODO: Is this always the right choice?
	}

	r.PrimaryInstanceID = primaryInstanceID

	return nil
}

func (r *InstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	primaryInstanceID, err := r.getPrimaryInstanceID(ctx, rc.Injector)
	if err != nil {
		return err
	}
	r.PrimaryInstanceID = primaryInstanceID

	if r.Spec.InstanceID != r.PrimaryInstanceID {
		// this is a no-op on non-primary instances
		return nil
	}

	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}

	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return err
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r)
	if err != nil {
		return fmt.Errorf("failed to get instance DSN: %w", err)
	}
	r.ConnectionInfo = connInfo

	createDBConn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: &postgres.DSN{
			Host:   connInfo.AdminHost,
			Port:   connInfo.AdminPort,
			DBName: "postgres",
			User:   "pgedge",
		},
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to 'postgres' database on instance: %w", err)
	}
	defer createDBConn.Close(ctx)

	err = postgres.CreateDatabase(r.Spec.DatabaseName).Exec(ctx, createDBConn)
	if err != nil {
		return fmt.Errorf("failed to create database %q: %w", r.Spec.DatabaseName, err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: &postgres.DSN{
			Host:   connInfo.AdminHost,
			Port:   connInfo.AdminPort,
			DBName: r.Spec.DatabaseName,
			User:   "pgedge",
		},
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", r.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	enabled, err := postgres.IsSpockEnabled().Row(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to check if spock is enabled: %w", err)
	}

	if enabled {
		err = postgres.EnableRepairMode().Exec(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to enable repair mode: %w", err)
		}
	}

	err = postgres.InitializePgEdgeExtensions(r.Spec.NodeName, &postgres.DSN{
		Host:        connInfo.PeerHost,
		Port:        connInfo.PeerPort,
		DBName:      r.Spec.DatabaseName,
		User:        "pgedge",
		SSLCert:     connInfo.PeerSSLCert,
		SSLKey:      connInfo.PeerSSLKey,
		SSLRootCert: connInfo.PeerSSLRootCert,
	}).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to initialize pgedge extensions: %w", err)
	}
	roleStatements, err := postgres.CreateBuiltInRoles(postgres.BuiltinRoleOptions{
		PGVersion: int(r.Spec.PgEdgeVersion.PostgresVersion.Major()),
		DBName:    r.Spec.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to generate built-in role statements: %w", err)
	}
	if err := roleStatements.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create built-in roles: %w", err)
	}

	for _, user := range r.Spec.DatabaseUsers {
		statement, err := postgres.CreateUserRole(postgres.UserRoleOptions{
			Name:       user.Username,
			Password:   user.Password,
			DBName:     r.Spec.DatabaseName,
			DBOwner:    user.DBOwner,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		})
		if err != nil {
			return fmt.Errorf("failed to produce create user role statement %q: %w", user.Username, err)
		}
		if err := statement.Exec(ctx, conn); err != nil {
			return fmt.Errorf("failed to create user role %q: %w", user.Username, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (r *InstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *InstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *InstanceResource) getPrimaryInstanceID(ctx context.Context, i *do.Injector) (uuid.UUID, error) {
	orch, err := do.Invoke[Orchestrator](i)
	if err != nil {
		return uuid.Nil, err
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get instance DSN: %w", err)
	}
	patroniURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:8888", connInfo.AdminHost),
	}
	patroniClient := patroni.NewClient(patroniURL, nil)

	status, err := patroniClient.GetClusterStatus(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	var primaryInstanceID uuid.UUID
	for _, m := range status.Members {
		if !m.IsLeader() {
			continue
		}
		if m.Name == nil {
			continue
		}
		id, err := uuid.Parse(*m.Name)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to parse instance ID from member name %q: %w", *m.Name, err)
		}
		primaryInstanceID = id
		break
	}

	return primaryInstanceID, nil
}

func (r *InstanceResource) Connection(ctx context.Context, rc *resource.Context, dbName string) (*pgx.Conn, error) {
	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: &postgres.DSN{
			Host:   r.ConnectionInfo.AdminHost,
			Port:   r.ConnectionInfo.AdminPort,
			DBName: dbName,
			User:   "pgedge",
		},
		TLS: tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %q: %w", r.Spec.DatabaseName, err)
	}
	return conn, nil
}
