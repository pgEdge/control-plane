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
// We key it by the slot name and provider node (where the slot lives).
func ReplicationSlotAdvanceFromCTSResourceIdentifier(dbName, providerNode, subscriberNode string) resource.Identifier {
	slotName := slotNameFor(dbName, providerNode, subscriberNode)
	return resource.Identifier{
		Type: ResourceTypeReplicationSlotAdvanceFromCTS,
		ID:   slotName, // e.g. spk_<db>_<provider>_sub_<subscriber>_<provider>
	}
}

// ReplicationSlotAdvanceFromCTSResource advances the replication slot on the provider
// to the LSN derived from the commit timestamp captured in lag_tracker.
type ReplicationSlotAdvanceFromCTSResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`   // slot lives here
	SubscriberNode string `json:"subscriber_node"` // target/receiver node
	// internal dependency wiring
	dependentResources []resource.Identifier
}

func NewReplicationSlotAdvanceFromCTSResource(dbName, providerNode, subscriberNode string) *ReplicationSlotAdvanceFromCTSResource {
	return &ReplicationSlotAdvanceFromCTSResource{
		DatabaseName:   dbName,
		ProviderNode:   providerNode,
		SubscriberNode: subscriberNode,
	}
}

func (r *ReplicationSlotAdvanceFromCTSResource) ResourceVersion() string { return "1" }

// No diff-ignore fields needed; this always executes idempotently when asked.
func (r *ReplicationSlotAdvanceFromCTSResource) DiffIgnore() []string { return nil }

// Execute on the provider node (the slot exists there).
func (r *ReplicationSlotAdvanceFromCTSResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   r.ProviderNode,
	}
}

func (r *ReplicationSlotAdvanceFromCTSResource) Identifier() resource.Identifier {
	return ReplicationSlotAdvanceFromCTSResourceIdentifier(r.DatabaseName, r.ProviderNode, r.SubscriberNode)
}

func (r *ReplicationSlotAdvanceFromCTSResource) AddDependentResource(dep resource.Identifier) {
	r.dependentResources = append(r.dependentResources, dep)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),                         // must run on provider
		LagTrackerCommitTSIdentifier(r.ProviderNode, r.SubscriberNode), // need commit_ts first
	}
	deps = append(deps, r.dependentResources...)
	return deps
}

func (r *ReplicationSlotAdvanceFromCTSResource) Refresh(ctx context.Context, rc *resource.Context) error {
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

	// Build and execute replication slot advance query using commit timestamp
	stmt := postgres.AdvanceReplicationSlotFromCommitTS(r.DatabaseName, r.ProviderNode, r.SubscriberNode, commitTS)
	if err := stmt.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to advance replication slot from commit ts: %w", err)
	}

	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Update(ctx context.Context, rc *resource.Context) error {
	// No-op; advance is a point-in-time action directed by dependencies.
	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op; advancing a slot does not create durable config to remove.
	return nil
}

// --- helpers ---

// slotNameFor matches the naming used elsewhere in the codebase:
//
//	sub name:  sub_<target>_<source>  (created on the target/subscriber)
//	slot name: spk_<db>_<provider>_<sub name>
func slotNameFor(dbName, providerNode, subscriberNode string) string {
	sub := fmt.Sprintf("sub_%s_%s", providerNode, subscriberNode)
	return fmt.Sprintf("spk_%s_%s_%s", dbName, providerNode, sub)
}
