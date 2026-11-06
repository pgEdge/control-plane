package common

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgRESTAuthenticatorResource)(nil)

const ResourceTypePostgRESTAuthenticator resource.Type = "common.postgrest_authenticator"

func PostgRESTAuthenticatorIdentifier(serviceID, nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID + "-auth-" + nodeName,
		Type: ResourceTypePostgRESTAuthenticator,
	}
}

// PostgRESTAuthenticatorResource configures a PostgreSQL role as a PostgREST
// authenticator. It targets the connect_as database user and adds
// PostgREST-specific configuration:
//
//   - ALTER ROLE ... WITH NOINHERIT (required for PostgREST's SET ROLE mechanism)
//   - GRANT CONNECT ON DATABASE to the authenticator user
//   - GRANT <anon_role> to the authenticator user
//
// On Update, the anonymous role grant is reconciled within a single transaction
// to prevent transient loss of anon-role membership when the anon role changes.
type PostgRESTAuthenticatorResource struct {
	ServiceID         string `json:"service_id"`
	DatabaseID        string `json:"database_id"`
	DatabaseName      string `json:"database_name"`
	NodeName          string `json:"node_name"`
	DBAnonRole        string `json:"db_anon_role"`
	ConnectAsUsername string `json:"connect_as_username"` // the database_users entry PostgREST connects as
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
		database.PostgresDatabaseResourceIdentifier(r.NodeName, r.DatabaseName),
	}
}

func (r *PostgRESTAuthenticatorResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *PostgRESTAuthenticatorResource) desiredAnonRole() string {
	return r.DBAnonRole
}

func (r *PostgRESTAuthenticatorResource) authenticatorUsername() string {
	return r.ConnectAsUsername
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
		postgres.AlterRoleNoInherit(username),
		postgres.GrantConnect(r.DatabaseName, username),
		postgres.GrantRole(anonRole, username),
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

// reconcileGrants revokes the previously-granted anon role from the
// previously-configured user (if either changed) and re-applies the desired
// grant within a single transaction to prevent transient loss of membership.
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

	if revoke, ok := r.staleAnonGrant(r.previous(rc)); ok {
		if err := revoke.Exec(ctx, tx); err != nil {
			return fmt.Errorf("failed to revoke stale anon role grant: %w", err)
		}
	}

	// The identifier doesn't include the username, so a connect_as change is
	// applied here rather than by Create, and the new user needs NOINHERIT too.
	grants := postgres.Statements{
		postgres.AlterRoleNoInherit(username),
		postgres.GrantConnect(r.DatabaseName, username),
		postgres.GrantRole(desiredAnon, username),
	}
	if err := grants.Exec(ctx, tx); err != nil {
		return fmt.Errorf("failed to reconcile PostgREST grants for %q: %w", username, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit grant reconciliation for %q: %w", username, err)
	}
	return nil
}

// previous reads the previously-stored resource from rc.State. Returns nil
// when no prior state exists (first apply).
func (r *PostgRESTAuthenticatorResource) previous(rc *resource.Context) *PostgRESTAuthenticatorResource {
	stored, ok := rc.State.Get(r.Identifier())
	if !ok {
		return nil
	}
	prev, err := resource.ToResource[*PostgRESTAuthenticatorResource](stored)
	if err != nil {
		return nil
	}
	return prev
}

// staleAnonGrant returns the statement that revokes the anon role grant made
// by the previous resource, if the user or the anon role has changed since.
// Only the exact grant this resource previously made is targeted:
// customer-managed grants on the connect_as user are never touched.
func (r *PostgRESTAuthenticatorResource) staleAnonGrant(prev *PostgRESTAuthenticatorResource) (postgres.ConditionalStatement, bool) {
	if prev == nil {
		return postgres.ConditionalStatement{}, false
	}
	prevUser, prevAnon := prev.authenticatorUsername(), prev.desiredAnonRole()
	if prevUser == "" || prevAnon == "" {
		return postgres.ConditionalStatement{}, false
	}
	if prevUser == r.authenticatorUsername() && prevAnon == r.desiredAnonRole() {
		return postgres.ConditionalStatement{}, false
	}
	return postgres.RevokeRole(prevAnon, prevUser), true
}

func (r *PostgRESTAuthenticatorResource) Delete(_ context.Context, _ *resource.Context) error {
	return nil
}
