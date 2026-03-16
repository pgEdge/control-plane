package swarm

import (
	"context"
	"errors"
	"fmt"
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

var _ resource.Resource = (*RAGServiceUserRole)(nil)

const ResourceTypeRAGServiceUserRole resource.Type = "swarm.rag_service_user_role"

func RAGServiceUserRoleIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGServiceUserRole,
	}
}

// The role is created on the primary of the co-located Postgres instance
// (same HostID) and granted the pgedge_application_read_only built-in role.
type RAGServiceUserRole struct {
	ServiceInstanceID string `json:"service_instance_id"`
	ServiceID         string `json:"service_id"`
	DatabaseID        string `json:"database_id"`
	DatabaseName      string `json:"database_name"`
	HostID            string `json:"host_id"`   // Used to find the co-located Postgres instance
	NodeName          string `json:"node_name"` // Database node name for PrimaryExecutor routing
	Username          string `json:"username"`
	Password          string `json:"password"` // Generated on Create, persisted in state
}

func (r *RAGServiceUserRole) ResourceVersion() string {
	return "1"
}

func (r *RAGServiceUserRole) DiffIgnore() []string {
	return []string{
		"/node_name",
		"/username",
		"/password",
	}
}

func (r *RAGServiceUserRole) Identifier() resource.Identifier {
	return RAGServiceUserRoleIdentifier(r.ServiceInstanceID)
}

func (r *RAGServiceUserRole) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *RAGServiceUserRole) Dependencies() []resource.Identifier {
	return nil
}

func (r *RAGServiceUserRole) TypeDependencies() []resource.Type {
	return nil
}

func (r *RAGServiceUserRole) Refresh(ctx context.Context, rc *resource.Context) error {
	if r.Username == "" || r.Password == "" {
		return resource.ErrNotFound
	}

	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_instance_id", r.ServiceInstanceID).
		Str("database_id", r.DatabaseID).
		Logger()

	conn, err := r.connectToColocatedPrimary(ctx, rc, logger, r.DatabaseName)
	if err != nil {
		logger.Warn().Err(err).Msg("could not connect to verify RAG role existence, assuming it exists")
		return nil
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)",
		r.Username,
	).Scan(&exists)
	if err != nil {
		// On query failure, assume it exists
		logger.Warn().Err(err).Msg("pg_roles query failed, assuming RAG role exists")
		return nil
	}
	if !exists {
		return resource.ErrNotFound
	}
	return nil
}

func (r *RAGServiceUserRole) Create(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_instance_id", r.ServiceInstanceID).
		Str("database_id", r.DatabaseID).
		Logger()
	logger.Info().Msg("creating RAG service user role")

	r.Username = database.GenerateServiceUsername(r.ServiceInstanceID)
	password, err := utils.RandomString(32)
	if err != nil {
		return fmt.Errorf("failed to generate password: %w", err)
	}
	r.Password = password

	if err := r.createRole(ctx, rc, logger); err != nil {
		return fmt.Errorf("failed to create RAG service user role: %w", err)
	}

	logger.Info().Str("username", r.Username).Msg("RAG service user role created successfully")
	return nil
}

func (r *RAGServiceUserRole) createRole(ctx context.Context, rc *resource.Context, logger zerolog.Logger) error {
	conn, err := r.connectToColocatedPrimary(ctx, rc, logger, r.DatabaseName)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	statements, err := postgres.CreateUserRole(postgres.UserRoleOptions{
		Name:       r.Username,
		Password:   r.Password,
		DBName:     r.DatabaseName,
		DBOwner:    false,
		Attributes: []string{"LOGIN"},
		Roles:      []string{"pgedge_application_read_only"},
	})
	if err != nil {
		return fmt.Errorf("failed to generate create user role statements: %w", err)
	}

	if err := statements.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create RAG service user: %w", err)
	}

	return nil
}

func (r *RAGServiceUserRole) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *RAGServiceUserRole) Delete(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_instance_id", r.ServiceInstanceID).
		Str("database_id", r.DatabaseID).
		Str("username", r.Username).
		Logger()
	logger.Info().Msg("deleting RAG service user from database")

	conn, err := r.connectToColocatedPrimary(ctx, rc, logger, "postgres")
	if err != nil {
		// During deletion the database may already be gone or unreachable.
		logger.Warn().Err(err).Msg("failed to connect to co-located primary, skipping RAG user deletion")
		return nil
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", sanitizeIdentifier(r.Username)))
	if err != nil {
		logger.Warn().Err(err).Msg("failed to drop RAG user role, continuing anyway")
		return nil
	}

	logger.Info().Msg("RAG service user deleted successfully")
	return nil
}

// connectToColocatedPrimary finds the primary Postgres instance on the same
// host as this RAG service instance and returns an authenticated connection.
// Filtering by HostID ensures the role is created on the correct node, since
// CREATE ROLE is not replicated by Spock in a multi-active setup.
func (r *RAGServiceUserRole) connectToColocatedPrimary(ctx context.Context, rc *resource.Context, logger zerolog.Logger, dbName string) (*pgx.Conn, error) {
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	primaryInstanceID, err := r.resolveColocatedPrimary(ctx, dbSvc, logger)
	if err != nil {
		return nil, err
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

// resolveColocatedPrimary fetches the database, selects co-located instances,
// and returns the primary instance ID via Patroni.
func (r *RAGServiceUserRole) resolveColocatedPrimary(ctx context.Context, dbSvc *database.Service, logger zerolog.Logger) (string, error) {
	db, err := dbSvc.GetDatabase(ctx, r.DatabaseID)
	if err != nil {
		if errors.Is(err, database.ErrDatabaseNotFound) {
			return "", fmt.Errorf("database not found: %w", err)
		}
		return "", fmt.Errorf("failed to get database: %w", err)
	}
	if len(db.Instances) == 0 {
		return "", fmt.Errorf("database has no instances")
	}
	candidates := r.colocatedInstances(db.Instances, logger)
	return r.findPrimaryAmong(ctx, dbSvc, candidates, logger), nil
}

// colocatedInstances returns the subset of instances that share r.HostID.
// Falls back to all instances if none are co-located.
func (r *RAGServiceUserRole) colocatedInstances(all []*database.Instance, logger zerolog.Logger) []*database.Instance {
	candidates := make([]*database.Instance, 0, len(all))
	for _, inst := range all {
		if inst.HostID == r.HostID {
			candidates = append(candidates, inst)
		}
	}
	if len(candidates) == 0 {
		logger.Warn().Str("host_id", r.HostID).Msg("no co-located Postgres instances found, falling back to all instances")
		return all
	}
	return candidates
}

// findPrimaryAmong queries Patroni for each candidate and returns the primary
// instance ID. Falls back to the first candidate if none can be determined.
func (r *RAGServiceUserRole) findPrimaryAmong(ctx context.Context, dbSvc *database.Service, candidates []*database.Instance, logger zerolog.Logger) string {
	for _, inst := range candidates {
		connInfo, err := dbSvc.GetInstanceConnectionInfo(ctx, r.DatabaseID, inst.InstanceID)
		if err != nil {
			continue
		}
		primaryID, err := database.GetPrimaryInstanceID(ctx, patroni.NewClient(connInfo.PatroniURL(), nil), 10*time.Second)
		if err == nil && primaryID != "" {
			return primaryID
		}
	}
	logger.Warn().Msg("could not determine primary instance, using first co-located instance")
	return candidates[0].InstanceID
}
