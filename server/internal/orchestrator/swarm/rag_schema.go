package swarm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*RAGSchemaResource)(nil)

const ResourceTypeRAGSchema resource.Type = "swarm.rag_schema"

func RAGSchemaResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGSchema,
	}
}

// RAGSchemaResource ensures the pgvector extension is enabled and the service
// user has SELECT access on the configured tables. Table creation is the
// user's responsibility (e.g. via pgedge_vectorizer or direct DDL).
type RAGSchemaResource struct {
	ServiceInstanceID string                `json:"service_instance_id"`
	DatabaseID        string                `json:"database_id"`
	DatabaseName      string                `json:"database_name"`
	HostID            string                `json:"host_id"`
	PostgresHostID    string                `json:"postgres_host_id"`
	ServiceSpec       *database.ServiceSpec `json:"service_spec"`
	// SchemaSetup tracks whether initial schema creation has been performed.
	// Excluded from diff so it does not trigger spurious updates.
	SchemaSetup bool `json:"schema_setup"`
}

func (r *RAGSchemaResource) ResourceVersion() string { return "1" }

func (r *RAGSchemaResource) DiffIgnore() []string {
	return []string{"/schema_setup"}
}

func (r *RAGSchemaResource) Identifier() resource.Identifier {
	return RAGSchemaResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGSchemaResource) Executor() resource.Executor {
	if r.PostgresHostID != "" {
		return resource.HostExecutor(r.PostgresHostID)
	}
	return resource.HostExecutor(r.HostID)
}

func (r *RAGSchemaResource) Dependencies() []resource.Identifier {
	// Depends on ServiceUserRole so we can grant SELECT on the user's tables.
	return []resource.Identifier{
		ServiceUserRoleIdentifier(r.ServiceInstanceID),
	}
}

// Refresh returns ErrNotFound until Create has run and set SchemaSetup=true.
func (r *RAGSchemaResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.SchemaSetup {
		return resource.ErrNotFound
	}
	return nil
}

func (r *RAGSchemaResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.setup(ctx, rc)
}

func (r *RAGSchemaResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.setup(ctx, rc)
}

func (r *RAGSchemaResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Schema teardown is not performed — tables belong to the user's database.
	return nil
}

func (r *RAGSchemaResource) setup(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_instance_id", r.ServiceInstanceID).
		Str("database_id", r.DatabaseID).
		Logger()

	// Resolve the service user so we can grant SELECT on the user's tables.
	userRole, err := resource.FromContext[*ServiceUserRole](rc, ServiceUserRoleIdentifier(r.ServiceInstanceID))
	if err != nil {
		return fmt.Errorf("failed to get service user role: %w", err)
	}

	conn, err := r.connectToDatabase(ctx, rc, logger)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	logger.Info().Msg("setting up RAG schema")

	// Enable pgvector extension (requires superuser, admin connection used).
	// Two RAG instances may run setup() concurrently on the same database;
	// execDDLIdempotent treats concurrent-creation races as a no-op.
	if err := execDDLIdempotent(ctx, conn, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Collect unique table names across all pipelines and grant SELECT to the
	// service user. Table creation is the user's responsibility.
	tables := collectRAGTables(r.ServiceSpec.Config)
	for _, tableName := range tables {
		grantSQL := fmt.Sprintf(`GRANT SELECT ON %s TO %s`,
			sanitizeIdentifier(tableName),
			sanitizeIdentifier(userRole.Username),
		)
		if err := execDDLIdempotent(ctx, conn, grantSQL); err != nil {
			return fmt.Errorf("failed to grant SELECT on table %q to %q: %w", tableName, userRole.Username, err)
		}
		logger.Info().Str("table", tableName).Msg("granted SELECT on RAG table")
	}

	r.SchemaSetup = true
	return nil
}

// collectRAGTables returns a deduplicated list of table names referenced across
// all pipelines in the config.
func collectRAGTables(cfg map[string]any) []string {
	seen := map[string]bool{}
	var tables []string
	rawPipelines, _ := cfg["pipelines"].([]any)
	for _, rawPipeline := range rawPipelines {
		p, ok := rawPipeline.(map[string]any)
		if !ok {
			continue
		}
		rawTables, _ := p["tables"].([]any)
		for _, t := range rawTables {
			tableMap, ok := t.(map[string]any)
			if !ok {
				continue
			}
			tableName, _ := tableMap["table"].(string)
			if tableName != "" && !seen[tableName] {
				seen[tableName] = true
				tables = append(tables, tableName)
			}
		}
	}
	return tables
}

// execDDLIdempotent executes a DDL statement and ignores concurrent-creation
// races (SQLSTATE 23505 unique_violation, 42710 duplicate_object). These occur
// when two RAG instances run schema setup simultaneously on the same database.
func execDDLIdempotent(ctx context.Context, conn *pgx.Conn, sql string) error {
	_, err := conn.Exec(ctx, sql)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "42710" || pgErr.Code == "XX000") {
		return nil
	}
	return err
}

// connectToDatabase connects to the service database (not "postgres") using the
// admin user so that CREATE EXTENSION and CREATE TABLE succeed.
func (r *RAGSchemaResource) connectToDatabase(ctx context.Context, rc *resource.Context, logger zerolog.Logger) (*pgx.Conn, error) {
	orch, err := do.Invoke[database.Orchestrator](rc.Injector)
	if err != nil {
		return nil, err
	}

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

	// Prefer the co-located Postgres instance so schema setup runs on the
	// correct Spock node. CREATE ROLE is not replicated by Spock, so each
	// node must have the schema created directly on its own primary.
	targetHostID := r.PostgresHostID
	if targetHostID == "" {
		targetHostID = r.HostID
	}

	var seedInstance *database.Instance
	for i := range db.Instances {
		if db.Instances[i].HostID == targetHostID {
			seedInstance = db.Instances[i]
			break
		}
	}
	if seedInstance == nil {
		seedInstance = db.Instances[0]
		logger.Warn().Str("target_host_id", targetHostID).Msg("no co-located postgres instance found, falling back to first available")
	}

	var primaryInstanceID string
	{
		connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.DatabaseID, seedInstance.InstanceID)
		if err == nil {
			patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
			primaryID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 10*time.Second)
			if err == nil && primaryID != "" {
				primaryInstanceID = primaryID
			}
		}
	}
	if primaryInstanceID == "" {
		primaryInstanceID = seedInstance.InstanceID
		logger.Warn().Msg("could not determine primary instance, using co-located instance directly")
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.DatabaseID, primaryInstanceID)
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
		DSN: connInfo.AdminDSN(r.DatabaseName),
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %q: %w", r.DatabaseName, err)
	}

	return conn, nil
}
