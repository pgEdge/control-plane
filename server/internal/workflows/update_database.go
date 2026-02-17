package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type UpdateDatabaseInput struct {
	TaskID      uuid.UUID      `json:"task_id"`
	Spec        *database.Spec `json:"spec"`
	ForceUpdate bool           `json:"force_update"`
	RemoveHosts []string       `json:"remove_hosts"`
}

type UpdateDatabaseOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) UpdateDatabase(ctx workflow.Context, input *UpdateDatabaseInput) (*UpdateDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			updateStateInput := &activities.UpdateDbStateInput{
				DatabaseID: input.Spec.DatabaseID,
				State:      database.DatabaseStateFailed,
			}

			_, err := w.Activities.ExecuteUpdateDbState(cleanupCtx, updateStateInput).Get(cleanupCtx)
			if err != nil {
				logger.With("error", err).Error("failed to update database state ")
			}

			w.cancelTask(cleanupCtx, task.ScopeDatabase, input.Spec.DatabaseID, input.TaskID, logger)

		}
	}()

	logger.Info("updating database")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to update database")

		updateStateInput := &activities.UpdateDbStateInput{
			DatabaseID: input.Spec.DatabaseID,
			State:      database.DatabaseStateFailed,
		}
		_, stateErr := w.Activities.
			ExecuteUpdateDbState(ctx, updateStateInput).
			Get(ctx)
		if stateErr != nil {
			logger.With("error", stateErr).Error("failed to update database state")
		}

		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      input.Spec.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	refreshCurrentInput := &RefreshCurrentStateInput{
		DatabaseID:  input.Spec.DatabaseID,
		TaskID:      input.TaskID,
		RemoveHosts: input.RemoveHosts,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	planInput := &PlanUpdateInput{
		Spec:    input.Spec,
		Current: current,
		Options: operations.UpdateDatabaseOptions{
			PlanOptions: resource.PlanOptions{
				ForceUpdate: input.ForceUpdate,
			},
		},
	}
	planOutput, err := w.ExecutePlanUpdate(ctx, planInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to execute plan update: %w", err))
	}

	err = w.persistPlans(ctx, input.Spec.DatabaseID, input.TaskID, planOutput.Plans)
	if err != nil {
		return nil, handleError(err)
	}

	err = w.applyPlans(ctx, input.Spec.DatabaseID, input.TaskID, current, planOutput.Plans, input.RemoveHosts...)
	if err != nil {
		return nil, handleError(err)
	}

	// Provision services after database resources are applied
	logger.With("service_count_in_spec", len(input.Spec.Services)).Info("checking if we need to provision services")
	if len(input.Spec.Services) > 0 {
		provisionServicesInput := &ProvisionServicesInput{
			TaskID: input.TaskID,
			Spec:   input.Spec,
		}

		logger.With("service_count", len(input.Spec.Services)).Info("calling ProvisionServices workflow")

		_, err = w.ExecuteProvisionServices(ctx, provisionServicesInput).Get(ctx)
		if err != nil {
			// Log service provisioning failure but allow database to succeed
			// Service instances will be marked as "failed" with error details
			logger.With("error", err).Error("failed to provision services - database will be available but services degraded")

			err = w.logTaskEvent(ctx,
				task.ScopeDatabase,
				input.Spec.DatabaseID,
				input.TaskID,
				task.LogEntry{
					Message: "service provisioning failed - database available but services unavailable",
					Fields: map[string]any{
						"error": err.Error(),
					},
				},
			)
			if err != nil {
				logger.With("error", err).Warn("failed to log service provisioning error")
			}
		}
	}

	updateStateInput := &activities.UpdateDbStateInput{
		DatabaseID: input.Spec.DatabaseID,
		State:      database.DatabaseStateAvailable,
	}
	_, err = w.Activities.
		ExecuteUpdateDbState(ctx, updateStateInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to update database state to available: %w", err))
	}

	updateTaskInput = &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}
	logger.Info("successfully updated database")

	return &UpdateDatabaseOutput{
		Updated: current,
	}, nil
}
