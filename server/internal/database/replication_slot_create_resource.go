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
	return resource.Identifier{
		ID:   postgres.ReplicationSlotName(databaseName, providerNode, subscriberNode),
		Type: ResourceTypeReplicationSlotCreate,
	}
}

type ReplicationSlotCreateResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
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

func (r *ReplicationSlotCreateResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		NodeResourceIdentifier(r.ProviderNode),
	}
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

	needsCreate, err := postgres.
		ReplicationSlotNeedsCreate(r.DatabaseName, r.ProviderNode, r.SubscriberNode).
		Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if replication slot exists: %w", err)
	}
	if needsCreate {
		return resource.ErrNotFound
	}

	return nil
}

func (r *ReplicationSlotCreateResource) Create(ctx context.Context, rc *resource.Context) error {
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

func (r *ReplicationSlotCreateResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *ReplicationSlotCreateResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
