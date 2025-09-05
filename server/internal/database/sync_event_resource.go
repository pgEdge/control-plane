package database

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SyncEventResource)(nil)

const ResourceTypeSyncEvent resource.Type = "database.sync_event"

func SyncEventResourceIdentifier(subscriberNode string, providerNode string) resource.Identifier {
	return resource.Identifier{
		ID:   subscriberNode + providerNode,
		Type: ResourceTypeSyncEvent,
	}
}

type SyncEventResource struct {
	ProviderNode       string                `json:"provider_node"`
	SubscriberNode     string                `json:"subscriber_node"`
	SyncEventLsn       string                `json:"sync_event_lsn"`
	DependentResources []resource.Identifier `json:"dependent_resources"`
}

func NewSyncEventResource(subscriberNode string, providerNode string) *SyncEventResource {
	return &SyncEventResource{
		ProviderNode:       providerNode,
		SubscriberNode:     subscriberNode,
		DependentResources: []resource.Identifier{},
	}
}

func (r *SyncEventResource) ResourceVersion() string {
	return "1"
}

func (r *SyncEventResource) DiffIgnore() []string {
	return nil
}

func (r *SyncEventResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   r.ProviderNode,
	}
}

func (r *SyncEventResource) Identifier() resource.Identifier {
	return SyncEventResourceIdentifier(r.SubscriberNode, r.ProviderNode)
}

func (r *SyncEventResource) AddDependentResource(dep resource.Identifier) {
	r.DependentResources = append(r.DependentResources, dep)
}

func (r *SyncEventResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),
		SubscriptionResourceIdentifier(r.SubscriberNode, r.ProviderNode),
	}

	deps = append(deps, r.DependentResources...)
	return deps
}

// Confirm synchronization by sending sync_event from provider and waiting for it on subscriber
func (r *SyncEventResource) Refresh(ctx context.Context, rc *resource.Context) error {
	// Get provider instance
	provider, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instance: %w", err)
	}
	providerConn, err := provider.Connection(ctx, rc, provider.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to provider database %q: %w", provider.Spec.DatabaseName, err)
	}
	defer providerConn.Close(ctx)

	// Send sync event from provider
	lsn, err := postgres.SyncEvent().Row(ctx, providerConn)
	if err != nil {
		return fmt.Errorf("failed to send sync event %q from provider: %w", lsn, err)
	}

	fmt.Println("sent sync event from provider", r.Identifier(), lsn)

	r.SyncEventLsn = lsn

	return nil
}

func (r *SyncEventResource) Create(ctx context.Context, rc *resource.Context) error {
	// Confirm sync is a no-op for create, just call Refresh
	return r.Refresh(ctx, rc)
}

func (r *SyncEventResource) Update(ctx context.Context, rc *resource.Context) error {
	// No-op for update
	return nil
}

func (r *SyncEventResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op for delete
	return nil
}
