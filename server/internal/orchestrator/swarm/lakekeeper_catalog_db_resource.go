package swarm

import (
	"context"
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// lakekeeperCatalogDBName derives the managed catalog database name for
// a database. Kept within Postgres's 63-byte identifier limit by
// truncating the database-name prefix, never the suffix. Truncation
// backs off to the last whole UTF-8 rune so it never leaves a partial
// multi-byte rune, which would be an invalid identifier on a UTF8
// database.
func lakekeeperCatalogDBName(databaseName string) string {
	const suffix = "_lakekeeper"
	const maxLen = 63
	if limit := maxLen - len(suffix); len(databaseName) > limit {
		databaseName = databaseName[:limit]
		for len(databaseName) > 0 && !utf8.ValidString(databaseName) {
			databaseName = databaseName[:len(databaseName)-1]
		}
	}
	return databaseName + suffix
}

// buildManagedCatalogDBURL constructs the catalog Postgres URL for a
// control-plane-managed catalog, using the overlay-network host entry
// and the service's connect-as credentials. The result contains a
// password: never log it.
func buildManagedCatalogDBURL(host database.ServiceHostEntry, username, password, dbName string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", host.Host, host.Port),
		Path:   "/" + dbName,
	}
	return u.String()
}

var _ resource.Resource = (*LakekeeperCatalogDBResource)(nil)

const ResourceTypeLakekeeperCatalogDB resource.Type = "swarm.lakekeeper_catalog_db"

func LakekeeperCatalogDBResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperCatalogDB,
	}
}

// LakekeeperCatalogDBResource creates the Postgres database that backs
// the Lakekeeper catalog when the spec sets catalog_db_create. It runs
// idempotently against the node's primary (PrimaryExecutor) after the
// main database is available, and before the Lakekeeper migrate step
// (enforced via LakekeeperMigrateResource.Dependencies).
//
// Delete is a deliberate no-op: the catalog maps every Iceberg table's
// cold data — dropping it makes the cold data unreadable. The database
// rides the node's lifecycle instead.
type LakekeeperCatalogDBResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	DatabaseID        string `json:"database_id"`
	// DatabaseName is the node's main database — the connection target
	// from which CREATE DATABASE runs.
	DatabaseName string `json:"database_name"`
	NodeName     string `json:"node_name"`
	// CatalogDBName/CatalogDBOwner are derived by the orchestrator
	// (lakekeeperCatalogDBName / the service's connect-as user).
	CatalogDBName  string `json:"catalog_db_name"`
	CatalogDBOwner string `json:"catalog_db_owner"`
	Created        bool   `json:"created"`
}

func (r *LakekeeperCatalogDBResource) ResourceVersion() string { return "1" }

func (r *LakekeeperCatalogDBResource) DiffIgnore() []string {
	return []string{"/created"}
}

func (r *LakekeeperCatalogDBResource) Identifier() resource.Identifier {
	return LakekeeperCatalogDBResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperCatalogDBResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *LakekeeperCatalogDBResource) Dependencies() []resource.Identifier {
	// The main database must exist before we can connect to it and issue
	// CREATE DATABASE for the catalog.
	return []resource.Identifier{
		database.PostgresDatabaseResourceIdentifier(r.NodeName, r.DatabaseName),
	}
}

func (r *LakekeeperCatalogDBResource) TypeDependencies() []resource.Type { return nil }

func (r *LakekeeperCatalogDBResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.Created {
		return fmt.Errorf("%w: lakekeeper catalog database has not yet been created", resource.ErrNotFound)
	}
	return nil
}

func (r *LakekeeperCatalogDBResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.ensure(ctx, rc); err != nil {
		return err
	}
	r.Created = true
	return nil
}

func (r *LakekeeperCatalogDBResource) Update(ctx context.Context, rc *resource.Context) error {
	// CREATE DATABASE is conditional and owner alignment is idempotent,
	// so re-running is safe.
	if err := r.ensure(ctx, rc); err != nil {
		return err
	}
	r.Created = true
	return nil
}

func (r *LakekeeperCatalogDBResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Deliberate no-op — see type comment.
	return nil
}

func (r *LakekeeperCatalogDBResource) ensure(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("lakekeeper catalog db: failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("lakekeeper catalog db: failed to connect to database %s on node %s: %w",
			r.DatabaseName, r.NodeName, err)
	}
	defer conn.Close(ctx)

	for _, stmt := range ensureCatalogDBStatements(r.CatalogDBName, r.CatalogDBOwner) {
		if err := stmt.Exec(ctx, conn); err != nil {
			return fmt.Errorf("lakekeeper catalog db %q: %w", r.CatalogDBName, err)
		}
	}

	// The extension pre-create must run connected to the catalog database
	// itself, so open a second connection after create+owner above.
	catConn, err := primary.Connection(ctx, rc, r.CatalogDBName)
	if err != nil {
		return fmt.Errorf("lakekeeper catalog db: failed to connect to catalog database %s: %w",
			r.CatalogDBName, err)
	}
	defer catConn.Close(ctx)
	for _, ext := range catalogDBExtensions() {
		stmt := postgres.Statement{
			SQL: fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s;", postgres.QuoteIdentifier(ext)),
		}
		if err := stmt.Exec(ctx, catConn); err != nil {
			return fmt.Errorf("lakekeeper catalog db %q: create extension %s: %w",
				r.CatalogDBName, ext, err)
		}
	}
	return nil
}

// catalogDBExtensions is the exact set of extensions Lakekeeper's v0.9.0
// migrations require (CREATE EXTENSION IF NOT EXISTS). All four are TRUSTED on
// stock PG13+, so the owner could install them itself; ensure pre-creates them
// as the system user (belt-and-braces). This is the single source of truth for
// both the ensure step and its test.
func catalogDBExtensions() []string {
	return []string{"uuid-ossp", "pgcrypto", "pg_trgm", "btree_gin"}
}

// ensureCatalogDBStatements returns the idempotent statement sequence:
// create-if-absent, then align ownership so the Lakekeeper migrate step
// (which connects as the owner) can create its schema.
func ensureCatalogDBStatements(dbName, owner string) []postgres.IStatement {
	return []postgres.IStatement{
		postgres.CreateDatabase(dbName),
		postgres.Statement{
			SQL: fmt.Sprintf("ALTER DATABASE %s OWNER TO %s;",
				postgres.QuoteIdentifier(dbName), postgres.QuoteIdentifier(owner)),
		},
	}
}
