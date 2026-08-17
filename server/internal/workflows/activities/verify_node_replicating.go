package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// VerifyNodeReplicatingInput identifies one node whose primary instance's
// subscriptions must all reach "replicating" before the MajorVersionUpgrade
// workflow moves on to the next node — the fail-loud check for a per-node
// major-version bump, mirroring VerifySubscriptionReplicatingResource's role
// in the add-node pipeline (see design item 9a): rather than assume Spock's
// own extension-upgrade-on-mismatch mechanism (ALTER EXTENSION UPDATE,
// triggered automatically once the new binary starts) brought replication
// back cleanly, this actually checks.
type VerifyNodeReplicatingInput struct {
	DatabaseID string `json:"database_id"`
	NodeName   string `json:"node_name"`
}

type VerifyNodeReplicatingOutput struct{}

func (a *Activities) ExecuteVerifyNodeReplicating(
	ctx workflow.Context,
	input *VerifyNodeReplicatingInput,
) workflow.Future[*VerifyNodeReplicatingOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.AnyQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*VerifyNodeReplicatingOutput](ctx, options, a.VerifyNodeReplicating, input)
}

const (
	verifyNodeReplicatingPollInterval = 5 * time.Second
	verifyNodeReplicatingTimeout      = 3 * time.Minute
)

// VerifyNodeReplicating polls the already-collected instance monitor status
// (InstanceStatus.Subscriptions, refreshed independently every
// database.InstanceMonitorRefreshInterval) rather than opening its own
// Postgres connection, since that status already reflects
// spock.sub_show_status() for every subscription on the instance and doing
// it this way means this activity isn't pinned to any specific host — it
// only ever reads from etcd.
func (a *Activities) VerifyNodeReplicating(ctx context.Context, input *VerifyNodeReplicatingInput) (*VerifyNodeReplicatingOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "node_name", input.NodeName)
	logger.Info("verifying node resumed replicating after major-version upgrade")

	deadline := time.Now().Add(verifyNodeReplicatingTimeout)
	var lastReason string

	for {
		reason, err := checkNodeReplicating(ctx, a.DatabaseService, input.DatabaseID, input.NodeName)
		if err != nil {
			return nil, err
		}
		if reason == "" {
			logger.Info("node is replicating")
			return &VerifyNodeReplicatingOutput{}, nil
		}
		lastReason = reason

		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"node %q did not resume replicating within %s after its major-version upgrade: %s",
				input.NodeName, verifyNodeReplicatingTimeout, lastReason,
			)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(verifyNodeReplicatingPollInterval):
		}
	}
}

// checkNodeReplicating returns "" if the node's primary instance reports
// every subscription as replicating, per the most recently collected (and
// not stale) instance status. Otherwise it returns a human-readable reason
// the check hasn't passed yet, to fail with a specific error if the overall
// timeout is reached instead of a bare "timed out."
func checkNodeReplicating(ctx context.Context, dbSvc *database.Service, databaseID, nodeName string) (string, error) {
	instances, err := dbSvc.GetInstances(ctx, databaseID)
	if err != nil {
		return "", fmt.Errorf("failed to get instances: %w", err)
	}

	var primary *database.Instance
	for _, instance := range instances {
		if instance.NodeName != nodeName {
			continue
		}
		if instance.Status != nil && instance.Status.IsPrimary() {
			primary = instance
			break
		}
	}
	if primary == nil {
		return fmt.Sprintf("no primary instance found for node %q yet", nodeName), nil
	}
	if primary.Status == nil || primary.Status.IsStale() {
		return fmt.Sprintf("status for node %q's primary instance %q is missing or stale", nodeName, primary.InstanceID), nil
	}
	if len(primary.Status.Subscriptions) == 0 {
		// A single-node database has no peer subscriptions at all — nothing
		// to wait on.
		return "", nil
	}
	for _, sub := range primary.Status.Subscriptions {
		if sub.Status != postgres.SubStatusReplicating {
			return fmt.Sprintf("subscription %q (from %q) has status %q, not %q",
				sub.Name, sub.ProviderNode, sub.Status, postgres.SubStatusReplicating), nil
		}
	}
	return "", nil
}
