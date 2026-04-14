package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*InstanceResource)(nil)

const ResourceTypeInstance resource.Type = "database.instance"

func InstanceResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeInstance,
	}
}

type InstanceResource struct {
	Spec                     *InstanceSpec         `json:"spec"`
	InstanceHostname         string                `json:"instance_hostname"`
	PrimaryInstanceID        string                `json:"primary_instance_id"`
	OrchestratorDependencies []resource.Identifier `json:"dependencies"`
	ConnectionInfo           *ConnectionInfo       `json:"connection_info"`
	PostInit                 *Script               `json:"post_init"`
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
	return resource.HostExecutor(r.Spec.HostID)
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

func (r *InstanceResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *InstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return resource.ErrNotFound
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, r.patroniClient(), 30*time.Second)
	if err != nil {
		return resource.ErrNotFound
	}
	r.PrimaryInstanceID = primaryInstanceID

	if err := SetScriptNeedsToRun(ctx, rc, r.PostInit); err != nil {
		return err
	}

	return nil
}

func (r *InstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.initializeInstance(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	if err := r.patroniClient().Reload(ctx); err != nil {
		err = fmt.Errorf("failed to reload patroni conf: %w", err)
		return r.recordError(ctx, rc, err)
	}

	if err := r.initializeInstance(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	// It's unnecessary and potentially error-prone to delete anything here.
	// Instead, we just shut down the instance and delete the data from the
	// filesystem when we're fulfilling a delete request.
	return nil
}

func (r *InstanceResource) Connection(ctx context.Context, rc *resource.Context, dbName string) (*pgx.Conn, error) {
	if rc.HostID != r.Spec.HostID {
		return nil, fmt.Errorf("cannot connect to an instance running on a different host. executing host = '%s', instance host = '%s'", rc.HostID, r.Spec.HostID)
	}

	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN(dbName),
		TLS: tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database '%s': %w", dbName, err)
	}
	return conn, nil
}

func (r *InstanceResource) initializeInstance(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return err
	}

	patroniClient := r.patroniClient()
	err := WaitForPatroniRunning(ctx, patroniClient, 0)
	if err != nil {
		return fmt.Errorf("failed to wait for patroni to enter running state: %w", err)
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, patroniClient, time.Minute)
	if err != nil {
		return err
	}
	r.PrimaryInstanceID = primaryInstanceID

	if r.Spec.InstanceID != r.PrimaryInstanceID {
		err = r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
		if err != nil {
			return r.recordError(ctx, rc, err)
		}
		// no other initialization needed on non-primary instances
		return nil
	}

	conn, err := r.Connection(ctx, rc, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if err := ExecuteScript(ctx, rc, conn, r.PostInit); err != nil {
		return fmt.Errorf("failed to execute post-init script: %w", err)
	}

	// Spock shouldn't exist in the 'postgres' database, but we want to err on
	// the side of caution.
	tx, err := postgres.StartRepairModeTxn(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to start repair mode transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	roleStatements, err := postgres.CreateBuiltInRoles(postgres.BuiltinRoleOptions{
		PGVersion: r.Spec.PgEdgeVersion.PostgresVersion.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to generate built-in role statements: %w", err)
	}
	if err := roleStatements.Exec(ctx, tx); err != nil {
		return fmt.Errorf("failed to create built-in roles: %w", err)
	}

	for _, user := range r.Spec.DatabaseUsers {
		statement, err := postgres.CreateUserRole(postgres.UserRoleOptions{
			Name:       user.Username,
			Password:   user.Password,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		})
		if err != nil {
			return fmt.Errorf("failed to produce create user role statement %q: %w", user.Username, err)
		}
		if err := statement.Exec(ctx, tx); err != nil {
			return fmt.Errorf("failed to create user role %q: %w", user.Username, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	err = r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) updateInstanceRecord(ctx context.Context, rc *resource.Context, opts *InstanceUpdateOptions) error {
	svc, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}
	opts.InstanceID = r.Spec.InstanceID
	opts.DatabaseID = r.Spec.DatabaseID
	opts.HostID = r.Spec.HostID
	opts.NodeName = r.Spec.NodeName
	opts.Port = r.Spec.Port
	opts.PatroniPort = r.Spec.PatroniPort
	opts.PgEdgeVersion = r.Spec.PgEdgeVersion
	err = svc.UpdateInstance(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to update instance state: %w", err)
	}

	return nil
}

func (r *InstanceResource) recordError(ctx context.Context, rc *resource.Context, cause error) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	err = r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{
		State: InstanceStateFailed,
		Error: cause.Error(),
	})
	if err != nil {
		logger.Err(err).Msg("failed to persist instance error status")
	}

	return cause
}

func (r *InstanceResource) updateConnectionInfo(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	connInfo, err := orch.GetInstanceConnectionInfo(ctx,
		r.Spec.DatabaseID, r.Spec.InstanceID,
		r.Spec.Port, r.Spec.PatroniPort,
		r.Spec.PgEdgeVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to get instance connection info: %w", err)
	}
	r.ConnectionInfo = connInfo

	return nil
}

func (r *InstanceResource) patroniClient() *patroni.Client {
	return patroni.NewClient(r.ConnectionInfo.PatroniURL(), nil)
}
