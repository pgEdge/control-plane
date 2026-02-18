package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

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

const defaultSyncTimeout = 1 * time.Hour

// syncTimeout returns the effective sync timeout. Priority:
//  1. Per-resource Timeout field (if set)
//  2. PGEDGE_SYNC_TIMEOUT_SECONDS env var (if set)
//  3. defaultSyncTimeout (1 hour)
func syncTimeout(resourceTimeout time.Duration) time.Duration {
	if resourceTimeout > 0 {
		return resourceTimeout
	}
	if v := os.Getenv("PGEDGE_SYNC_TIMEOUT_SECONDS"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultSyncTimeout
}

type WaitForSyncEventResource struct {
	SubscriberNode string        `json:"subscriber_node"`
	ProviderNode   string        `json:"provider_node"`
	Timeout        time.Duration `json:"timeout,omitempty"`
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

	const pollInterval = 10 * time.Second

	timeout := syncTimeout(r.Timeout)

	deadline := time.Now().Add(timeout)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("replication sync timed out after %s: provider=%s subscriber=%s lsn=%s",
				timeout, r.ProviderNode, r.SubscriberNode, syncEvent.SyncEventLsn)
		}

		// Check subscription health first — fail early if broken.
		// Only statuses where the spock worker is running can make
		// progress. The others ("disabled", "down") mean sync will
		// never complete.
		status, err := postgres.GetSubscriptionStatus(r.ProviderNode, r.SubscriberNode).
			Scalar(ctx, subscriberConn)
		if err != nil {
			return fmt.Errorf("failed to check subscription status: %w", err)
		}
		switch status {
		case postgres.SubStatusInitializing, postgres.SubStatusReplicating, postgres.SubStatusUnknown:
			// Worker is running — continue waiting
		default:
			return fmt.Errorf("subscription has unhealthy status %q: provider=%s subscriber=%s",
				status, r.ProviderNode, r.SubscriberNode)
		}

		// Try short wait for sync event with poll interval as timeout
		synced, err := postgres.WaitForSyncEvent(
			r.ProviderNode, syncEvent.SyncEventLsn, int(pollInterval.Seconds()),
		).Scalar(ctx, subscriberConn)
		if errors.Is(err, pgx.ErrNoRows) {
			return resource.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to wait for sync event on subscriber: %w", err)
		}
		if synced {
			return nil
		}

		// Not yet synced, but subscription is healthy — continue waiting
	}
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
