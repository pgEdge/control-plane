package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*ServiceUserRole)(nil)

const ResourceTypeServiceUserRole resource.Type = "swarm.service_user_role"

const (
	ServiceUserRoleRO = "ro"
	ServiceUserRoleRW = "rw"
)

// sanitizeIdentifier quotes a string for use as a PostgreSQL identifier.
// It doubles any internal double-quotes and wraps the result in double-quotes.
func sanitizeIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func ServiceUserRoleIdentifier(serviceInstanceID string, mode string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID + "-" + mode,
		Type: ResourceTypeServiceUserRole,
	}
}

func ServiceUserRolePerNodeIdentifier(serviceID, mode, nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID + "-" + mode + "-" + nodeName,
		Type: ResourceTypeServiceUserRole,
	}
}

// ServiceUserRole manages the lifecycle of a database user for a service.
//
// Two ServiceUserRole resources are created per service: one with Mode set to
// ServiceUserRoleRO (read-only) and one with Mode set to ServiceUserRoleRW
// (read-write). Mode determines which group role the user is granted membership
// in: pgedge_application_read_only for RO, or pgedge_application for RW. All
// permissions are inherited via the group role — no custom grants are applied.
//
// On Create, a deterministic username (incorporating the mode) and a random
// password are generated, the Postgres role is created with the appropriate
// group role membership, and the credentials are stored in the resource state.
// On subsequent reconciliation cycles, credentials are reused from the persisted
// state (no password regeneration).
//
// When CredentialSource is nil, this is the canonical resource: it generates
// credentials and runs on the first node. When CredentialSource is non-nil,
// this is a per-node resource: it reads credentials from the canonical resource
// and runs on its own node's primary.
type ServiceUserRole struct {
	ServiceID        string               `json:"service_id"`
	DatabaseID       string               `json:"database_id"`
	DatabaseName     string               `json:"database_name"`
	NodeName         string               `json:"node_name"` // Database node name for PrimaryExecutor routing
	Mode             string               `json:"mode"`      // ServiceUserRoleRO or ServiceUserRoleRW
	Username         string               `json:"username"`
	Password         string               `json:"password"` // Generated on Create, persisted in state
	CredentialSource *resource.Identifier `json:"credential_source,omitempty"`
}

func (r *ServiceUserRole) ResourceVersion() string {
	return "4"
}

func (r *ServiceUserRole) DiffIgnore() []string {
	return []string{
		"/node_name",
		"/mode",
		"/username",
		"/password",
		"/credential_source",
	}
}

func (r *ServiceUserRole) Identifier() resource.Identifier {
	if r.CredentialSource != nil {
		return ServiceUserRolePerNodeIdentifier(r.ServiceID, r.Mode, r.NodeName)
	}
	return ServiceUserRoleIdentifier(r.ServiceID, r.Mode)
}

func (r *ServiceUserRole) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *ServiceUserRole) Dependencies() []resource.Identifier {
	if r.CredentialSource != nil {
		return []resource.Identifier{*r.CredentialSource}
	}
	return nil
}

func (r *ServiceUserRole) TypeDependencies() []resource.Type {
	return nil
}

func (r *ServiceUserRole) Refresh(ctx context.Context, rc *resource.Context) error {
	// If username or password is empty, the resource state is from before we
	// added credential management. Return ErrNotFound to trigger recreation.
	if r.Username == "" || r.Password == "" {
		return resource.ErrNotFound
	}
	return nil
}

func (r *ServiceUserRole) Create(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_id", r.ServiceID).
		Str("database_id", r.DatabaseID).
		Logger()
	logger.Info().Msg("creating service user role")

	if r.CredentialSource != nil {
		// Per-node resource: read credentials from the canonical resource in state.
		canonical, err := resource.FromContext[*ServiceUserRole](rc, *r.CredentialSource)
		if err != nil {
			return fmt.Errorf("canonical service user role %s must be created before per-node role: %w", r.CredentialSource, err)
		}
		r.Username = canonical.Username
		r.Password = canonical.Password
	} else {
		// Canonical resource: generate credentials.
		r.Username = database.GenerateServiceUsername(r.ServiceID, r.Mode)
		password, err := utils.RandomString(32)
		if err != nil {
			return fmt.Errorf("failed to generate password: %w", err)
		}
		r.Password = password
	}

	if err := r.createUserRole(ctx, rc, logger); err != nil {
		return fmt.Errorf("failed to create service user role: %w", err)
	}

	logger.Info().Str("username", r.Username).Msg("service user role created successfully")
	return nil
}

