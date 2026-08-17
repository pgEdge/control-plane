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

// MajorVersionUpgradeInput drives a rolling Spock major-version upgrade
// (e.g. 5.x to 6.x). Deliberately separate from UpdateDatabaseInput: rather
// than redeploy every node onto the new image at once the way a same-major
// minor/patch bump does, this walks NodeOrder one node at a time, verifying
// each node actually resumed replicating before moving to the next — the
// only upgrade path proven to work for a Spock major bump (see the
// compatibility finding below), since a from-scratch Spock 6 subscription
// syncing from a Spock 5 peer fails outright (spock.read_peer_progress does
// not exist on 5.x), which rules out any mechanism that would establish a
// brand-new cross-major subscription.
type MajorVersionUpgradeInput struct {
	TaskID             uuid.UUID      `json:"task_id"`
	Spec               *database.Spec `json:"spec"`
	TargetSpockVersion string         `json:"target_spock_version"`
	Image              string         `json:"image"`
	NodeOrder          []string       `json:"node_order"`
}

type MajorVersionUpgradeOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) MajorVersionUpgrade(ctx workflow.Context, input *MajorVersionUpgradeInput) (*MajorVersionUpgradeOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			updateStateInput := &activities.UpdateDbStateInput{
				DatabaseID: input.Spec.DatabaseID,
				State:      database.DatabaseStateFailed,
			}
			if _, err := w.Activities.ExecuteUpdateDbState(cleanupCtx, updateStateInput).Get(cleanupCtx); err != nil {
				logger.With("error", err).Error("failed to update database state")
			}

			w.cancelTask(cleanupCtx, task.ScopeDatabase, input.Spec.DatabaseID, input.TaskID, logger)
		}
	}()

	logger.Info("starting spock major-version upgrade")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to apply spock major-version upgrade")

		updateStateInput := &activities.UpdateDbStateInput{
			DatabaseID: input.Spec.DatabaseID,
			State:      database.DatabaseStateFailed,
		}
		if _, stateErr := w.Activities.ExecuteUpdateDbState(ctx, updateStateInput).Get(ctx); stateErr != nil {
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

	logEvent := func(message string) {
		if err := w.logTaskEvent(ctx, task.ScopeDatabase, input.Spec.DatabaseID, input.TaskID,
			task.LogEntry{Message: message}); err != nil {
			logger.With("error", err).Error("failed to log task event")
		}
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

	spec := input.Spec
	var lastState *resource.State

	for i, nodeName := range input.NodeOrder {
		logEvent(fmt.Sprintf("upgrading node %q to spock %s (%d/%d)", nodeName, input.TargetSpockVersion, i+1, len(input.NodeOrder)))

		nodeSpec := spec.Clone()
		found := false
		for _, node := range nodeSpec.Nodes {
			if node.Name != nodeName {
				continue
			}
			node.SpockVersion = input.TargetSpockVersion
			if node.OrchestratorOpts == nil {
				node.OrchestratorOpts = &database.OrchestratorOpts{}
			}
			if node.OrchestratorOpts.Swarm == nil {
				node.OrchestratorOpts.Swarm = &database.SwarmOpts{}
			}
			node.OrchestratorOpts.Swarm.Image = input.Image
			found = true
			break
		}
		if !found {
			return nil, handleError(fmt.Errorf("node %q not found in database spec", nodeName))
		}

		// Persist this node's spec change durably, one node at a time, before
		// applying it — via the privileged path that bypasses
		// ValidateChangedSpec's ordinary same-major guard, so that a
		// crashed/resumed workflow can tell exactly which nodes have already
		// been rolled just by reading the spec back.
		updateSpecInput := &activities.UpdateSpecInput{
			Spec:  nodeSpec,
			State: database.DatabaseStateModifying,
		}
		if _, err := w.Activities.ExecuteUpdateSpec(ctx, updateSpecInput).Get(ctx); err != nil {
			return nil, handleError(fmt.Errorf("failed to persist spec change for node %q: %w", nodeName, err))
		}

		refreshCurrentInput := &RefreshCurrentStateInput{
			DatabaseID: input.Spec.DatabaseID,
			TaskID:     input.TaskID,
		}
		refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to get current state before upgrading node %q: %w", nodeName, err))
		}
		current := refreshCurrentOutput.State

		planInput := &PlanUpdateInput{
			Spec:    nodeSpec,
			Current: current,
			Options: operations.UpdateDatabaseOptions{},
		}
		planOutput, err := w.ExecutePlanUpdate(ctx, planInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to plan upgrade for node %q: %w", nodeName, err))
		}

		if err := w.persistPlans(ctx, input.Spec.DatabaseID, input.TaskID, planOutput.Plans); err != nil {
			return nil, handleError(err)
		}

		if err := w.applyPlans(ctx, input.Spec.DatabaseID, input.TaskID, current, nil, planOutput.Plans); err != nil {
			return nil, handleError(fmt.Errorf("failed to redeploy node %q: %w", nodeName, err))
		}

		// The fail-loud check: don't move on to the next node — and don't
		// leave a future add-node/other operation to silently discover this
		// later — until this node's replication has actually resumed.
		verifyInput := &activities.VerifyNodeReplicatingInput{
			DatabaseID: input.Spec.DatabaseID,
			NodeName:   nodeName,
		}
		if _, err := w.Activities.ExecuteVerifyNodeReplicating(ctx, verifyInput).Get(ctx); err != nil {
			return nil, handleError(err)
		}

		logEvent(fmt.Sprintf("node %q upgraded and replicating on spock %s", nodeName, input.TargetSpockVersion))

		spec = nodeSpec
		lastState = current
	}

	// Every node is now on the target Spock major. Normalize the spec: move
	// the version onto the database-level field and clear the now-redundant
	// per-node overrides, so the database looks like an ordinary,
	// steady-state spec again rather than one perpetually "mid-upgrade".
	finalSpec := spec.Clone()
	finalSpec.SpockVersion = input.TargetSpockVersion
	for _, node := range finalSpec.Nodes {
		node.SpockVersion = ""
		if node.OrchestratorOpts != nil && node.OrchestratorOpts.Swarm != nil && node.OrchestratorOpts.Swarm.Image == input.Image {
			node.OrchestratorOpts.Swarm.Image = ""
		}
	}

	finalizeInput := &activities.UpdateSpecInput{
		Spec:  finalSpec,
		State: database.DatabaseStateAvailable,
	}
	if _, err := w.Activities.ExecuteUpdateSpec(ctx, finalizeInput).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to normalize spec after major-version upgrade: %w", err))
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
	logger.Info("successfully completed spock major-version upgrade")

	return &MajorVersionUpgradeOutput{Updated: lastState}, nil
}
