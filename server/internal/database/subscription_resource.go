package database

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SubscriptionResource)(nil)

const ResourceTypeSubscription resource.Type = "database.subscription"

func SubscriptionResourceIdentifier(providerNode, subscriberNode string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeSubscription,
		ID:   providerNode + subscriberNode,
	}
}

type SubscriptionResource struct {
	SubscriberNode    string                `json:"subscriber_node"`
	ProviderNode      string                `json:"provider_node"`
	Disabled          bool                  `json:"disabled"`
	SyncStructure     bool                  `json:"sync_structure"`
	SyncData          bool                  `json:"sync_data"`
	ExtraDependencies []resource.Identifier `json:"dependent_subscriptions"`
	NeedsUpdate       bool                  `json:"needs_update"`
}

func (s *SubscriptionResource) ResourceVersion() string {
	return "1"
}

func (s *SubscriptionResource) DiffIgnore() []string {
	return nil
}

func (s *SubscriptionResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   s.SubscriberNode,
	}
}

func (s *SubscriptionResource) Identifier() resource.Identifier {
	return SubscriptionResourceIdentifier(s.ProviderNode, s.SubscriberNode)
}

func (s *SubscriptionResource) AddDependentResource(dep resource.Identifier) {
	s.ExtraDependencies = append(s.ExtraDependencies, dep)
}

func (s *SubscriptionResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		NodeResourceIdentifier(s.SubscriberNode),
		NodeResourceIdentifier(s.ProviderNode),
	}
	deps = append(deps, s.ExtraDependencies...)
	return deps
}

func (s *SubscriptionResource) Refresh(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providerDSN, err := s.providerDSN(ctx, rc)
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
		Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription needs to be created: %w", err)
	}
	if needsCreate {
		return resource.ErrNotFound
	}

	dsnNeedsUpdate, err := postgres.
		SubscriptionDsnNeedsUpdate(s.ProviderNode, s.SubscriberNode, providerDSN).
		Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription needs to be updated: %w", err)
	}

	needsEnable, err := postgres.
		SubscriptionNeedsEnable(s.ProviderNode, s.SubscriberNode, s.Disabled).
		Row(ctx, conn)
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
	providerDSN, err := s.providerDSN(ctx, rc)
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

func (s *SubscriptionResource) providerDSN(ctx context.Context, rc *resource.Context) (*postgres.DSN, error) {
	providers, err := GetAllInstances(ctx, rc, s.ProviderNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider instances: %w", err)
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: no provider instance found for node %s", resource.ErrNotFound, s.ProviderNode)
	}
	// Sorting instances so that our final DSN is deterministic
	slices.SortStableFunc(providers, func(a, b *InstanceResource) int {
		return strings.Compare(a.ConnectionInfo.PeerHost, b.ConnectionInfo.PeerHost)
	})
	hosts := make([]string, len(providers))
	ports := make([]int, len(providers))
	for i, provider := range providers {
		hosts[i] = provider.ConnectionInfo.PeerHost
		ports[i] = provider.ConnectionInfo.PeerPort
	}

	return &postgres.DSN{
		Hosts:       hosts,
		Ports:       ports,
		DBName:      providers[0].Spec.DatabaseName,
		User:        "pgedge",
		SSLCert:     providers[0].ConnectionInfo.PeerSSLCert,
		SSLKey:      providers[0].ConnectionInfo.PeerSSLKey,
		SSLRootCert: providers[0].ConnectionInfo.PeerSSLRootCert,
		Extra: map[string]string{
			"target_session_attrs": "primary",
		},
	}, nil
}
func (s *SubscriptionResource) Update(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providerDSN, err := s.providerDSN(ctx, rc)
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
	if node.PrimaryInstanceID == "" {
		return nil, resource.ErrNotFound
	}
	instance, err := resource.FromContext[*InstanceResource](rc, InstanceResourceIdentifier(node.PrimaryInstanceID))
	if err != nil {
		return nil, fmt.Errorf("failed to get primary instance %q: %w", node.PrimaryInstanceID, err)
	}
	return instance, nil
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
