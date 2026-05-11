package database

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ReplicationOriginAdvanceResource)(nil)

const ResourceTypeReplicationOriginAdvance resource.Type = "database.replication_origin_advance"

func ReplicationOriginAdvanceResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationOriginAdvance,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

// ReplicationOriginAdvanceResource advances the replication origin on the
// subscriber to the LSN that ReplicationSlotAdvanceFromCTSResource recorded
// after advancing the provider-side slot. Both must be updated together to
// prevent the apply worker from replaying historical WAL from 0/0.
//
// Runs on the subscriber's host (cross-host connections are not allowed, so
// this must be separate from ReplicationSlotAdvanceFromCTSResource which runs
// on the provider's host).
type ReplicationOriginAdvanceResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

func (r *ReplicationOriginAdvanceResource) ResourceVersion() string { return "1" }
func (r *ReplicationOriginAdvanceResource) DiffIgnore() []string    { return nil }

func (r *ReplicationOriginAdvanceResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.SubscriberNode)
}

func (r *ReplicationOriginAdvanceResource) Identifier() resource.Identifier {
	return ReplicationOriginAdvanceResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName)
}

func (r *ReplicationOriginAdvanceResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		ReplicationSlotAdvanceFromCTSResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	}
}

func (r *ReplicationOriginAdvanceResource) TypeDependencies() []resource.Type { return nil }

func (r *ReplicationOriginAdvanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationOriginAdvanceResource) Create(ctx context.Context, rc *resource.Context) error {
	slotAdvance, err := resource.FromContext[*ReplicationSlotAdvanceFromCTSResource](
		rc,
		ReplicationSlotAdvanceFromCTSResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	)
	if err != nil {
		return fmt.Errorf("failed to get slot advance resource: %w", err)
	}
	if slotAdvance.AdvancedToLSN == "" {
		// Slot advance was skipped (slot active or no commit timestamp) — nothing to do.
		return nil
	}

	subscriber, err := GetPrimaryInstance(ctx, rc, r.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance for node %q: %w", r.SubscriberNode, err)
	}
	conn, err := subscriber.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to subscriber %q: %w", r.SubscriberNode, err)
	}
	defer conn.Close(ctx)

	slotName := postgres.ReplicationSlotName(r.DatabaseName, r.ProviderNode, r.SubscriberNode)

	if err := postgres.EnsureReplicationOriginExists(slotName).Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to ensure replication origin on subscriber %q: %w", r.SubscriberNode, err)
	}
	if err := postgres.AdvanceReplicationOrigin(slotName, slotAdvance.AdvancedToLSN).Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to advance replication origin on subscriber %q: %w", r.SubscriberNode, err)
	}
	return nil
}

func (r *ReplicationOriginAdvanceResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *ReplicationOriginAdvanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
