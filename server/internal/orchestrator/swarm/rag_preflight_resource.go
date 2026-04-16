package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*RAGPreflightResource)(nil)

const ResourceTypeRAGPreflightResource resource.Type = "swarm.rag_preflight"

func RAGPreflightResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGPreflightResource,
	}
}

// RAGPreflightResource verifies that the Postgres database is available and
// the connect_as user exists before the RAG config file is written and the
// Docker service is started. It uses PrimaryExecutor so it runs on a host
// with guaranteed database connectivity.
//
// Refresh returns ErrNotFound until all checks pass, causing the resource
// engine to retry on each reconciliation cycle — effectively acting as a
// readiness gate that prevents the RAG container from starting while Patroni
// is still bootstrapping.
type RAGPreflightResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	NodeName          string `json:"node_name"`
	DatabaseName      string `json:"database_name"`
	// ConnectAsUsername is the database role the RAG service connects as.
	// It must be declared in database_users.
	ConnectAsUsername string `json:"connect_as_username"`
}

func (r *RAGPreflightResource) ResourceVersion() string { return "1" }
func (r *RAGPreflightResource) DiffIgnore() []string    { return nil }

func (r *RAGPreflightResource) Identifier() resource.Identifier {
	return RAGPreflightResourceIdentifier(r.ServiceInstanceID)
}

func (r *RAGPreflightResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *RAGPreflightResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		database.NodeResourceIdentifier(r.NodeName),
		database.PostgresDatabaseResourceIdentifier(r.NodeName, r.DatabaseName),
	}
}

func (r *RAGPreflightResource) TypeDependencies() []resource.Type {
	return nil
}

// Refresh returns ErrNotFound when the database or connect_as user is not yet
// ready, causing the resource engine to retry rather than treating the service
// as permanently failed.
func (r *RAGPreflightResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := r.validate(ctx, rc); err != nil {
		return fmt.Errorf("%w: %s", resource.ErrNotFound, err.Error())
	}
	return nil
}

func (r *RAGPreflightResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.validate(ctx, rc)
}

func (r *RAGPreflightResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.validate(ctx, rc)
}

func (r *RAGPreflightResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *RAGPreflightResource) validate(ctx context.Context, rc *resource.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	primary, err := database.GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("preflight: failed to get primary instance: %w", err)
	}

	conn, err := primary.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("preflight: database %q is not yet available on node %s: %w",
			r.DatabaseName, r.NodeName, err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)",
		r.ConnectAsUsername,
	).Scan(&exists); err != nil {
		return fmt.Errorf("preflight: failed to check role %q: %w", r.ConnectAsUsername, err)
	}
	if !exists {
		return fmt.Errorf("preflight: role %q does not exist; ensure it is declared in database_users",
			r.ConnectAsUsername)
	}

	return nil
}
