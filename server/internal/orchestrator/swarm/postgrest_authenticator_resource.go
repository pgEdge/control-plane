package swarm

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

const ResourceTypePostgRESTAuthenticator resource.Type = "swarm.postgrest_authenticator"

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
	if r.DBAnonRole != "" {
		return r.DBAnonRole
	}
	return "pgedge_application_read_only"
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

// reconcileGrants revokes the previously-granted anon role (if changed) and
// re-applies the desired one within a single transaction to prevent transient
// loss of membership.
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
	previousAnon := r.previousAnonRole(rc)

	if err := r.revokeStaleAnonRoles(ctx, tx, username, desiredAnon, previousAnon); err != nil {
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

// previousAnonRole reads the previously-stored resource from rc.State and
// returns the anon role it had applied. Returns empty string when no prior
// state exists (first apply) so the caller can skip the revoke entirely.
func (r *PostgRESTAuthenticatorResource) previousAnonRole(rc *resource.Context) string {
	stored, ok := rc.State.Get(r.Identifier())
	if !ok {
		return ""
	}
	prev, err := resource.ToResource[*PostgRESTAuthenticatorResource](stored)
	if err != nil {
		return ""
	}
	return prev.desiredAnonRole()
}

// revokeStaleAnonRoles revokes the previously-applied anon role when it
// differs from the desired one. Only the exact role this resource previously
// granted is targeted — customer-managed grants on the connect_as user are
// never touched.
func (r *PostgRESTAuthenticatorResource) revokeStaleAnonRoles(ctx context.Context, conn postgres.Executor, username, desiredAnon, previousAnon string) error {
	if previousAnon == "" || previousAnon == desiredAnon {
		return nil
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", // #nosec G201 -- sanitizeIdentifier quotes all identifiers
		sanitizeIdentifier(previousAnon), sanitizeIdentifier(username))); err != nil {
		return fmt.Errorf("failed to revoke stale anon role %q from %q: %w", previousAnon, username, err)
	}
	return nil
}

func (r *PostgRESTAuthenticatorResource) Delete(_ context.Context, _ *resource.Context) error {
	return nil
}
