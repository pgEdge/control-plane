package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

var _ resource.Resource = (*ServiceUserRole)(nil)

const ResourceTypeServiceUserRole resource.Type = "swarm.service_user_role"

// sanitizeIdentifier quotes a string for use as a PostgreSQL identifier.
// It doubles any internal double-quotes and wraps the result in double-quotes.
func sanitizeIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func ServiceUserRoleIdentifier(serviceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID,
		Type: ResourceTypeServiceUserRole,
	}
}

// ServiceUserRole manages the lifecycle of a database user for a service.
//
// This resource is keyed by ServiceID, so one role is shared across all
// instances of the same service. It handles creation, verification, and
// cleanup of database users. On Create, it generates a deterministic username
// and random password, creates the Postgres role, and stores the credentials
// in the resource state. On subsequent reconciliation cycles, the credentials
// are reused from the persisted state (no password regeneration).
//
// The role is created on the primary instance and Spock replicates it to all
// other nodes automatically.
type ServiceUserRole struct {
	ServiceID    string `json:"service_id"`
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name"`
	NodeName     string `json:"node_name"`    // Database node name for PrimaryExecutor routing
	ServiceType  string `json:"service_type"` // "mcp" or "postgrest" — controls role attributes and grants
	DBAnonRole   string `json:"db_anon_role"` // PostgREST only: anonymous role granted to the service user
	Username     string `json:"username"`
	Password     string `json:"password"` // Generated on Create, persisted in state
}

func (r *ServiceUserRole) ResourceVersion() string {
	return "4"
}

func (r *ServiceUserRole) DiffIgnore() []string {
	return []string{
		"/node_name",
		"/username",
		"/password",
	}
}

func (r *ServiceUserRole) Identifier() resource.Identifier {
	return ServiceUserRoleIdentifier(r.ServiceID)
}

func (r *ServiceUserRole) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *ServiceUserRole) Dependencies() []resource.Identifier {
	// No dependencies - this resource can be created/deleted independently
	return nil
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

	// Generate deterministic username and random password
	r.Username = database.GenerateServiceUsername(r.ServiceID)
	password, err := utils.RandomString(32)
	if err != nil {
		return fmt.Errorf("failed to generate password: %w", err)
	}
	r.Password = password

	if err := r.createUserRole(ctx, rc, logger); err != nil {
		return fmt.Errorf("failed to create service user role: %w", err)
	}

	logger.Info().Str("username", r.Username).Msg("service user role created successfully")
	return nil
}

func (r *ServiceUserRole) createUserRole(ctx context.Context, rc *resource.Context, logger zerolog.Logger) error {
	// Connect to the application database so schema-level grants resolve correctly.
	// The role is created on the primary and Spock replicates it to other nodes.
	conn, err := r.connectToPrimary(ctx, rc, logger, r.DatabaseName)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	attributes, grants := r.roleAttributesAndGrants()

	statements, err := postgres.CreateUserRole(postgres.UserRoleOptions{
		Name:       r.Username,
		Password:   r.Password,
		DBName:     r.DatabaseName,
		DBOwner:    false,
		Attributes: attributes,
	})
	if err != nil {
		return fmt.Errorf("failed to generate create user role statements: %w", err)
	}

	if err := statements.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create service user: %w", err)
	}

	if err := grants.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to grant service user permissions: %w", err)
	}

	return nil
}

// roleAttributesAndGrants returns the role attributes and SQL grant statements
// appropriate for the service type.
func (r *ServiceUserRole) roleAttributesAndGrants() ([]string, postgres.Statements) {
	switch r.ServiceType {
	case "postgrest":
		// NOINHERIT + GRANT <anon_role> enables PostgREST's SET ROLE mechanism.
		attributes := []string{"LOGIN", "NOINHERIT"}
		anonRole := r.DBAnonRole
		if anonRole == "" {
			anonRole = "pgedge_application_read_only"
		}
		grants := postgres.Statements{
			postgres.Statement{SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(r.Username))},
			postgres.Statement{SQL: fmt.Sprintf("GRANT %s TO %s;", sanitizeIdentifier(anonRole), sanitizeIdentifier(r.Username))},
		}
		return attributes, grants

	default: // "mcp" and future service types
		// MCP role: direct grants rather than role membership to avoid exposing
		// replication internals via pgedge_application_read_only.
		// https://github.com/pgEdge/pgedge-postgres-mcp/blob/main/docs/guide/security_mgmt.go
		attributes := []string{"LOGIN"}
		grants := postgres.Statements{
			postgres.Statement{SQL: fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", sanitizeIdentifier(r.DatabaseName), sanitizeIdentifier(r.Username))},
			postgres.Statement{SQL: fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s;", sanitizeIdentifier(r.Username))},
			postgres.Statement{SQL: fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s;", sanitizeIdentifier(r.Username))},
			postgres.Statement{SQL: fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %s;", sanitizeIdentifier(r.Username))},
			postgres.Statement{SQL: fmt.Sprintf("GRANT pg_read_all_settings TO %s;", sanitizeIdentifier(r.Username))},
		}
		return attributes, grants
	}
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

	conn, err := r.connectToPrimary(ctx, rc, logger, "postgres")
	if err != nil {
		// During deletion, connection failures are non-fatal — the database
		// may already be gone or unreachable.
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

// connectToPrimary finds the primary Postgres instance and returns an
// authenticated connection to it. The caller is responsible for closing
// the connection.
func (r *ServiceUserRole) connectToPrimary(ctx context.Context, rc *resource.Context, logger zerolog.Logger, dbName string) (*pgx.Conn, error) {
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

	// Find primary instance via Patroni
	var primaryInstanceID string
	for _, inst := range db.Instances {
		connInfo, err := dbSvc.GetInstanceConnectionInfo(ctx, r.DatabaseID, inst.InstanceID)
		if err != nil {
			continue
		}
		patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
		primaryID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 10*time.Second)
		if err == nil && primaryID != "" {
			primaryInstanceID = primaryID
			break
		}
	}
	if primaryInstanceID == "" {
		primaryInstanceID = db.Instances[0].InstanceID
		logger.Warn().Msg("could not determine primary instance, using first available instance")
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
