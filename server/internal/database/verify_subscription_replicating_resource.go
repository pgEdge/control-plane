package database

import (
	"context"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*VerifySubscriptionReplicatingResource)(nil)

const ResourceTypeVerifySubscriptionReplicating resource.Type = "database.verify_subscription_replicating"

func VerifySubscriptionReplicatingResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeVerifySubscriptionReplicating,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

// VerifySubscriptionReplicatingResource polls a subscription's status until
// it reaches "replicating", failing loudly if it doesn't within a bounded
// wait. Mirrors spock's own zodan.sql reference add-node flow
// (spock.verify_subscription_replicating, samples/Z0DAN/zodan.sql), which
// Control Plane's pipeline otherwise has no equivalent of: nothing
// previously checked that an enabled subscription's apply worker actually
// started. That gap made a real, unresolved Spock issue invisible — some
// enabled peer subscriptions never get an apply worker at all, with no
// error anywhere (see
// demo/PLAT-650/spock6_peer_subscription_worker_not_starting_report.md,
// including confirmed live data loss: a table created on a peer never
// reached the new node at all). This resource does not fix that
// underlying issue — it can't, the bug is inside Spock's own worker
// manager — but it turns silent, permanent data loss into a visible,
// actionable task failure instead.
type VerifySubscriptionReplicatingResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`
	SubscriberNode string `json:"subscriber_node"`
}

func (r *VerifySubscriptionReplicatingResource) ResourceVersion() string { return "1" }
func (r *VerifySubscriptionReplicatingResource) DiffIgnore() []string    { return nil }

// Subscription status is local to the subscriber (spock.sub_show_status()
// reports on incoming subscriptions), so this must run on that host.
func (r *VerifySubscriptionReplicatingResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.SubscriberNode)
}

func (r *VerifySubscriptionReplicatingResource) Identifier() resource.Identifier {
	return VerifySubscriptionReplicatingResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName)
}

func (r *VerifySubscriptionReplicatingResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		SubscriptionResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	}
}

func (r *VerifySubscriptionReplicatingResource) TypeDependencies() []resource.Type { return nil }

func (r *VerifySubscriptionReplicatingResource) Refresh(ctx context.Context, rc *resource.Context) error {
	subscriber, err := GetPrimaryInstance(ctx, rc, r.SubscriberNode)
	if err != nil {
		return fmt.Errorf("failed to get subscriber instance for node %q: %w", r.SubscriberNode, err)
	}
	conn, err := subscriber.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to subscriber %q: %w", r.SubscriberNode, err)
	}
	defer conn.Close(ctx)

	// Matches spock's own verify_subscription_replicating default wait
	// (120s) with headroom, since ours is the second such check in the
	// pipeline (after whatever WaitForSyncEventResource already waited
	// for) rather than the only one.
	const (
		pollInterval = 2 * time.Second
		waitTimeout  = 3 * time.Minute
	)
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	var lastStatus string
	for {
		status, err := postgres.GetSubscriptionStatus(r.ProviderNode, r.SubscriberNode).Scalar(waitCtx, conn)
		if err != nil {
			if postgres.IsSpockNodeNotConfigured(err) {
				return resource.ErrNotFound
			}
			return fmt.Errorf("failed to check subscription status: %w", err)
		}
		if status == postgres.SubStatusReplicating {
			return nil
		}
		lastStatus = status

		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"subscription %s->%s did not reach %q status within %s (last status: %q) — "+
					"this matches a known, unresolved Spock issue where the apply worker never "+
					"starts for some enabled subscriptions; see "+
					"demo/PLAT-650/spock6_peer_subscription_worker_not_starting_report.md",
				r.ProviderNode, r.SubscriberNode, postgres.SubStatusReplicating, waitTimeout, lastStatus)
		case <-time.After(pollInterval):
		}
	}
}

func (r *VerifySubscriptionReplicatingResource) Create(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}

func (r *VerifySubscriptionReplicatingResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Refresh(ctx, rc)
}

func (r *VerifySubscriptionReplicatingResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
