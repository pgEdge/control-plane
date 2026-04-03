package swarm

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*RAGServiceUserRole)(nil)

const ResourceTypeRAGServiceUserRole resource.Type = "swarm.rag_service_user_role"

func RAGServiceUserRoleIdentifier(serviceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID,
		Type: ResourceTypeRAGServiceUserRole,
	}
}

// RAGServiceUserRole manages the Postgres role for a RAG service.
// The role is created on the primary of the co-located Postgres instance
// and granted the pgedge_application_read_only built-in role.
// Spock replicates the role to every other node because we connect via r.DatabaseName.
type RAGServiceUserRole struct {
	ServiceID    string `json:"service_id"`
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name"`
	NodeName     string `json:"node_name"` // Database node name for PrimaryExecutor routing
	Username     string `json:"username"`
	Password     string `json:"password"` // Generated on Create, persisted in state
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
	return RAGServiceUserRoleIdentifier(r.ServiceID)
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
		Str("service_id", r.ServiceID).
		Str("database_id", r.DatabaseID).
		Logger()

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)

	needsCreate, err := postgres.UserRoleNeedsCreate(r.Username).Scalar(ctx, conn)
	if err != nil {
		logger.Warn().Err(err).Msg("pg_roles query failed")
		return fmt.Errorf("pg_roles query failed: %w", err)
	}
	if needsCreate {
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
		Str("service_id", r.ServiceID).
		Str("database_id", r.DatabaseID).
		Logger()
	logger.Info().Msg("creating RAG service user role")

	r.Username = database.GenerateServiceUsername(r.ServiceID, ServiceUserRoleRO)
	if r.Password == "" {
		password, err := utils.RandomString(32)
		if err != nil {
			return fmt.Errorf("failed to generate password: %w", err)
		}
		r.Password = password
	}

	if err := r.createRole(ctx, rc); err != nil {
		return fmt.Errorf("failed to create RAG service user role: %w", err)
	}

	logger.Info().Str("username", r.Username).Msg("RAG service user role created successfully")
	return nil
}

func (r *RAGServiceUserRole) createRole(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)

	statements, err := postgres.CreateUserRole(postgres.UserRoleOptions{
		Name:       r.Username,
		Password:   r.Password,
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
		Str("service_id", r.ServiceID).
		Str("database_id", r.DatabaseID).
		Str("username", r.Username).
		Logger()
	logger.Info().Msg("deleting RAG service user from database")

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		// During deletion the database may already be gone or unreachable.
		logger.Warn().Err(err).Msg("failed to get primary instance, skipping RAG user deletion")
		return nil
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		// During deletion the database may already be gone or unreachable.
		logger.Warn().Err(err).Msg("failed to connect to database, skipping RAG user deletion")
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
