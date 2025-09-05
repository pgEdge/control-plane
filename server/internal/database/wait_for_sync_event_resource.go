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

func WaitForSyncEventResourceIdentifier(subscriberNode string, providerNode string) resource.Identifier {
	return resource.Identifier{
		ID:   subscriberNode + providerNode,
		Type: ResourceTypeWaitForSyncEvent,
	}
}

type WaitForSyncEventResource struct {
	SubscriberNode     string                `json:"subscriber_node"`
	ProviderNode       string                `json:"provider_node"`
	DependentResources []resource.Identifier `json:"dependent_resources"`
}

func NewWaitForSyncEventResource(subscriberNode string, providerNode string) *WaitForSyncEventResource {
	return &WaitForSyncEventResource{
		SubscriberNode: subscriberNode,
		ProviderNode:   providerNode,
	}
}

func (r *WaitForSyncEventResource) ResourceVersion() string {
	return "1"
}

func (r *WaitForSyncEventResource) DiffIgnore() []string {
	return nil
}

func (r *WaitForSyncEventResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   r.SubscriberNode,
	}
}

func (r *WaitForSyncEventResource) Identifier() resource.Identifier {
	return WaitForSyncEventResourceIdentifier(r.SubscriberNode, r.ProviderNode)
}

func (r *WaitForSyncEventResource) AddDependentResource(dep resource.Identifier) {
	r.DependentResources = append(r.DependentResources, dep)
}

func (r *WaitForSyncEventResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(r.SubscriberNode),
		SubscriptionResourceIdentifier(r.SubscriberNode, r.ProviderNode),
		SyncEventResourceIdentifier(r.SubscriberNode, r.ProviderNode),
	}

	deps = append(deps, r.DependentResources...)

	return deps
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

	syncEvent, err := resource.FromContext[*SyncEventResource](rc, SyncEventResourceIdentifier(r.SubscriberNode, r.ProviderNode))
	if err != nil {
		return fmt.Errorf("failed to get node %q: %w", r.ProviderNode, err)
	}
	if syncEvent.SyncEventLsn == "" {
		return fmt.Errorf("sync event LSN is empty on resource %q", syncEvent.Identifier())
	}

	// TODO: Set wait limit
	err = postgres.WaitForSyncEvent(r.ProviderNode, syncEvent.SyncEventLsn, 100).Exec(ctx, subscriberConn)
	if errors.Is(err, pgx.ErrNoRows) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to wait for sync event on subscriber: %w", err)
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
