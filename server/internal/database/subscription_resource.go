package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SubscriptionResource)(nil)

const ResourceTypeSubscription resource.Type = "database.subscription"

func SubscriptionResourceIdentifier(subscriberNode, providerNode string) resource.Identifier {
	return resource.Identifier{
		ID:   subscriberNode + providerNode,
		Type: ResourceTypeSubscription,
	}
}

type SubscriptionResource struct {
	SubscriberNode    string   `json:"subscriber_node"`
	ProviderNode      string   `json:"provider_node"`
	ProviderInstances []string `json:"provider_instances"`
	Zodan             bool     `json:"zodan,omitempty"`
	Spec              *Spec    `json:"spec,omitempty"`
}

func NewSubscriptionResource(subscriber *NodeInstances, provider *NodeInstances) *SubscriptionResource {
	return &SubscriptionResource{
		SubscriberNode:    subscriber.NodeName,
		ProviderNode:      provider.NodeName,
		ProviderInstances: provider.InstanceIDs(),
	}
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
	return SubscriptionResourceIdentifier(s.SubscriberNode, s.ProviderNode)
}

func (s *SubscriptionResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		NodeResourceIdentifier(s.SubscriberNode),
		NodeResourceIdentifier(s.ProviderNode),
	}
}

func (s *SubscriptionResource) Refresh(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	_, err = postgres.GetSubscriptionID(s.SubscriberNode, s.ProviderNode).Row(ctx, conn)
	if errors.Is(err, pgx.ErrNoRows) {
		// subscription does not exist
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get subscription ID %q: %w", s.SubscriberNode, err)
	}

	return nil
}

func (s *SubscriptionResource) Create(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	providers, err := GetAllInstances(ctx, rc, s.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instances: %w", err)
	}
	if len(providers) < 1 {
		return fmt.Errorf("no provider instance found for node %s", s.ProviderNode)
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

	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %s on node %s: %w", subscriber.Spec.DatabaseName, s.SubscriberNode, err)
	}
	defer conn.Close(ctx)

	err = postgres.CreateSubscription(s.SubscriberNode, s.ProviderNode, &postgres.DSN{
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
	}).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to create subscription on node %s: %w", s.SubscriberNode, err)
	}

	_, err = postgres.GetSubscriptionID(s.SubscriberNode, s.ProviderNode).Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get subscription ID on node %s: %w", s.SubscriberNode, err)
	}

	return nil
}

func (s *SubscriptionResource) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
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

	err = postgres.DropSubscription(s.SubscriberNode, s.ProviderNode).Exec(ctx, conn)
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
