package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PostgRESTPreflightResource)(nil)

const ResourceTypePostgRESTPreflightResource resource.Type = "swarm.postgrest_preflight"

func PostgRESTPreflightResourceIdentifier(serviceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceID,
		Type: ResourceTypePostgRESTPreflightResource,
	}
}

// PostgRESTPreflightResource validates that the configured schemas and anon role
// exist in the database before PostgREST is provisioned. It uses PrimaryExecutor
// so the check runs on a host with guaranteed database connectivity.
type PostgRESTPreflightResource struct {
	ServiceID    string `json:"service_id"`
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name"`
	NodeName     string `json:"node_name"`
	DBSchemas    string `json:"db_schemas"`
	DBAnonRole   string `json:"db_anon_role"`
}

func (r *PostgRESTPreflightResource) ResourceVersion() string { return "1" }
func (r *PostgRESTPreflightResource) DiffIgnore() []string    { return nil }

func (r *PostgRESTPreflightResource) Identifier() resource.Identifier {
	return PostgRESTPreflightResourceIdentifier(r.ServiceID)
}

func (r *PostgRESTPreflightResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *PostgRESTPreflightResource) Dependencies() []resource.Identifier {
	return nil
}

func (r *PostgRESTPreflightResource) TypeDependencies() []resource.Type {
	return nil
}

// Refresh validates prerequisites and returns ErrNotFound only when validation
// fails, triggering a Create that surfaces the error. When prerequisites are
// satisfied the resource is considered up-to-date (no permadrift).
func (r *PostgRESTPreflightResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := r.validate(ctx, rc); err != nil {
		return fmt.Errorf("%w: %s", resource.ErrNotFound, err.Error())
	}
	return nil
}

func (r *PostgRESTPreflightResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.validate(ctx, rc)
}

func (r *PostgRESTPreflightResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.validate(ctx, rc)
}

func (r *PostgRESTPreflightResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *PostgRESTPreflightResource) validate(ctx context.Context, rc *resource.Context) error {
	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("preflight: failed to get primary instance: %w", err)
	}
	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("preflight: failed to connect to database %s on node %s: %w", r.DatabaseName, r.NodeName, err)
	}
	defer conn.Close(ctx)

	var errs []error

	for _, schema := range splitSchemas(r.DBSchemas) {
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
				schema, r.DatabaseName,
			))
		}
	}

	if r.DBAnonRole != "" {
		var exists bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)",
			r.DBAnonRole,
		).Scan(&exists); err != nil {
			errs = append(errs, fmt.Errorf("failed to check role %q: %w", r.DBAnonRole, err))
		} else if !exists {
			errs = append(errs, fmt.Errorf(
				"role %q does not exist on the Postgres cluster; create it before deploying PostgREST",
				r.DBAnonRole,
			))
		}
	}

	return errors.Join(errs...)
}

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
