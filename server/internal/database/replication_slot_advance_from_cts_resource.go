package database

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// Ensure interface conformance
var _ resource.Resource = (*ReplicationSlotAdvanceFromCTSResource)(nil)

const ResourceTypeReplicationSlotAdvanceFromCTS resource.Type = "database.replication_slot_advance_from_cts"

// ReplicationSlotAdvanceFromCTSResourceIdentifier creates a stable identifier for this resource.
func ReplicationSlotAdvanceFromCTSResourceIdentifier(providerNode, subscriberNode string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationSlotAdvanceFromCTS,
		ID:   providerNode + subscriberNode,
	}
}

// ReplicationSlotAdvanceFromCTSResource advances the replication slot on the provider
// to the LSN derived from the commit timestamp captured in lag_tracker.
type ReplicationSlotAdvanceFromCTSResource struct {
	ProviderNode   string `json:"provider_node"`   // slot lives here
	SubscriberNode string `json:"subscriber_node"` // target/receiver node
}

func (r *ReplicationSlotAdvanceFromCTSResource) ResourceVersion() string { return "1" }

// No diff-ignore fields needed; this always executes idempotently when asked.
func (r *ReplicationSlotAdvanceFromCTSResource) DiffIgnore() []string { return nil }

// Execute on the provider node (the slot exists there).
func (r *ReplicationSlotAdvanceFromCTSResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.ProviderNode)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Identifier() resource.Identifier {
	return ReplicationSlotAdvanceFromCTSResourceIdentifier(r.ProviderNode, r.SubscriberNode)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),                         // must run on provider
		LagTrackerCommitTSIdentifier(r.ProviderNode, r.SubscriberNode), // need commit_ts first
	}
}

func (r *ReplicationSlotAdvanceFromCTSResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Create(ctx context.Context, rc *resource.Context) error {
	// Fetch commit timestamp from lag tracker resource
	lagTracker, err := resource.FromContext[*LagTrackerCommitTimestampResource](
		rc,
		LagTrackerCommitTSIdentifier(r.ProviderNode, r.SubscriberNode),
	)
	if err != nil {
		return fmt.Errorf("failed to get lag tracker resource for %q->%q: %w", r.ProviderNode, r.SubscriberNode, err)
	}
	if lagTracker.CommitTimestamp == nil || lagTracker.CommitTimestamp.IsZero() {
		// Skip advancing if no commit timestamp available
		return nil
	}

	commitTS := *lagTracker.CommitTimestamp

	// Connect to provider (slot lives here)
	provider, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instance for node %q: %w", r.ProviderNode, err)
	}
	conn, err := provider.Connection(ctx, rc, provider.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to provider %q: %w", r.ProviderNode, err)
	}
	defer conn.Close(ctx)

	currentLSN, err := postgres.
		CurrentReplicationSlotLSN(
			provider.Spec.DatabaseName,
			r.ProviderNode,
			r.SubscriberNode).
		Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query current replication slot lsn: %w", err)
	}

	targetLSN, err := postgres.
		GetReplicationSlotLSNFromCommitTS(
			provider.Spec.DatabaseName,
			r.ProviderNode,
			r.SubscriberNode,
			commitTS).
		Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query target replication slot lsn: %w", err)
	}

	if targetLSN <= currentLSN {
		// No need to advance if the slot is already ahead of the commit
		// timestamp
		return nil
	}

	err = postgres.
		AdvanceReplicationSlotToLSN(
			provider.Spec.DatabaseName,
			r.ProviderNode,
			r.SubscriberNode,
			targetLSN).
		Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to advance replication slot: %w", err)
	}

	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op; advancing a slot does not create durable config to remove.
	return nil
}
