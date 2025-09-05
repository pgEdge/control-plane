package database

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*ReplicationSlotCreateResource)(nil)

const ResourceTypeReplicationSlotCreate resource.Type = "database.replication_slot_create"

func ReplicationSlotCreateResourceIdentifier(databaseName, providerNode, subscriberNode string) resource.Identifier {
	subName := fmt.Sprintf("sub_%s_%s", providerNode, subscriberNode)
	slotName := fmt.Sprintf("spk_%s_%s_%s", databaseName, providerNode, subName)
	return resource.Identifier{
		ID:   slotName,
		Type: ResourceTypeReplicationSlotCreate,
	}
}

type ReplicationSlotCreateResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`

	DependentResources []resource.Identifier `json:"dependent_resources,omitempty"`
}

func NewReplicationSlotCreateResource(databaseName, providerNode, subscriberNode string) *ReplicationSlotCreateResource {
	return &ReplicationSlotCreateResource{
		DatabaseName:   databaseName,
		ProviderNode:   providerNode,
		SubscriberNode: subscriberNode,
	}
}

func (r *ReplicationSlotCreateResource) ResourceVersion() string {
	return "1"
}

func (r *ReplicationSlotCreateResource) DiffIgnore() []string {
	return nil
}

func (r *ReplicationSlotCreateResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   r.ProviderNode,
	}
}

func (r *ReplicationSlotCreateResource) Identifier() resource.Identifier {
	return ReplicationSlotCreateResourceIdentifier(r.DatabaseName, r.ProviderNode, r.SubscriberNode)
}

func (r *ReplicationSlotCreateResource) AddDependentResource(dep resource.Identifier) {
	r.DependentResources = append(r.DependentResources, dep)
}

func (r *ReplicationSlotCreateResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),
	}
	deps = append(deps, r.DependentResources...)
	return deps
}

func (r *ReplicationSlotCreateResource) Refresh(ctx context.Context, rc *resource.Context) error {
	instance, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get primary instance for provider node: %w", err)
	}

	conn, err := instance.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	stmt := postgres.CreateReplicationSlot(r.DatabaseName, r.ProviderNode, r.SubscriberNode)
	if err := stmt.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create replication slot: %w", err)
	}

	return nil
}

func (r *ReplicationSlotCreateResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}

func (r *ReplicationSlotCreateResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotCreateResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
