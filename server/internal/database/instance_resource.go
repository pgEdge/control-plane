package database

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, orch, r.Spec.DatabaseID, r.Spec.InstanceID, 30*time.Second)
	if err != nil {
		return resource.ErrNotFound // TODO: Is this always the right choice?
	}
	r.PrimaryInstanceID = primaryInstanceID

	return nil
}

func (r *InstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return err
	}

	err = WaitForPatroniRunning(ctx, orch, r.Spec.DatabaseID, r.Spec.InstanceID, 12*time.Hour)
	if err != nil {
		return fmt.Errorf("failed to wait for patroni to enter running state: %w", err)
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, orch, r.Spec.DatabaseID, r.Spec.InstanceID, time.Minute)
	if err != nil {
		return err
	}
	r.PrimaryInstanceID = primaryInstanceID

	if r.Spec.InstanceID != r.PrimaryInstanceID {
		// this is a no-op on non-primary instances
		return nil
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.Spec.DatabaseID, r.Spec.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance DSN: %w", err)
	}
	r.ConnectionInfo = connInfo

	firstTimeSetup, err := r.isFirstTimeSetup(rc)
	if err != nil {
		return err
	}

	if r.Spec.RestoreConfig != nil && firstTimeSetup {
		err = r.renameDB(ctx, connInfo, tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to rename database %q: %w", r.Spec.DatabaseName, err)
		}
		err = r.dropSpock(ctx, connInfo, tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to drop spock: %w", err)
		}
	}

	err = r.createDB(ctx, connInfo, tlsCfg)
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

func (r *InstanceResource) createDB(ctx context.Context, connInfo *ConnectionInfo, tlsCfg *tls.Config) error {
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

	return nil
}

func (r *InstanceResource) renameDB(ctx context.Context, connInfo *ConnectionInfo, tlsCfg *tls.Config) error {
	// Short circuit if the restore config doesn't include a dbname or if the
	// database name is the same.
	if r.Spec.RestoreConfig.SourceDatabaseName == "" || r.Spec.RestoreConfig.SourceDatabaseName == r.Spec.DatabaseName {
		return nil
	}

	// This operation can be flaky because of other processes connected to the
	// database. We retry it a few times to avoid failing the entire create
	// operation.
	err := utils.Retry(3, 500*time.Millisecond, func() error {
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

		return postgres.
			RenameDB(r.Spec.RestoreConfig.SourceDatabaseName, r.Spec.DatabaseName).
			Exec(ctx, createDBConn)
	})
	if err != nil {
		return fmt.Errorf("failed to rename database %q: %w", r.Spec.DatabaseName, err)
	}

	return nil
}

func (r *InstanceResource) dropSpock(ctx context.Context, connInfo *ConnectionInfo, tlsCfg *tls.Config) error {
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

	err = postgres.DropSpockAndCleanupSlots(r.Spec.DatabaseName).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to drop spock: %w", err)
	}

	return nil
}

func (r *InstanceResource) isFirstTimeSetup(rc *resource.Context) (bool, error) {
	// This instance will already exist in the state if it's been successfully
	// created before.
	_, err := resource.FromContext[*InstanceResource](rc, r.Identifier())
	if errors.Is(err, resource.ErrNotFound) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check state for previous version of this instance: %w", err)
	}

	return false, nil
}
