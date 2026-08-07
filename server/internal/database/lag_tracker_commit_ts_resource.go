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

func LagTrackerCommitTSIdentifier(originNode, receiverNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeLagTrackerCommitTS,
		ID:   fmt.Sprintf("%s:%s:%s", originNode, receiverNode, databaseName),
	}
}

type LagTrackerCommitTimestampResource struct {
	// Planner fields
	OriginNode   string `json:"origin_node"`
	ReceiverNode string `json:"receiver_node"`
	DatabaseName string `json:"database_name"`

	// Dependency wiring
	ExtraDependencies []resource.Identifier `json:"dependent_resources,omitempty"`

	// Output (filled at Refresh/Create time)
	CommitTimestamp *time.Time `json:"commit_timestamp,omitempty"`

	// ProgressLSN is the receiver's own spock.progress.remote_lsn (or
	// remote_commit_lsn on Spock 6+) for the origin node, read on the same
	// connection as CommitTimestamp above since both need to run on the
	// receiver's host. Used by ReplicationSlotAdvanceFromCTSResource as the
	// resume LSN, in place of calling spock.get_lsn_from_commit_ts() on the
	// provider — see that resource for why. nil if no progress entry
	// exists yet.
	ProgressLSN *string `json:"progress_lsn,omitempty"`
}

func (r *LagTrackerCommitTimestampResource) ResourceVersion() string { return "1" }
func (r *LagTrackerCommitTimestampResource) DiffIgnore() []string {
	return []string{"commit_timestamp", "progress_lsn"}
}

func (r *LagTrackerCommitTimestampResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.ReceiverNode)
}

func (r *LagTrackerCommitTimestampResource) Identifier() resource.Identifier {
	return LagTrackerCommitTSIdentifier(r.OriginNode, r.ReceiverNode, r.DatabaseName)
}

func (r *LagTrackerCommitTimestampResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		PostgresDatabaseResourceIdentifier(r.ReceiverNode, r.DatabaseName),
		PostgresDatabaseResourceIdentifier(r.OriginNode, r.DatabaseName),
	}
	deps = append(deps, r.ExtraDependencies...)
	return deps
}

func (r *LagTrackerCommitTimestampResource) TypeDependencies() []resource.Type {
	return nil
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

	ts, err := postgres.LagTrackerCommitTimestamp(r.OriginNode, r.ReceiverNode).Scalar(ctx, conn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			r.CommitTimestamp = nil
		} else {
			return fmt.Errorf("failed to query lag tracker commit timestamp: %w", err)
		}
	} else {
		r.CommitTimestamp = &ts
	}

	var spockMajor uint64
	if instance.Spec != nil && instance.Spec.PgEdgeVersion != nil && instance.Spec.PgEdgeVersion.SpockVersion != nil {
		if major, ok := instance.Spec.PgEdgeVersion.SpockVersion.Major(); ok {
			spockMajor = major
		}
	}
	progressLSN, err := postgres.GetSpockProgressLSN(spockMajor, r.OriginNode).Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query spock progress for origin %q: %w", r.OriginNode, err)
	}
	r.ProgressLSN = progressLSN

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
