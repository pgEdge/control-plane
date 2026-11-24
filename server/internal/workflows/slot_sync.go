package workflows

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
)

// pendingSlotCopyWarnings returns a task log message for each replica that
// the plans created or updated and that doesn't hold permanent copies of all
// its primary's Spock slots yet. See
// database.InstanceResource.SpockSlotCopiesPending.
func pendingSlotCopyWarnings(state *resource.State, plans []resource.Plan) []string {
	var changed []string
	for _, plan := range plans {
		for _, phase := range plan {
			for _, event := range phase {
				if event.Resource == nil || event.Resource.Identifier.Type != database.ResourceTypeInstance {
					continue
				}
				if event.Type != resource.EventTypeCreate && event.Type != resource.EventTypeUpdate {
					continue
				}
				if !slices.Contains(changed, event.Resource.Identifier.ID) {
					changed = append(changed, event.Resource.Identifier.ID)
				}
			}
		}
	}
	slices.Sort(changed)

	var warnings []string
	for _, id := range changed {
		instance, err := resource.FromState[*database.InstanceResource](state, database.InstanceResourceIdentifier(id))
		if err != nil {
			// Deleted, or not applied because the update failed.
			continue
		}
		if len(instance.SpockSlotCopiesPending) == 0 {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"replica %s doesn't hold permanent copies of the spock replication slots for %s yet. Native slot sync makes them permanent once each subscriber confirms new changes. Until then, switchovers won't promote this replica",
			id, strings.Join(instance.SpockSlotCopiesPending, ", "),
		))
	}
	return warnings
}

// logPendingSlotCopies adds the warnings from pendingSlotCopyWarnings to the
// task log. Failing to log them doesn't fail the update.
func (w *Workflows) logPendingSlotCopies(
	ctx workflow.Context,
	databaseID string,
	taskID uuid.UUID,
	state *resource.State,
	plans []resource.Plan,
) {
	warnings := pendingSlotCopyWarnings(state, plans)
	entries := make([]task.LogEntry, len(warnings))
	for i, warning := range warnings {
		entries[i] = task.LogEntry{Message: warning}
	}
	if err := w.logTaskEvent(ctx, task.ScopeDatabase, databaseID, taskID, entries...); err != nil {
		workflow.Logger(ctx).With("database_id", databaseID).Warn("failed to log pending spock slot copies", "error", err)
	}
}
