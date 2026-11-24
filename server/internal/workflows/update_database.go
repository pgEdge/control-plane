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
	TaskID      uuid.UUID          `json:"task_id"`
	Spec        *database.Spec     `json:"spec"`
	ForceUpdate bool               `json:"force_update"`
	RemoveHosts []string           `json:"remove_hosts"`
	Variables   resource.Variables `json:"variables"`
	// SpockUpgradePending is true while an earlier Spock major upgrade of the
	// database hasn't finished, so this update must finish it.
	SpockUpgradePending bool `json:"spock_upgrade_pending,omitempty"`
}

type UpdateDatabaseOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) UpdateDatabase(ctx workflow.Context, input *UpdateDatabaseInput) (*UpdateDatabaseOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)

	// Set when this update raises the Spock major version. DDL replication is
	// paused for the duration of the upgrade and must be resumed however the
	// workflow ends.
	var upgrade *spockUpgrade
	ddlPaused := false
	resumeDDL := func(ctx workflow.Context) error {
		if !ddlPaused {
			return nil
		}
		if err := w.setSpockDDLReplication(ctx, upgrade, false); err != nil {
			logger.With("error", err).Error("failed to resume spock ddl replication")
			// The upgrade stays pending, and the next update resumes it.
			w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "failed to resume spock ddl replication; it resumes when the next update of the database finishes")
			return err
		}
		ddlPaused = false
		w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "resumed spock ddl replication")
		return nil
	}

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			_ = resumeDDL(cleanupCtx)

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

		// Before marking the database failed: a failed database accepts new
		// updates, and one that starts another upgrade must not have its
		// DDL pause undone by this resume.
		if err := resumeDDL(ctx); err != nil {
			cause = errors.Join(cause, err)
		}

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
		Variables:   input.Variables,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	if err := checkPrimaryNotRemoved(input.Spec, current, input.RemoveHosts); err != nil {
		return nil, handleError(err)
	}

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

	upgrade, err = detectSpockUpgrade(current, input.Spec, input.RemoveHosts, input.SpockUpgradePending)
	if err != nil {
		return nil, handleError(err)
	}
	if upgrade != nil {
		if !upgrade.RaisesMajor {
			// An earlier upgrade failed after every instance reached the
			// new major version. Repeat its remaining steps, which are safe
			// to run again.
			w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "finishing an earlier spock upgrade")
		}
		w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "upgrading the spock major version: pausing spock ddl replication on every instance")
		// Set before pausing, so that a partial pause is resumed too.
		ddlPaused = true
		if err := w.setSpockDDLReplication(ctx, upgrade, true); err != nil {
			return nil, handleError(err)
		}
		w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "enabling failover on spock replication slots")
		if err := w.prepareSpockUpgrade(ctx, upgrade); err != nil {
			return nil, handleError(err)
		}
	}

	// Spock steps that run on each node as soon as the rolling update has
	// finished updating it, so that a node that fails them stops the update
	// before the next node is updated.
	var spockNodes *spockInstances
	fixBeta1Catalog := fixesSpockBeta1Catalog(input.Spec)
	switch {
	case upgrade != nil:
		spockNodes = &upgrade.spockInstances
	case fixBeta1Catalog:
		spockNodes, _, err = keptSpockInstances(current, input.Spec, input.RemoveHosts)
		if err != nil {
			return nil, handleError(err)
		}
	}
	nodeSteps := newSpockNodeSteps(input.Spec.DatabaseID, input.TaskID, spockNodes, upgrade, fixBeta1Catalog, planOutput.Plans)
	var afterPlan func(int) error
	if nodeSteps != nil {
		afterPlan = func(i int) error {
			return nodeSteps.run(ctx, w, i)
		}
	}

	err = w.applyPlansWithHook(ctx, input.Spec.DatabaseID, input.TaskID, current, input.Variables, planOutput.Plans, afterPlan, input.RemoveHosts...)
	if err != nil {
		return nil, handleError(err)
	}
	// The nodes that no plan changed.
	if err := nodeSteps.run(ctx, w, len(planOutput.Plans)); err != nil {
		return nil, handleError(err)
	}

	if upgrade != nil {
		w.logSpockUpgradeEvent(ctx, input.Spec.DatabaseID, input.TaskID, "checking replication on every node")
		if err := w.verifySpockUpgrade(ctx, upgrade); err != nil {
			return nil, handleError(err)
		}
		if err := resumeDDL(ctx); err != nil {
			return nil, handleError(err)
		}
		_, err := w.Activities.ExecuteClearSpockUpgradePending(ctx, &activities.ClearSpockUpgradePendingInput{
			DatabaseID: input.Spec.DatabaseID,
		}).Get(ctx)
		if err != nil {
			return nil, handleError(err)
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
