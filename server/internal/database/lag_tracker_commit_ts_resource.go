package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LagTrackerCommitTimestampResource)(nil)

const ResourceTypeLagTrackerCommitTS resource.Type = "database.lag_tracker_commit_ts"

func LagTrackerCommitTSIdentifier(originNode, receiverNode string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeLagTrackerCommitTS,
		ID:   originNode + receiverNode,
	}
}

type LagTrackerCommitTimestampResource struct {
	// Planner fields
	OriginNode   string `json:"origin_node"`
	ReceiverNode string `json:"receiver_node"`

	// Dependency wiring
	ExtraDependencies []resource.Identifier `json:"dependent_resources,omitempty"`

	// Output (filled at Refresh/Create time)
	CommitTimestamp *time.Time `json:"commit_timestamp,omitempty"`
}

func NewLagTrackerCommitTimestampResource(originNode, receiverNode string) *LagTrackerCommitTimestampResource {
	return &LagTrackerCommitTimestampResource{
		OriginNode:   originNode,
		ReceiverNode: receiverNode,
	}
}

func (r *LagTrackerCommitTimestampResource) ResourceVersion() string { return "1" }
func (r *LagTrackerCommitTimestampResource) DiffIgnore() []string {
	return []string{"commit_timestamp"}
}

func (r *LagTrackerCommitTimestampResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   r.ReceiverNode,
	}
}

func (r *LagTrackerCommitTimestampResource) Identifier() resource.Identifier {
	return LagTrackerCommitTSIdentifier(r.OriginNode, r.ReceiverNode)
}

func (r *LagTrackerCommitTimestampResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(r.ReceiverNode),
	}
	deps = append(deps, r.ExtraDependencies...)
	return deps
}

func (r *LagTrackerCommitTimestampResource) Refresh(ctx context.Context, rc *resource.Context) error {
	// Connect to receiver node
	instance, err := GetPrimaryInstance(ctx, rc, r.ReceiverNode)
	if err != nil {
		return fmt.Errorf("failed to get instance for node %s: %w", r.ReceiverNode, err)
	}

	conn, err := instance.Connection(ctx, rc, instance.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close(ctx)

	ts, err := postgres.LagTrackerCommitTimestamp(r.OriginNode, r.ReceiverNode).Row(ctx, conn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			r.CommitTimestamp = nil
			return nil // skip, no rows
		}
		return fmt.Errorf("failed to query lag tracker commit timestamp: %w", err)
	}

	// Convert time.Time -> string (RFC3339Nano) for storage in resource

	r.CommitTimestamp = &ts
	return nil
}

func (r *LagTrackerCommitTimestampResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}
func (r *LagTrackerCommitTimestampResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}
func (r *LagTrackerCommitTimestampResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
