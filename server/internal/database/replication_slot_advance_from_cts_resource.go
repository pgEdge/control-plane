package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// Ensure interface conformance
var _ resource.Resource = (*ReplicationSlotAdvanceFromCTSResource)(nil)

const ResourceTypeReplicationSlotAdvanceFromCTS resource.Type = "database.replication_slot_advance_from_cts"

// ReplicationSlotAdvanceFromCTSResourceIdentifier creates a stable identifier for this resource.
func ReplicationSlotAdvanceFromCTSResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeReplicationSlotAdvanceFromCTS,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

// ReplicationSlotAdvanceFromCTSResource advances the replication slot on the provider
// to the LSN derived from the commit timestamp captured in lag_tracker.
// AdvancedToLSN is written as output after a successful advance so that
// ReplicationOriginAdvanceResource (running on the subscriber) can read it.
type ReplicationSlotAdvanceFromCTSResource struct {
	DatabaseName   string `json:"database_name"`
	ProviderNode   string `json:"provider_node"`   // slot lives here
	SubscriberNode string `json:"subscriber_node"` // target/receiver node

	// Output: LSN the slot was advanced to (empty if advance was skipped).
	AdvancedToLSN string `json:"advanced_to_lsn,omitempty"`
}

func (r *ReplicationSlotAdvanceFromCTSResource) ResourceVersion() string { return "1" }

func (r *ReplicationSlotAdvanceFromCTSResource) DiffIgnore() []string {
	return []string{"advanced_to_lsn"}
}

// Execute on the provider node (the slot exists there).
func (r *ReplicationSlotAdvanceFromCTSResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.ProviderNode)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Identifier() resource.Identifier {
	return ReplicationSlotAdvanceFromCTSResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		PostgresDatabaseResourceIdentifier(r.ProviderNode, r.DatabaseName),
		PostgresDatabaseResourceIdentifier(r.SubscriberNode, r.DatabaseName),
		LagTrackerCommitTSIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	}
}

func (r *ReplicationSlotAdvanceFromCTSResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Create(ctx context.Context, rc *resource.Context) error {
	r.AdvancedToLSN = ""

	// LagTrackerCommitTimestampResource runs on the subscriber's host (it's
	// the receiver), so it's the one place we can read the subscriber's own
	// spock.progress without an illegal cross-host connection — this
	// resource runs on the provider's host and can only ever connect to
	// the provider. Its CommitTimestamp value is no longer used; ProgressLSN
	// is what we need.
	lagTracker, err := resource.FromContext[*LagTrackerCommitTimestampResource](
		rc,
		LagTrackerCommitTSIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	)
	if err != nil {
		return fmt.Errorf("failed to get lag tracker resource for %q->%q: %w", r.ProviderNode, r.SubscriberNode, err)
	}

	// Connect to provider (slot lives here)
	provider, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instance for node %q: %w", r.ProviderNode, err)
	}
	conn, err := provider.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to provider %q: %w", r.ProviderNode, err)
	}
	defer conn.Close(ctx)

	// If the subscription is already enabled and its slot is active, it's
	// genuinely, permanently replicating on its own — nothing to advance,
	// same as the original intent here.
	enabled, err := postgres.IsSubscriptionEnabled(r.ProviderNode, r.SubscriberNode).Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if subscription is enabled: %w", err)
	}
	if enabled {
		isActive, err := postgres.
			IsReplicationSlotActive(r.DatabaseName, r.ProviderNode, r.SubscriberNode).
			Scalar(ctx, conn)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to check if replication slot is active: %w", err)
		}
		if err == nil && isActive {
			return nil
		}
	}

	// The subscription is still disabled (the normal case for this resource,
	// which only runs as part of add-node peer catchup). Its slot shouldn't
	// have anything consuming it yet — but spock's internal sub_create setup
	// (the C-side pause/snapshot dance around ensure_replication_slot_snapshot
	// in spock_sync.c, present in both 5.x and 6.x) can hold it transiently
	// active around creation time. A single "active"
	// snapshot here would previously be treated as permanent ("already
	// replicating, nothing to do"), which is wrong for a disabled
	// subscription — it's a race: if this runs during that transient window,
	// AdvancedToLSN never gets set, and nothing ever re-checks. Poll until
	// the slot goes idle instead, the same pattern PeerCatchupResource
	// already uses for its own wait.
	const (
		activePollInterval = 500 * time.Millisecond
		activeWaitTimeout  = 2 * time.Minute
	)
	waitCtx, cancel := context.WithTimeout(ctx, activeWaitTimeout)
	defer cancel()
	for {
		isActive, err := postgres.
			IsReplicationSlotActive(
				r.DatabaseName,
				r.ProviderNode,
				r.SubscriberNode).
			Scalar(waitCtx, conn)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to check if replication slot is active: %w", err)
		}
		if err == nil && !isActive {
			break
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for replication slot %q->%q to go idle before advancing it",
				r.ProviderNode, r.SubscriberNode)
		case <-time.After(activePollInterval):
		}
	}

	currentLSN, err := postgres.
		CurrentReplicationSlotLSN(
			r.DatabaseName,
			r.ProviderNode,
			r.SubscriberNode).
		Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query current replication slot lsn: %w", err)
	}

	// The resume LSN comes from the subscriber's own spock.progress (read by
	// LagTrackerCommitTimestampResource above, on the subscriber's host) —
	// the same source Spock's own v6 zodan.sql uses, instead of computing
	// one via spock.get_lsn_from_commit_ts() on the provider (which has
	// been observed to hang indefinitely on an idle node — see
	// demo/PLAT-650/spock6_get_lsn_from_commit_ts_report.md). The
	// subscriber's src->new subscription already calls spock.sub_create(),
	// which internally seeds spock.progress for other peers via
	// spock.read_peer_progress() as part of its own snapshot dance — this
	// reads that seeded value rather than recomputing it.
	//
	// NOT YET LIVE-VERIFIED: we haven't confirmed, against a running
	// cluster, that this row is actually present by the time this resource
	// runs in Control Plane's own dependency ordering (zodan.sql's phase
	// numbering doesn't map 1:1 onto our resource graph). If it's ever nil
	// in practice, this resource just skips advancing this round (same as
	// the old "no commit timestamp yet" behavior) rather than falling back
	// to get_lsn_from_commit_ts, since falling back would silently
	// reintroduce the hang this change exists to avoid.
	if lagTracker.ProgressLSN == nil {
		// No seeded progress entry yet for this peer — nothing to advance
		// to this round.
		return nil
	}
	targetLSN := *lagTracker.ProgressLSN

	atOrBefore, err := postgres.LsnAtOrBefore(targetLSN, currentLSN).Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to compare LSNs: %w", err)
	}
	if atOrBefore {
		return nil
	}

	err = postgres.
		AdvanceReplicationSlotToLSN(
			r.DatabaseName,
			r.ProviderNode,
			r.SubscriberNode,
			targetLSN).
		Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to advance replication slot: %w", err)
	}

	// Record the LSN so ReplicationOriginAdvanceResource (running on the
	// subscriber's host) can advance the origin to the same position.
	r.AdvancedToLSN = targetLSN
	return nil
}

func (r *ReplicationSlotAdvanceFromCTSResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *ReplicationSlotAdvanceFromCTSResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op; advancing a slot does not create durable config to remove.
	return nil
}
