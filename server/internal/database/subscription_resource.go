package database

import (
	"context"
	"fmt"

	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SubscriptionResource)(nil)

const ResourceTypeSubscription resource.Type = "database.subscription"

func SubscriptionResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeSubscription,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type SubscriptionResource struct {
	DatabaseName      string                `json:"database_name"`
	SubscriberNode    string                `json:"subscriber_node"`
	ProviderNode      string                `json:"provider_node"`
	Disabled          bool                  `json:"disabled"`
	SyncStructure     bool                  `json:"sync_structure"`
	SyncData          bool                  `json:"sync_data"`
	ExtraDependencies []resource.Identifier `json:"extra_dependencies"`
	NeedsUpdate       bool                  `json:"needs_update"`
}

func (s *SubscriptionResource) ResourceVersion() string {
	return "1"
}

func (s *SubscriptionResource) DiffIgnore() []string {
	return nil
}

func (s *SubscriptionResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(s.SubscriberNode)
}

func (s *SubscriptionResource) Identifier() resource.Identifier {
	return SubscriptionResourceIdentifier(s.ProviderNode, s.SubscriberNode, s.DatabaseName)
}

func (s *SubscriptionResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		PostgresDatabaseResourceIdentifier(s.SubscriberNode, s.DatabaseName),
		PostgresDatabaseResourceIdentifier(s.ProviderNode, s.DatabaseName),
		ReplicationSlotResourceIdentifier(s.ProviderNode, s.SubscriberNode, s.DatabaseName),
	}
	deps = append(deps, s.ExtraDependencies...)
	return deps
}

func (s *SubscriptionResource) TypeDependencies() []resource.Type {
	return nil
}

func (s *SubscriptionResource) Refresh(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providerDSN, err := s.providerDSN(ctx, rc, subscriber)
	if err != nil {
		return err
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	needsCreate, err := postgres.
		SubscriptionNeedsCreate(s.ProviderNode, s.SubscriberNode).
		Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription needs to be created: %w", err)
	}
	if needsCreate {
		return resource.ErrNotFound
	}

	dsnNeedsUpdate, err := postgres.
		SubscriptionDsnNeedsUpdate(s.ProviderNode, s.SubscriberNode, providerDSN).
		Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription needs to be updated: %w", err)
	}

	needsEnable, err := postgres.
		SubscriptionNeedsEnable(s.ProviderNode, s.SubscriberNode, s.Disabled).
		Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription needs to be enabled: %w", err)
	}

	s.NeedsUpdate = dsnNeedsUpdate || needsEnable

	return nil
}

func (s *SubscriptionResource) Create(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providerDSN, err := s.providerDSN(ctx, rc, subscriber)
	if err != nil {
		return err
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %s on node %s: %w", subscriber.Spec.DatabaseName, s.SubscriberNode, err)
	}
	defer conn.Close(ctx)

	err = postgres.
		CreateSubscription(
			s.ProviderNode,
			s.SubscriberNode,
			providerDSN,
			s.Disabled,
			s.SyncStructure,
			s.SyncData,
		).
		Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to create subscription on node %s: %w", s.SubscriberNode, err)
	}

	return nil
}

func (s *SubscriptionResource) providerDSN(ctx context.Context, rc *resource.Context, subscriber *InstanceResource) (*postgres.DSN, error) {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return nil, err
	}
	providerDSN, err := orch.NodeDSN(ctx, rc, s.ProviderNode, subscriber.Spec.InstanceID, s.DatabaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider dsn: %w", err)
	}

	return providerDSN, nil
}

func (s *SubscriptionResource) Update(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providerDSN, err := s.providerDSN(ctx, rc, subscriber)
	if err != nil {
		return err
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %s on node %s: %w", subscriber.Spec.DatabaseName, s.SubscriberNode, err)
	}
	defer conn.Close(ctx)

	err = postgres.
		CreateSubscription(
			s.ProviderNode,
			s.SubscriberNode,
			providerDSN,
			s.Disabled,
			s.SyncStructure,
			s.SyncData,
		).
		Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to update subscription on node %s: %w", s.SubscriberNode, err)
	}

	err = postgres.
		EnableSubscription(s.ProviderNode, s.SubscriberNode, s.Disabled).
		Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to enable subscription on node %s: %w", s.SubscriberNode, err)
	}

	return nil
}

func (s *SubscriptionResource) Delete(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}

	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	err = postgres.DropSubscription(s.ProviderNode, s.SubscriberNode).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to drop subscription %q: %w", s.SubscriberNode, err)
	}

	return nil
}

func GetPrimaryInstance(ctx context.Context, rc *resource.Context, nodeName string) (*InstanceResource, error) {
	node, err := resource.FromContext[*NodeResource](rc, NodeResourceIdentifier(nodeName))
	if err != nil {
		return nil, fmt.Errorf("failed to get node %q: %w", nodeName, err)
	}
	return node.PrimaryInstance(ctx, rc)
}

func GetAllInstances(ctx context.Context, rc *resource.Context, nodeName string) ([]*InstanceResource, error) {
	node, err := resource.FromContext[*NodeResource](rc, NodeResourceIdentifier(nodeName))
	if err != nil {
		return nil, fmt.Errorf("failed to get node %q: %w", nodeName, err)
	}
	instances := make([]*InstanceResource, len(node.InstanceIDs))
	for i, instanceID := range node.InstanceIDs {
		instance, err := resource.FromContext[*InstanceResource](rc, InstanceResourceIdentifier(instanceID))
		if err != nil {
			return nil, fmt.Errorf("failed to get instance %q: %w", instanceID, err)
		}
		instances[i] = instance
	}
	return instances, nil
}
