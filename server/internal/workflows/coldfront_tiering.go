package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

// ColdFrontTieringInput carries all arguments needed to run one tiering binary
// (archiver, partitioner, or compactor) against a database node.
type ColdFrontTieringInput struct {
	DatabaseID    string          `json:"database_id"`
	NodeName      string          `json:"node_name"`
	ServiceID     string          `json:"service_id"`
	ServiceConfig map[string]any  `json:"service_config"`
	DatabaseName  string          `json:"database_name"`
	Binary        string          `json:"binary"` // "archiver" | "partitioner" | "compactor"
	Instances     []*InstanceHost `json:"instances"`
	TaskID        uuid.UUID       `json:"task_id"`
}

type ColdFrontTieringOutput struct{}

// ColdFrontTiering resolves the current primary node for the database, renders
// the binary's config, and docker-execs the single-pass tiering binary inside
// the primary's Postgres container. Exit codes are captured: "no tables
// configured" archiver exits are recorded as benign (not a failure).
func (w *Workflows) ColdFrontTiering(ctx workflow.Context, input *ColdFrontTieringInput) (*ColdFrontTieringOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"binary", input.Binary,
		"task_id", input.TaskID.String(),
	)

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)
			w.cancelTask(cleanupCtx, task.ScopeDatabase, input.DatabaseID, input.TaskID, logger)
		}
	}()

	logger.Info("starting coldfront tiering run")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("coldfront tiering run failed")
		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)
		return cause
	}

	if len(input.Instances) == 0 {
		return nil, handleError(fmt.Errorf("no instances available for database %s node %s", input.DatabaseID, input.NodeName))
	}

	// Use the first instance as the seed for primary resolution.
	seed := input.Instances[0]

	updateOptions := task.UpdateStart()
	if err := w.updateTask(ctx, logger, &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: updateOptions,
	}); err != nil {
		return nil, handleError(err)
	}

	// Resolve the current primary. Done at run time so that it follows Patroni
	// failover automatically.
	getPrimaryOutput, err := w.Activities.
		ExecuteGetPrimaryInstance(ctx, seed.HostID, &activities.GetPrimaryInstanceInput{
			DatabaseID: input.DatabaseID,
			InstanceID: seed.InstanceID,
		}).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to resolve primary instance: %w", err))
	}

	// Match primary instance ID back to a host.
	var primaryInstance *InstanceHost
	for _, inst := range input.Instances {
		if inst.InstanceID == getPrimaryOutput.PrimaryInstanceID {
			primaryInstance = inst
			break
		}
	}
	if primaryInstance == nil {
		return nil, handleError(fmt.Errorf("primary instance %q not found in instance list", getPrimaryOutput.PrimaryInstanceID))
	}

	// Run the binary on the primary.
	_, err = w.Activities.
		ExecuteRunColdFrontBinary(ctx, primaryInstance.HostID, &activities.RunColdFrontBinaryInput{
			DatabaseID:    input.DatabaseID,
			NodeName:      input.NodeName,
			InstanceID:    primaryInstance.InstanceID,
			ServiceConfig: input.ServiceConfig,
			DatabaseName:  input.DatabaseName,
			Binary:        input.Binary,
		}).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("coldfront %s failed: %w", input.Binary, err))
	}

	if err := w.updateTask(ctx, logger, &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}); err != nil {
		return nil, handleError(err)
	}

	logger.Info("coldfront tiering run completed successfully")
	return &ColdFrontTieringOutput{}, nil
}
