package database

import (
	"context"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PeerCatchupResource)(nil)

const ResourceTypePeerCatchup resource.Type = "database.peer_catchup"

func PeerCatchupResourceIdentifier(sourceNode, peerNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypePeerCatchup,
		ID:   fmt.Sprintf("%s:%s:%s", sourceNode, peerNode, databaseName),
	}
}

// PeerCatchupResource waits until the source node's apply progress from the
// peer node has reached the peer's sync event LSN. This ensures the COPY
// snapshot (Phase 5 source→new subscription) includes all peer writes up to
// the slot creation point, preventing data loss on add-node.
//
// Uses spock.progress.remote_lsn (apply progress at last committed
// transaction) rather than received_lsn, which can advance on keepalive
// messages before commits have been applied.
//
// Ref: zodan.sql lines 1455–1523, spock PR #392
type PeerCatchupResource struct {
	DatabaseName string `json:"database_name"`
	SourceNode   string `json:"source_node"` // node where we check progress
	PeerNode     string `json:"peer_node"`   // peer whose commits must be applied
}

func (r *PeerCatchupResource) ResourceVersion() string { return "1" }
func (r *PeerCatchupResource) DiffIgnore() []string    { return nil }

func (r *PeerCatchupResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.SourceNode)
}

func (r *PeerCatchupResource) Identifier() resource.Identifier {
	return PeerCatchupResourceIdentifier(r.SourceNode, r.PeerNode, r.DatabaseName)
}

func (r *PeerCatchupResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		SyncEventResourceIdentifier(r.PeerNode, r.SourceNode, r.DatabaseName),
	}
}

func (r *PeerCatchupResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *PeerCatchupResource) Refresh(ctx context.Context, rc *resource.Context) error {
	syncEvent, err := resource.FromContext[*SyncEventResource](
		rc,
		SyncEventResourceIdentifier(r.PeerNode, r.SourceNode, r.DatabaseName),
	)
	if err != nil {
		return fmt.Errorf("failed to get sync event for peer %q: %w", r.PeerNode, err)
	}
	if syncEvent.SyncEventLsn == "" {
		return resource.ErrNotFound
	}

	source, err := GetPrimaryInstance(ctx, rc, r.SourceNode)
	if err != nil {
		return fmt.Errorf("failed to get source instance for node %q: %w", r.SourceNode, err)
	}
	conn, err := source.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to source node %q: %w", r.SourceNode, err)
	}
	defer conn.Close(ctx)

	const pollInterval = 500 * time.Millisecond

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		reached, err := postgres.SpockProgressReachedLSN(r.PeerNode, syncEvent.SyncEventLsn).
			Scalar(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to query spock progress for peer %q: %w", r.PeerNode, err)
		}
		if reached {
			return nil
		}

		time.Sleep(pollInterval)
	}
}

func (r *PeerCatchupResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}

func (r *PeerCatchupResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *PeerCatchupResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
