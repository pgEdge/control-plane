package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
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
	nodeID := database.NodeResourceIdentifier(r.NodeName)
	if r.CredentialSource != nil {
		return []resource.Identifier{nodeID, *r.CredentialSource}
	}
	return []resource.Identifier{nodeID}
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

	// Verify the role actually exists in pg_roles. This guards against the role
	// being dropped externally, or a failed Create that left the state persisted
	// but the role absent from Postgres.
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("failed to connect to postgres on node %s: %w", r.NodeName, err)
	}
	defer conn.Close(ctx)

	needsCreate, err := postgres.UserRoleNeedsCreate(r.Username).Scalar(ctx, conn)
	if err != nil {
		logger, logErr := do.Invoke[zerolog.Logger](rc.Injector)
		if logErr == nil {
			logger.Warn().Err(err).Str("username", r.Username).Msg("pg_roles query failed during service user role refresh")
		}
		return fmt.Errorf("pg_roles query failed: %w", err)
	}
	if needsCreate {
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
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("failed to connect to database postgres on node %s: %w", r.NodeName, err)
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

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		// During deletion, connection failures are non-fatal — the database
		// may already be gone or unreachable.
		logger.Warn().Err(err).Msg("failed to get primary instance, skipping user deletion")
		return nil
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
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
