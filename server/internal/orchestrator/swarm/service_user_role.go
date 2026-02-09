package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ServiceUserRole)(nil)

const ResourceTypeServiceUserRole resource.Type = "swarm.service_user_role"

// sanitizeIdentifier quotes a string for use as a PostgreSQL identifier.
// It doubles any internal double-quotes and wraps the result in double-quotes.
func sanitizeIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func ServiceUserRoleIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceUserRole,
	}
}

// ServiceUserRole manages the lifecycle of a database user for a service instance.
//
// This resource handles cleanup of database users when service instances are deleted.
// User creation is performed by the CreateServiceUser activity during provisioning,
// but deletion requires infrastructure access and is therefore handled by this resource.
type ServiceUserRole struct {
	ServiceInstanceID string `json:"service_instance_id"`
	DatabaseID        string `json:"database_id"`
	DatabaseName      string `json:"database_name"`
	Username          string `json:"username"`
	HostID            string `json:"host_id"`
}

func (r *ServiceUserRole) ResourceVersion() string {
	return "1"
}

func (r *ServiceUserRole) DiffIgnore() []string {
	return nil
}

func (r *ServiceUserRole) Identifier() resource.Identifier {
	return ServiceUserRoleIdentifier(r.ServiceInstanceID)
}

func (r *ServiceUserRole) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *ServiceUserRole) Dependencies() []resource.Identifier {
	// No dependencies - this resource can be created/deleted independently
	return nil
}

func (r *ServiceUserRole) Refresh(ctx context.Context, rc *resource.Context) error {
	// Nothing to refresh - user existence is managed by Create/Delete
	return nil
}

func (r *ServiceUserRole) Create(ctx context.Context, rc *resource.Context) error {
	// User was already created by the CreateServiceUser activity during provisioning.
	// This resource only handles deletion cleanup.
	return nil
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
		Str("service_instance_id", r.ServiceInstanceID).
		Str("database_id", r.DatabaseID).
		Str("username", r.Username).
		Logger()
	logger.Info().Msg("deleting service user from database")

	orch, err := do.Invoke[database.Orchestrator](rc.Injector)
	if err != nil {
		return err
	}

	// Get database service to find an instance to connect to
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}

	db, err := dbSvc.GetDatabase(ctx, r.DatabaseID)
	if err != nil {
		if errors.Is(err, database.ErrDatabaseNotFound) {
			logger.Info().Msg("database not found, skipping user deletion")
			return nil
		}
		return fmt.Errorf("failed to get database: %w", err)
	}

	if len(db.Instances) == 0 {
		logger.Info().Msg("database has no instances, skipping user deletion")
		return nil
	}

	// Connect to primary instance (or any available instance)
	var primaryInstanceID string
	for _, inst := range db.Instances {
		connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.DatabaseID, inst.InstanceID)
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
		// Fallback: use first available instance
		primaryInstanceID = db.Instances[0].InstanceID
		logger.Warn().Msg("could not determine primary instance, using first available instance")
	}

	// Get connection info for the primary instance
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.DatabaseID, primaryInstanceID)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get instance connection info, skipping user deletion")
		return nil
	}

	// Get certificate service for TLS authentication
	certSvc, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return fmt.Errorf("failed to get certificate service: %w", err)
	}

	// Create TLS config with pgedge user certificates
	tlsConfig, err := certSvc.PostgresUserTLS(ctx, primaryInstanceID, connInfo.InstanceHostname, "pgedge")
	if err != nil {
		logger.Warn().Err(err).Msg("failed to create TLS config, skipping user deletion")
		return nil
	}

	// Connect to the postgres system database
	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: connInfo.AdminDSN("postgres"),
		TLS: tlsConfig,
	})
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to database, skipping user deletion")
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
