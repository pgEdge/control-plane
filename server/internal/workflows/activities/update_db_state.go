package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type UpdateDbStateInput struct {
	DatabaseID string                 `json:"database_id"`
	State      database.DatabaseState `json:"state"`
}

type UpdateDbStateOutput struct{}

func (a *Activities) ExecuteUpdateDbState(
	ctx workflow.Context,
	input *UpdateDbStateInput,
) workflow.Future[*UpdateDbStateOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateDbStateOutput](ctx, options, a.UpdateDbState, input)
}

func (a *Activities) UpdateDbState(ctx context.Context, input *UpdateDbStateInput) (*UpdateDbStateOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Debug("updating database state")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = dbSvc.UpdateDatabaseState(ctx, input.DatabaseID, "", input.State)
	if err != nil {
		return nil, fmt.Errorf("failed to update database state: %w", err)
	}

	switch input.State {
	case database.DatabaseStateFailed:
		err := a.handleDatabaseFailed(ctx, input.DatabaseID)
		if err != nil {
			return nil, err
		}
	case database.DatabaseStateAvailable:
		err := a.handleDatabaseSucceeded(ctx, input.DatabaseID)
		if err != nil {
			return nil, err
		}
	}

	return &UpdateDbStateOutput{}, nil
}

func (a *Activities) handleDatabaseFailed(ctx context.Context, databaseID string) error {
	// Mark all in-progress instances as failed
	instances, err := a.DatabaseService.GetInstances(ctx, databaseID)
	if err != nil {
		return fmt.Errorf("failed to get database instances: %w", err)
	}
	now := time.Now()
	for _, instance := range instances {
		if instance.State.IsInProgress() {
			err := a.DatabaseService.UpdateInstance(ctx, &database.InstanceUpdateOptions{
				InstanceID: instance.InstanceID,
				DatabaseID: instance.DatabaseID,
				HostID:     instance.HostID,
				NodeName:   instance.NodeName,
				State:      database.InstanceStateFailed,
				Now:        now,
			})
			if err != nil {
				return fmt.Errorf("failed to update instance '%s': %w", instance.InstanceID, err)
			}
		}
	}

	return nil
}

func (a *Activities) handleDatabaseSucceeded(ctx context.Context, databaseID string) error {
	// Delete any instances that are no longer part of the database
	db, err := a.DatabaseService.GetDatabase(ctx, databaseID)
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	// Populate CloneOrigin if this database was created from a clone
	if err := a.populateCloneOrigin(ctx, db); err != nil {
		return fmt.Errorf("failed to populate clone origin: %w", err)
	}

	nodes, err := db.Spec.NodeInstances()
	if err != nil {
		return fmt.Errorf("failed to compute instances: %w", err)
	}
	instanceIDs := ds.NewSet[string]()
	for _, node := range nodes {
		for _, instance := range node.Instances {
			instanceIDs.Add(instance.InstanceID)
		}
	}
	for _, instance := range db.Instances {
		if !instanceIDs.Has(instance.InstanceID) {
			err := a.DatabaseService.DeleteInstance(ctx, instance.DatabaseID, instance.InstanceID)
			if err != nil {
				return fmt.Errorf("failed to delete instance record for '%s': %w", instance.InstanceID, err)
			}
		}
	}

	return nil
}

func (a *Activities) populateCloneOrigin(ctx context.Context, db *database.Database) error {
	if db.CloneOrigin != nil {
		return nil // already populated
	}
	// Find a node with CloneConfig
	var cloneConfig *database.CloneConfig
	for _, node := range db.Spec.Nodes {
		if node.CloneConfig != nil {
			cloneConfig = node.CloneConfig
			break
		}
	}
	if cloneConfig == nil {
		return nil // not a clone
	}

	// Look up source database name
	var sourceName string
	sourceDB, err := a.DatabaseService.GetDatabase(ctx, cloneConfig.SourceDatabaseID)
	if err == nil {
		sourceName = sourceDB.Spec.DatabaseName
	}
	// If source is deleted, we still record the origin with an empty name

	return a.DatabaseService.SetCloneOrigin(ctx, db.DatabaseID, &database.CloneOrigin{
		SourceDatabaseID:   cloneConfig.SourceDatabaseID,
		SourceDatabaseName: sourceName,
		SourceNodeName:     cloneConfig.SourceNodeName,
		ClonedAt:           time.Now(),
	})
}
