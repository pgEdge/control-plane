package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type CleanupOrphanedSlotsInput struct {
	State          *resource.State `json:"state"`
	DatabaseName   string          `json:"database_name"`
	ProviderNode   string          `json:"provider_node"`
	SubscriberNode string          `json:"subscriber_node"`
}

type CleanupOrphanedSlotsOutput struct{}

// ExecuteCleanupOrphanedSlots queues the CleanupOrphanedSlots activity on the manager.
func (a *Activities) ExecuteCleanupOrphanedSlots(
	ctx workflow.Context,
	input *CleanupOrphanedSlotsInput,
) workflow.Future[*CleanupOrphanedSlotsOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 3,
		},
	}
	return workflow.ExecuteActivity[*CleanupOrphanedSlotsOutput](ctx, options, a.CleanupOrphanedSlots, input)
}

// CleanupOrphanedSlots drops a replication slot on a surviving provider node
// that was serving a subscription to a removed subscriber node. This prevents
// WAL accumulation from orphaned slots after host removal.
func (a *Activities) CleanupOrphanedSlots(ctx context.Context, input *CleanupOrphanedSlotsInput) (*CleanupOrphanedSlotsOutput, error) {
	logger := activity.Logger(ctx).With(
		"provider_node", input.ProviderNode,
		"subscriber_node", input.SubscriberNode,
		"database_name", input.DatabaseName,
	)
	logger.Info("cleaning up orphaned replication slot")

	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return nil, err
	}

	rc := &resource.Context{
		State:    input.State,
		Injector: a.Injector,
		Registry: registry,
	}

	provider, err := database.GetPrimaryInstance(ctx, rc, input.ProviderNode)
	if err != nil {
		// Provider instance doesn't exist — slot is already gone
		logger.Info("provider instance not found, skipping slot cleanup")
		return &CleanupOrphanedSlotsOutput{}, nil
	}

	conn, err := provider.Connection(ctx, rc, input.DatabaseName)
	if err != nil {
		// Can't connect to provider — best effort
		logger.Warn("cannot connect to provider, skipping slot cleanup", "error", err)
		return &CleanupOrphanedSlotsOutput{}, nil
	}
	defer conn.Close(ctx)

	stmt := postgres.DropReplicationSlot(input.DatabaseName, input.ProviderNode, input.SubscriberNode)
	if err := stmt.Exec(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to drop orphaned replication slot: %w", err)
	}

	logger.Info("successfully cleaned up orphaned replication slot")
	return &CleanupOrphanedSlotsOutput{}, nil
}
