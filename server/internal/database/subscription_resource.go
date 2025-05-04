package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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
	SubscriberNode string `json:"subscriber_node"`
	ProviderNode   string `json:"provider_node"`
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
	subscriber, err := getPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}
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
	subscriber, err := getPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	provider, err := getPrimaryInstance(ctx, rc, s.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instance: %w", err)
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}

	err = postgres.CreateSubscription(s.SubscriberNode, s.ProviderNode, &postgres.DSN{
		Host:        provider.ConnectionInfo.PeerHost,
		Port:        provider.ConnectionInfo.PeerPort,
		DBName:      provider.Spec.DatabaseName,
		User:        "pgedge",
		SSLCert:     provider.ConnectionInfo.PeerSSLCert,
		SSLKey:      provider.ConnectionInfo.PeerSSLKey,
		SSLRootCert: provider.ConnectionInfo.PeerSSLRootCert,
	}).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to create subscription %q: %w", s.SubscriberNode, err)
	}

	_, err = postgres.GetSubscriptionID(s.SubscriberNode, s.ProviderNode).Row(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get subscription ID %q: %w", s.SubscriberNode, err)
	}

	return nil
}

func (s *SubscriptionResource) Update(ctx context.Context, rc *resource.Context) error {
	// Note that this won't update the interface if the subscription already
	// exists.
	return s.Create(ctx, rc)
}

func (s *SubscriptionResource) Delete(ctx context.Context, rc *resource.Context) error {
	subscriber, err := getPrimaryInstance(ctx, rc, s.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance: %w", err)
	}
	conn, err := subscriber.Connection(ctx, rc, subscriber.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", subscriber.Spec.DatabaseName, err)
	}
	err = postgres.DropSubscription(s.SubscriberNode, s.ProviderNode).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to drop subscription %q: %w", s.SubscriberNode, err)
	}

	return nil
}

func getPrimaryInstance(ctx context.Context, rc *resource.Context, nodeName string) (*InstanceResource, error) {
	node, err := resource.FromContext[*NodeResource](rc, NodeResourceIdentifier(nodeName))
	if err != nil {
		return nil, fmt.Errorf("failed to get node %q: %w", nodeName, err)
	}
	if node.PrimaryInstanceID == uuid.Nil {
		return nil, resource.ErrNotFound
	}
	instance, err := resource.FromContext[*InstanceResource](rc, InstanceResourceIdentifier(node.PrimaryInstanceID))
	if err != nil {
		return nil, fmt.Errorf("failed to get primary instance %q: %w", node.PrimaryInstanceID.String(), err)
	}
	return instance, nil
}
