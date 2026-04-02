package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgRESTAuthenticatorResource)(nil)

const ResourceTypePostgRESTAuthenticator resource.Type = "swarm.postgrest_authenticator"

func PostgRESTAuthenticatorIdentifier(serviceID, nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID + "-auth-" + nodeName,
		Type: ResourceTypePostgRESTAuthenticator,
	}
}

// PostgRESTAuthenticatorResource configures a PostgreSQL role as a PostgREST
// authenticator. It depends on the corresponding ServiceUserRole (which creates
// the basic LOGIN+group-role user) and adds PostgREST-specific configuration:
//
//   - ALTER ROLE ... WITH NOINHERIT (required for PostgREST's SET ROLE mechanism)
//   - GRANT CONNECT ON DATABASE to the authenticator user
//   - GRANT <anon_role> to the authenticator user
//
// On Update, the anonymous role grant is reconciled within a single transaction
// to prevent transient loss of anon-role membership when the anon role changes.
// The actual DROP ROLE is handled by ServiceUserRole.Delete; this resource only
// revokes the CONNECT privilege it added.
type PostgRESTAuthenticatorResource struct {
	ServiceID    string              `json:"service_id"`
	DatabaseID   string              `json:"database_id"`
	DatabaseName string              `json:"database_name"`
	NodeName     string              `json:"node_name"`
	DBAnonRole   string              `json:"db_anon_role"`
	UserRoleID   resource.Identifier `json:"user_role_id"` // the RW ServiceUserRole this wraps
}

func (r *PostgRESTAuthenticatorResource) ResourceVersion() string { return "1" }
func (r *PostgRESTAuthenticatorResource) DiffIgnore() []string    { return nil }

func (r *PostgRESTAuthenticatorResource) Identifier() resource.Identifier {
	return PostgRESTAuthenticatorIdentifier(r.ServiceID, r.NodeName)
}

func (r *PostgRESTAuthenticatorResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *PostgRESTAuthenticatorResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		database.NodeResourceIdentifier(r.NodeName),
		r.UserRoleID,
	}
}

func (r *PostgRESTAuthenticatorResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *PostgRESTAuthenticatorResource) desiredAnonRole() string {
	if r.DBAnonRole != "" {
		return r.DBAnonRole
	}
	return "pgedge_application_read_only"
}

func (r *PostgRESTAuthenticatorResource) authenticatorUsername() string {
	return database.GenerateServiceUsername(r.ServiceID, ServiceUserRoleRW)
}

// Refresh checks whether the role has NOINHERIT. If not — new deployment or
// manual change — returns ErrNotFound to trigger Create, which is idempotent.
func (r *PostgRESTAuthenticatorResource) Refresh(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("authenticator refresh: failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("authenticator refresh: failed to connect on node %s: %w", r.NodeName, err)
	}
	defer conn.Close(ctx)

	var noInherit bool
	err = conn.QueryRow(ctx,
		"SELECT NOT rolinherit FROM pg_catalog.pg_roles WHERE rolname = $1",
		r.authenticatorUsername(),
	).Scan(&noInherit)
	if err != nil {
		return fmt.Errorf("%w", errors.Join(resource.ErrNotFound, fmt.Errorf("role %q not found: %w", r.authenticatorUsername(), err)))
	}
	if !noInherit {
		return fmt.Errorf("%w: role %q does not have NOINHERIT", resource.ErrNotFound, r.authenticatorUsername())
	}
	return nil
}

