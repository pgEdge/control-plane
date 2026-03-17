package activities

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// ValidatePostgRESTPrereqsInput carries the database and PostgREST config fields
// needed for the preflight check.
type ValidatePostgRESTPrereqsInput struct {
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name"`
	// DBSchemas is the comma-separated list from the PostgREST config (e.g. "api,public").
	DBSchemas    string `json:"db_schemas"`
	DBAnonymRole string `json:"db_anon_role"`
}

// ValidatePostgRESTPrereqsOutput is empty on success.
type ValidatePostgRESTPrereqsOutput struct{}

// ExecuteValidatePostgRESTPrereqs schedules the preflight check on the local
// host queue.
func (a *Activities) ExecuteValidatePostgRESTPrereqs(
	ctx workflow.Context,
	input *ValidatePostgRESTPrereqsInput,
) workflow.Future[*ValidatePostgRESTPrereqsOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ValidatePostgRESTPrereqsOutput](
		ctx, options, a.ValidatePostgRESTPrereqs, input,
	)
}

// ValidatePostgRESTPrereqs connects to the target database and verifies that
// all configured schemas and the anonymous role exist before PostgREST is
// provisioned. A missing schema or role would cause PostgREST to start but
// return 404 for every request.
func (a *Activities) ValidatePostgRESTPrereqs(
	ctx context.Context,
	input *ValidatePostgRESTPrereqsInput,
) (*ValidatePostgRESTPrereqsOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"database_name", input.DatabaseName,
	)
	logger.Info("running PostgREST preflight checks")

	conn, err := a.postgrestConnectToPrimary(ctx, input.DatabaseID, input.DatabaseName)
	if err != nil {
		return nil, fmt.Errorf("preflight: failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)

	var errs []error

	// Check each schema in the comma-separated db_schemas list.
	for _, schema := range splitSchemas(input.DBSchemas) {
		var exists bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)",
			schema,
		).Scan(&exists); err != nil {
			errs = append(errs, fmt.Errorf("failed to check schema %q: %w", schema, err))
			continue
		}
		if !exists {
			errs = append(errs, fmt.Errorf(
				"schema %q does not exist in database %q; create it before deploying PostgREST",
				schema, input.DatabaseName,
			))
		}
	}

	// Check that the anonymous role exists.
	if input.DBAnonymRole != "" {
		var exists bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)",
			input.DBAnonymRole,
		).Scan(&exists); err != nil {
			errs = append(errs, fmt.Errorf("failed to check role %q: %w", input.DBAnonymRole, err))
		} else if !exists {
			errs = append(errs, fmt.Errorf(
				"role %q does not exist in database %q; create it before deploying PostgREST",
				input.DBAnonymRole, input.DatabaseName,
			))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	logger.Info("PostgREST preflight checks passed")
	return &ValidatePostgRESTPrereqsOutput{}, nil
}

// postgrestConnectToPrimary finds the current primary Postgres instance and
// returns an authenticated connection to the named application database.
// The caller is responsible for closing the connection.
func (a *Activities) postgrestConnectToPrimary(
	ctx context.Context,
	databaseID string,
	databaseName string,
) (*pgx.Conn, error) {
	db, err := a.DatabaseService.GetDatabase(ctx, databaseID)
	if err != nil {
		if errors.Is(err, database.ErrDatabaseNotFound) {
			return nil, fmt.Errorf("database not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if len(db.Instances) == 0 {
		return nil, fmt.Errorf("database has no instances")
	}

	// Find the current primary via Patroni.
	var primaryInstanceID string
	var fallbackInstanceID string
	for _, inst := range db.Instances {
		connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, databaseID, inst.InstanceID)
		if err != nil {
			continue
		}
		if fallbackInstanceID == "" {
			fallbackInstanceID = inst.InstanceID
		}
		patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
		primaryID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 10*time.Second)
		if err == nil && primaryID != "" {
			primaryInstanceID = primaryID
			break
		}
	}
	if primaryInstanceID == "" {
		if fallbackInstanceID == "" {
			return nil, fmt.Errorf("failed to resolve connection info for any instance")
		}
		// The prereq queries are read-only, so any reachable instance is sufficient.
		primaryInstanceID = fallbackInstanceID
	}

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, databaseID, primaryInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	certSvc, err := do.Invoke[*certificates.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate service: %w", err)
	}

	tlsConfig, err := certSvc.PostgresUserTLS(ctx, primaryInstanceID, connInfo.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: connInfo.AdminDSN(databaseName),
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return conn, nil
}

// splitSchemas splits a comma-separated schema list and trims whitespace.
func splitSchemas(s string) []string {
	parts := strings.Split(s, ",")
	schemas := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			schemas = append(schemas, p)
		}
	}
	return schemas
}