func (r *ServiceUserRole) createUserRole(ctx context.Context, rc *resource.Context, logger zerolog.Logger) error {
	conn, err := r.connectToPrimary(ctx, rc, logger, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	// Determine group role based on mode
	var groupRole string
	switch r.Mode {
	case ServiceUserRoleRO:
		groupRole = "pgedge_application_read_only"
	case ServiceUserRoleRW:
		groupRole = "pgedge_application"
	default:
		return fmt.Errorf("unknown service user role mode: %q", r.Mode)
	}

	statements, err := postgres.CreateUserRole(postgres.UserRoleOptions{
		Name:       r.Username,
		Password:   r.Password,
		DBOwner:    false,
		Attributes: []string{"LOGIN"},
		Roles:      []string{groupRole},
	})
	if err != nil {
		return fmt.Errorf("failed to generate create user role statements: %w", err)
	}

	if err := statements.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create service user: %w", err)
	}

	return nil
}

func (r *ServiceUserRole) Update(ctx context.Context, rc *resource.Context) error {
	// Service users don't support updates (no credential rotation in Phase 1)
	return nil
}

func (r *ServiceUserRole) Delete(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_id", r.ServiceID).
		Str("database_id", r.DatabaseID).
		Str("username", r.Username).
		Logger()
	logger.Info().Msg("deleting service user from database")

	conn, err := r.connectToPrimary(ctx, rc, logger, "postgres")
	if err != nil {
		// During deletion, connection failures are non-fatal — the database
		// may already be gone or unreachable.
		logger.Warn().Err(err).Msg("failed to connect to primary instance, skipping user deletion")
		return nil
	}
	defer conn.Close(ctx)

	// Drop the user role
	// Using IF EXISTS to handle cases where the user was already dropped manually
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", sanitizeIdentifier(r.Username)))
	if err != nil {
		logger.Warn().Err(err).Msg("failed to drop user role, continuing anyway")
		// Don't fail the deletion if we can't drop the user - this prevents
		// the resource from getting stuck in a failed state
		return nil
	}

	logger.Info().Msg("service user deleted successfully")
	return nil
}

// connectToPrimary finds the primary Postgres instance and returns an
// authenticated connection to it. The caller is responsible for closing
// the connection.
func (r *ServiceUserRole) connectToPrimary(ctx context.Context, rc *resource.Context, logger zerolog.Logger, dbName string) (*pgx.Conn, error) {
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	db, err := dbSvc.GetDatabase(ctx, r.DatabaseID)
	if err != nil {
		if errors.Is(err, database.ErrDatabaseNotFound) {
			return nil, fmt.Errorf("database not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	if len(db.Instances) == 0 {
		return nil, fmt.Errorf("database has no instances")
	}

	// Find primary instance via Patroni
	var primaryInstanceID string
	for _, inst := range db.Instances {
		if inst.NodeName != r.NodeName {
			continue
		}
		connInfo, err := dbSvc.GetInstanceConnectionInfo(ctx, r.DatabaseID, inst.InstanceID)
		if err != nil {
			continue
		}
		patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
		primaryID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 10*time.Second)
		if err == nil && primaryID != "" {
			primaryInstanceID = primaryID
			break
		}
	}
	if primaryInstanceID == "" {
		return nil, fmt.Errorf("could not determine primary instance for node %s", r.NodeName)
	}

	connInfo, err := dbSvc.GetInstanceConnectionInfo(ctx, r.DatabaseID, primaryInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	certSvc, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate service: %w", err)
	}

	tlsConfig, err := certSvc.PostgresUserTLS(ctx, primaryInstanceID, connInfo.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config: %w", err)
	}

	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: connInfo.AdminDSN(dbName),
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return conn, nil
}
