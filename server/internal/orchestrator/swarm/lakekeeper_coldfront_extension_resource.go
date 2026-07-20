package swarm

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LakekeeperColdfrontExtensionResource)(nil)

const ResourceTypeLakekeeperColdfrontExtension resource.Type = "swarm.lakekeeper_coldfront_extension"

func LakekeeperColdfrontExtensionResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperColdfrontExtension,
	}
}

// LakekeeperColdfrontExtensionResource creates the ColdFront extension in the
// node's application database as part of lakekeeper provisioning. It runs
// idempotently against the node's primary (PrimaryExecutor) after the main
// database is available, and before the storage-secret step (which calls a
// coldfront function and therefore requires the extension).
//
// A lakekeeper service is non-functional without the extension, so this is
// unconditional — there is no meaningful "lakekeeper without coldfront" mode.
// It supersedes the previous assumption (documented on
// LakekeeperStorageSecretResource) that the extension was created elsewhere;
// nothing created it.
//
// pg_duckdb is created explicitly before coldfront (see
// coldfrontExtensionStatements — the packaged control file no longer declares it
// as a CASCADE dependency). pg_duckdb must be present in shared_preload_libraries
// for CREATE EXTENSION to succeed; the lakekeeper node config adds it (#6b),
// applied at instance start. On a fresh deploy the instance boots with it
// loaded; when a lakekeeper service is added to an existing database, the
// restart that loads it is driven by the standard config-update path
// (PatroniConfig.Update -> restartIfNeeded), and CREATE EXTENSION IF NOT EXISTS
// is idempotent across reconciles.
//
// Delete is a deliberate no-op: dropping the coldfront extension would CASCADE
// to pg_duckdb and make every Iceberg table's cold data unreadable. The
// extension rides the node's lifecycle instead (matching the catalog DB).
type LakekeeperColdfrontExtensionResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	DatabaseID        string `json:"database_id"`
	// DatabaseName is the node's main (application) database — the connection
	// target in which the extension is created.
	DatabaseName string `json:"database_name"`
	NodeName     string `json:"node_name"`
	Created      bool   `json:"created"`
}

func (r *LakekeeperColdfrontExtensionResource) ResourceVersion() string { return "1" }

func (r *LakekeeperColdfrontExtensionResource) DiffIgnore() []string {
	return []string{"/created"}
}

func (r *LakekeeperColdfrontExtensionResource) Identifier() resource.Identifier {
	return LakekeeperColdfrontExtensionResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperColdfrontExtensionResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *LakekeeperColdfrontExtensionResource) Dependencies() []resource.Identifier {
	// The main database must exist before we can connect to it and create the
	// extension.
	return []resource.Identifier{
		database.PostgresDatabaseResourceIdentifier(r.NodeName, r.DatabaseName),
	}
}

func (r *LakekeeperColdfrontExtensionResource) TypeDependencies() []resource.Type { return nil }

func (r *LakekeeperColdfrontExtensionResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.Created {
		return fmt.Errorf("%w: coldfront extension has not yet been created", resource.ErrNotFound)
	}
	return nil
}

func (r *LakekeeperColdfrontExtensionResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.ensure(ctx, rc); err != nil {
		return err
	}
	r.Created = true
	return nil
}

func (r *LakekeeperColdfrontExtensionResource) Update(ctx context.Context, rc *resource.Context) error {
	// CREATE EXTENSION IF NOT EXISTS is idempotent, so re-running is safe.
	if err := r.ensure(ctx, rc); err != nil {
		return err
	}
	r.Created = true
	return nil
}

func (r *LakekeeperColdfrontExtensionResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Deliberate no-op — see type comment.
	return nil
}

func (r *LakekeeperColdfrontExtensionResource) ensure(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("coldfront extension: failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("coldfront extension: failed to connect to database %s on node %s: %w",
			r.DatabaseName, r.NodeName, err)
	}
	defer conn.Close(ctx)

	for _, stmt := range coldfrontExtensionStatements() {
		if err := stmt.Exec(ctx, conn); err != nil {
			return fmt.Errorf("coldfront extension in database %q: %w", r.DatabaseName, err)
		}
	}
	return nil
}

// coldfrontExtensionStatements returns the idempotent statement sequence that
// creates the ColdFront extension in the application database. pg_duckdb is
// created explicitly first: the native-packages build (coldfront PR #44) dropped
// `requires = 'pg_duckdb'` from coldfront.control, so CREATE EXTENSION coldfront
// CASCADE no longer pulls pg_duckdb on the packaged image (it still does on main,
// which is where the trial image came from — hence the trial masked this). The
// explicit create is robust either way. CASCADE is kept as belt-and-braces for
// any future control-file requirement. Both are IF NOT EXISTS for idempotency.
// This is the single source of truth for both the ensure step and its test.
func coldfrontExtensionStatements() []postgres.IStatement {
	return []postgres.IStatement{
		postgres.Statement{SQL: "CREATE EXTENSION IF NOT EXISTS pg_duckdb;"},
		postgres.Statement{SQL: "CREATE EXTENSION IF NOT EXISTS coldfront CASCADE;"},
	}
}
