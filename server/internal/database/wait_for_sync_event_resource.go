package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*WaitForSyncEventResource)(nil)

const ResourceTypeWaitForSyncEvent resource.Type = "database.wait_for_sync_event"

func WaitForSyncEventResourceIdentifier(providerNode, subscriberNode string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeWaitForSyncEvent,
		ID:   providerNode + subscriberNode,
	}
}

type WaitForSyncEventResource struct {
	SubscriberNode string `json:"subscriber_node"`
	ProviderNode   string `json:"provider_node"`
}

func (r *WaitForSyncEventResource) ResourceVersion() string {
	return "1"
}

func (r *WaitForSyncEventResource) DiffIgnore() []string {
	return nil
}

func (r *WaitForSyncEventResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.SubscriberNode)
}

func (r *WaitForSyncEventResource) Identifier() resource.Identifier {
	return WaitForSyncEventResourceIdentifier(r.ProviderNode, r.SubscriberNode)
}

func (r *WaitForSyncEventResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		SyncEventResourceIdentifier(r.ProviderNode, r.SubscriberNode),
	}
}

// Confirm synchronization by sending sync_event from provider and waiting for it on subscriber
func (r *WaitForSyncEventResource) Refresh(ctx context.Context, rc *resource.Context) error {
	// Get subscriber instance
	subscriber, err := GetPrimaryInstance(ctx, rc, r.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	subscriberConn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to subscriber database %q: %w", subscriber.Spec.DatabaseName, err)
	}
	defer subscriberConn.Close(ctx)

	// Wait for sync event on subscriber

	syncEvent, err := resource.FromContext[*SyncEventResource](rc, SyncEventResourceIdentifier(r.ProviderNode, r.SubscriberNode))
	if err != nil {
		return fmt.Errorf("failed to get sync event: %w", err)
	}
	if syncEvent.SyncEventLsn == "" {
		return fmt.Errorf("sync event LSN is empty on resource %q", syncEvent.Identifier())
	}

	// TODO: Set wait limit
	synced, err := postgres.WaitForSyncEvent(r.ProviderNode, syncEvent.SyncEventLsn, 100).Scalar(ctx, subscriberConn)
	if errors.Is(err, pgx.ErrNoRows) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to wait for sync event on subscriber: %w", err)
	}
	if !synced {
		return fmt.Errorf("replication sync not confirmed: provider=%s subscriber=%s lsn=%s timeout_seconds=%d",
			r.ProviderNode,
			r.SubscriberNode,
			syncEvent.SyncEventLsn,
			100,
		)
	}

	return nil
}

func (r *WaitForSyncEventResource) Create(ctx context.Context, rc *resource.Context) error {
	// Confirm sync is a no-op for create, just call Refresh
	return r.Refresh(ctx, rc)
}

func (r *WaitForSyncEventResource) Update(ctx context.Context, rc *resource.Context) error {
	// No-op for update
	return nil
}

func (r *WaitForSyncEventResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op for delete
	return nil
}
