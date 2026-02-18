package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ReplicationSlotResource)(nil)

const ResourceTypeReplicationSlot resource.Type = "database.replication_slot"

func ReplicationSlotResourceIdentifier(providerNode, subscriberNode string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationSlot,
		ID:   providerNode + subscriberNode,
	}
}

// ReplicationSlotResource represents the replication slot on a provider node
// that serves a subscription from a subscriber node. It only implements the
// Delete lifecycle method: when a subscription is removed, this resource
// ensures the corresponding replication slot is dropped on the provider,
// preventing orphaned slots from accumulating WAL.
type ReplicationSlotResource struct {
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

func (r *ReplicationSlotResource) ResourceVersion() string {
	return "1"
}

func (r *ReplicationSlotResource) DiffIgnore() []string {
	return nil
}

func (r *ReplicationSlotResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.ProviderNode)
}

func (r *ReplicationSlotResource) Identifier() resource.Identifier {
	return ReplicationSlotResourceIdentifier(r.ProviderNode, r.SubscriberNode)
}

func (r *ReplicationSlotResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),
	}
}

func (r *ReplicationSlotResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotResource) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotResource) Delete(ctx context.Context, rc *resource.Context) error {
	provider, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			// Provider instance doesn't exist — slot is already gone
			return nil
		}
		return fmt.Errorf("failed to get primary instance: %w", err)
	}

	conn, err := provider.Connection(ctx, rc, provider.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to provider instance: %w", err)
	}
	defer conn.Close(ctx)

	dbName := provider.Spec.DatabaseName

	// Terminate any active walsender using this slot. This is necessary
	// when the subscriber has gone down and the walsender hasn't detected
	// the broken connection yet — pg_drop_replication_slot fails on active
	// slots.
	if err := postgres.TerminateReplicationSlot(dbName, r.ProviderNode, r.SubscriberNode).
		Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to terminate walsender for replication slot %s->%s: %w", r.ProviderNode, r.SubscriberNode, err)
	}

	if err := postgres.DropReplicationSlot(dbName, r.ProviderNode, r.SubscriberNode).
		Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to drop replication slot %s->%s: %w", r.ProviderNode, r.SubscriberNode, err)
	}

	return nil
}
