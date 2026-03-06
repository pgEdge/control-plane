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

// RAGSchemaResource ensures the pgvector extension and configured tables exist
// in the database before the RAG service container is deployed. It is idempotent
// and uses IF NOT EXISTS so it is safe to run multiple times.
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
	// Depends on ServiceUserRole so we can grant table access to the service user.
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

	// Resolve the service user so we can grant SELECT on the new tables.
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

	cfg := r.ServiceSpec.Config
	rawTables, _ := cfg["tables"].([]any)
	if len(rawTables) == 0 {
		return fmt.Errorf("no tables configured for RAG schema setup")
	}

	embeddingModel := stringConfigField(cfg, "embedding_model", "")
	dims := embeddingDimensions(embeddingModel)

	for _, t := range rawTables {
		tableMap, ok := t.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid table entry in RAG config")
		}
		tableName, _ := tableMap["table"].(string)
		textCol, _ := tableMap["text_column"].(string)
		vectorCol, _ := tableMap["vector_column"].(string)
		idCol, _ := tableMap["id_column"].(string)
		if idCol == "" {
			idCol = "id"
		}
		if tableName == "" || textCol == "" || vectorCol == "" {
			return fmt.Errorf("table entry missing required fields (table, text_column, vector_column)")
		}

		createSQL := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (%s BIGSERIAL PRIMARY KEY, %s TEXT NOT NULL, %s vector(%d))`,
			sanitizeIdentifier(tableName),
			sanitizeIdentifier(idCol),
			sanitizeIdentifier(textCol),
			sanitizeIdentifier(vectorCol),
			dims,
		)
		if err := execDDLIdempotent(ctx, conn, createSQL); err != nil {
			return fmt.Errorf("failed to create table %q: %w", tableName, err)
		}

		indexName := fmt.Sprintf("%s_%s_hnsw_idx", tableName, vectorCol)
		indexSQL := fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (%s vector_cosine_ops)`,
			sanitizeIdentifier(indexName),
			sanitizeIdentifier(tableName),
			sanitizeIdentifier(vectorCol),
		)
		if err := execDDLIdempotent(ctx, conn, indexSQL); err != nil {
			return fmt.Errorf("failed to create vector index on table %q: %w", tableName, err)
		}

		grantSQL := fmt.Sprintf(`GRANT SELECT ON %s TO %s`,
			sanitizeIdentifier(tableName),
			sanitizeIdentifier(userRole.Username),
		)
		if err := execDDLIdempotent(ctx, conn, grantSQL); err != nil {
			return fmt.Errorf("failed to grant SELECT on table %q to %q: %w", tableName, userRole.Username, err)
		}

		logger.Info().
			Str("table", tableName).
			Int("vector_dims", dims).
			Msg("RAG table ready")
	}

	r.SchemaSetup = true
	return nil
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

// embeddingDimensions returns the vector dimensions for a given embedding model name.
func embeddingDimensions(model string) int {
	switch model {
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	case "voyage-3-large":
		return 1024
	case "voyage-3":
		return 1024
	case "voyage-3-lite":
		return 512
	default:
		// text-embedding-3-small and most other models default to 1536.
		return 1536
	}
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