func (r *PostgRESTAuthenticatorResource) Create(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	username := r.authenticatorUsername()
	logger = logger.With().
		Str("service_id", r.ServiceID).
		Str("username", username).
		Logger()
	logger.Info().Msg("configuring PostgREST authenticator role")

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("failed to connect to database postgres on node %s: %w", r.NodeName, err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	anonRole := r.desiredAnonRole()
	statements := postgres.Statements{
		postgres.Statement{SQL: fmt.Sprintf("ALTER ROLE %s WITH NOINHERIT;", sanitizeIdentifier(username))},                                           // #nosec G201 -- sanitizeIdentifier quotes all identifiers
		postgres.Statement{SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(username))}, // #nosec G201
		postgres.Statement{SQL: fmt.Sprintf("GRANT %s TO %s;", sanitizeIdentifier(anonRole), sanitizeIdentifier(username))},                          // #nosec G201
	}
	if err := statements.Exec(ctx, tx); err != nil {
		return fmt.Errorf("failed to configure PostgREST authenticator %q: %w", username, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit authenticator configuration for %q: %w", username, err)
	}

	logger.Info().Str("anon_role", anonRole).Msg("PostgREST authenticator role configured")
	return nil
}

func (r *PostgRESTAuthenticatorResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.reconcileGrants(ctx, rc)
}

// reconcileGrants revokes stale anon role grants and re-applies the desired
// ones within a single transaction to prevent transient loss of membership.
func (r *PostgRESTAuthenticatorResource) reconcileGrants(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("failed to connect to database postgres on node %s: %w", r.NodeName, err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	username := r.authenticatorUsername()
	desiredAnon := r.desiredAnonRole()

	if err := r.revokeStaleAnonRoles(ctx, tx, username, desiredAnon); err != nil {
		return err
	}

	grants := postgres.Statements{
		postgres.Statement{SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(username))}, // #nosec G201
		postgres.Statement{SQL: fmt.Sprintf("GRANT %s TO %s;", sanitizeIdentifier(desiredAnon), sanitizeIdentifier(username))},                        // #nosec G201
	}
	if err := grants.Exec(ctx, tx); err != nil {
		return fmt.Errorf("failed to reconcile PostgREST grants for %q: %w", username, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit grant reconciliation for %q: %w", username, err)
	}
	return nil
}

// revokeStaleAnonRoles revokes any previously-granted anon roles that are no
// longer the desired one. The query is scoped to known anon role candidates so
// that base group roles granted by ServiceUserRole (pgedge_application,
// pgedge_application_read_only) are never touched. Must be called within a
// transaction for atomicity.
func (r *PostgRESTAuthenticatorResource) revokeStaleAnonRoles(ctx context.Context, conn postgres.Executor, username, desiredAnon string) error {
	// Only query memberships that this resource could have granted — the set of
	// known anon role names. This prevents accidentally revoking base group
	// roles that ServiceUserRole manages.
	currentRoles, err := postgres.Query[string]{
		SQL: `SELECT r.rolname
		      FROM pg_auth_members m
		      JOIN pg_roles r ON m.roleid = r.oid
		      JOIN pg_roles u ON m.member = u.oid
		      WHERE u.rolname = @username
		        AND r.rolname != 'pgedge_application'
		        AND r.rolname != 'pgedge_application_read_only'`,
		Args: pgx.NamedArgs{"username": username},
	}.Scalars(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query anon role memberships for %q: %w", username, err)
	}
	for _, current := range currentRoles {
		if current != desiredAnon {
			if _, err := conn.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", // #nosec G201 -- sanitizeIdentifier quotes all identifiers
				sanitizeIdentifier(current), sanitizeIdentifier(username))); err != nil {
				return fmt.Errorf("failed to revoke stale anon role %q from %q: %w", current, username, err)
			}
		}
	}
	return nil
}

func (r *PostgRESTAuthenticatorResource) Delete(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	username := r.authenticatorUsername()
	logger = logger.With().
		Str("service_id", r.ServiceID).
		Str("username", username).
		Logger()

	if r.DatabaseName == "" {
		return nil
	}

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get primary instance, skipping REVOKE CONNECT")
		return nil
	}
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to primary instance, skipping REVOKE CONNECT")
		return nil
	}
	defer conn.Close(ctx)

	if _, rErr := conn.Exec(ctx, fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM %s", // #nosec G201 -- sanitizeIdentifier quotes all identifiers
		sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(username))); rErr != nil {
		logger.Warn().Err(rErr).Msg("failed to revoke CONNECT privilege, continuing")
	}
	return nil
}
